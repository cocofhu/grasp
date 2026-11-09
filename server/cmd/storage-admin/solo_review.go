package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type soloReviewCheckpoint struct {
	Version             int       `json:"version"`
	ID                  string    `json:"id"`
	SandboxID           uint      `json:"sandboxId"`
	ConfigurationSHA256 string    `json:"configurationSHA256"`
	CreatedAt           time.Time `json:"createdAt"`
}

func soloReviewConfiguration(cfg *config.Config) (string, error) {
	sql, err := closureDatabaseIdentity(cfg.Database)
	if err != nil {
		return "", err
	}
	// Config locations, not credentials, bind a resumed workdir to the same
	// installation. Grant resolution separately checks current secret values.
	body, err := json.Marshal(struct {
		SQL, Profiles, MCP, Gateway, Transport string
		Connections                            []config.GatewayConnectionConfig
	}{sql, cfg.Engine.ProfilesRoot, cfg.Server.MCPAdvertise, cfg.Sandbox.GatewayURL, cfg.Sandbox.GatewayTransport, cfg.Sandbox.Gateways})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func prepareSoloReview(opts options, cfg *config.Config) (soloReviewCheckpoint, error) {
	var state soloReviewCheckpoint
	fingerprint, err := soloReviewConfiguration(cfg)
	if err != nil {
		return state, err
	}
	path := filepath.Join(opts.workdir, "solo-environment-review.json")
	raw, err := os.ReadFile(path)
	if err == nil {
		if !opts.resume || len(raw) > 16<<10 || json.Unmarshal(raw, &state) != nil || state.Version != 1 || uuid.Validate(state.ID) != nil || state.CreatedAt.IsZero() || state.SandboxID != opts.sandboxID || state.ConfigurationSHA256 != fingerprint {
			return state, errors.New("original Solo review checkpoint differs; resume the same sandbox, installation and workdir")
		}
		return state, nil
	}
	if !errors.Is(err, os.ErrNotExist) || opts.resume {
		return state, errors.New("original Solo review checkpoint is missing or unreadable")
	}
	entries, err := os.ReadDir(opts.workdir)
	if err != nil {
		return state, err
	}
	for _, entry := range entries {
		if entry.Name() != ".lock" {
			return state, errors.New("Solo environment review requires a new private workdir")
		}
	}
	state = soloReviewCheckpoint{Version: 1, ID: uuid.NewString(), SandboxID: opts.sandboxID, ConfigurationSHA256: fingerprint, CreatedAt: time.Now().UTC()}
	return state, writeJSON(path, state)
}

type soloReviewGateway struct {
	client        *sandbox.GatewayClient
	configuration string
}

func (p *soloReviewGateway) Observe(ctx context.Context, id string) (executionlineage.SoloReviewObservation, error) {
	var result executionlineage.SoloReviewObservation
	info, err := p.client.InfoFor(ctx, id)
	if err != nil {
		return result, errors.New("original gateway capabilities are unavailable")
	}
	// Pause would retain old credentials in RAM. This review admits only a
	// cold-stop provider; it never unpauses or claims experimental RAM support.
	if info.Driver != "docker" && info.Driver != "kubernetes" {
		return result, errors.New("offline Solo review requires a supported cold-stop provider; paused environments remain quarantined")
	}
	before, err := p.client.Get(ctx, id)
	if err != nil || before == nil || before.ID != id || before.Name == "" {
		return result, errors.New("original gateway resource identity is unavailable")
	}
	status, err := p.client.LiveStatus(ctx, id)
	if err != nil {
		return result, errors.New("original provider status is unconfirmed")
	}
	after, err := p.client.Get(ctx, id)
	if err != nil || after == nil || after.ID != id {
		return result, errors.New("original gateway resource identity changed")
	}
	fingerprint := func(row *sandbox.GWSandbox) string {
		body, _ := json.Marshal(struct {
			Configuration, Driver, ID, Name, Namespace, Image string
			Labels                                            map[string]string
		}{p.configuration, info.Driver, row.ID, row.Name, row.Namespace, row.Image, row.Labels})
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	if fingerprint(before) != fingerprint(after) {
		return result, errors.New("original gateway resource identity changed during observation")
	}
	switch status {
	case "stopped", "exited":
		status = "stopped"
	case "running":
	default:
		return result, errors.New("original provider is not positively running or stopped")
	}
	return executionlineage.SoloReviewObservation{Fingerprint: fingerprint(after), Status: status}, nil
}

func (p *soloReviewGateway) Stop(ctx context.Context, id string) error {
	if err := p.client.Stop(ctx, id); err != nil {
		return errors.New("original provider stop outcome is unknown; resume the same workdir to observe it")
	}
	return nil
}

func newSoloReviewAuthorization(db *gorm.DB, cfg *config.Config) executionlineage.SoloReviewAuthorization {
	crypto.SetKeySource(cfg.SecretsKey)
	credentials := services.NewCredentialService(db)
	connections := services.NewModelConnectionService(db)
	agents := services.NewAgentService(cfg.Engine.ProfilesRoot)
	shared := services.NewSharedAgentService(services.DefaultSharedAgentRoot(cfg.Engine.ProfilesRoot))
	agents.SetSharedAgent(shared)
	svc := services.NewSandboxService(db, nil, agents, nil, services.SandboxOptions{SharedAgent: shared, MCPEndpoint: cfg.Server.MCPAdvertise, Env: cfg.Sandbox.Env,
		ModelBinding: func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
			if err := modelrouting.ValidateConfigurationReference(binding); err != nil {
				return modelrouting.Resolved{}, err
			}
			return connections.Resolve(backend, *binding)
		},
		CredentialReferences: credentials.ResolveReferences, SelectedCredential: credentials.ResolveSelectedValue, GitCredential: credentials.ResolveGitValue})
	return svc.OfflineSoloEnvironmentAuthorization
}

func reviewSoloEnvironment(ctx context.Context, opts options, cfg *config.Config, db *gorm.DB, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	lock, err := lockExecutionWorkdir(opts.workdir, !opts.resume)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	state, err := prepareSoloReview(opts, cfg)
	if err != nil {
		return err
	}
	if opts.executionControl == "" && cfg.ExecutionControl == nil {
		return errors.New("existing independent execution control is required")
	}
	var authority *executionlineage.Authority
	if opts.executionControl != "" {
		authority, err = openExecutionControl(ctx, opts, cfg)
	} else {
		protected := []string{opts.workdir}
		if cfg.Database.Driver != "mysql" {
			protected = append(protected, filepath.Dir(cfg.Database.Path))
		}
		authority, err = executionlineage.OpenConfigured(ctx, *cfg.ExecutionControl, []config.StorageConfig{cfg.Storage}, protected)
	}
	if err != nil {
		return errors.New("existing independent execution authority is unavailable")
	}
	permit, err := authority.ReadActive(ctx)
	if err != nil {
		return err
	}
	if err := authority.CheckBinding(ctx, db, permit); err != nil {
		return err
	}
	// A lost final response reads the original SQL receipt. It must not enter
	// new maintenance or stop a workspace that has subsequently been resumed.
	if db.Migrator().HasTable(&executionlineage.SoloEnvironmentReview{}) {
		var saved executionlineage.SoloEnvironmentReview
		err := db.First(&saved, "id = ?", state.ID).Error
		if err == nil && saved.FinalizedAt != nil {
			if saved.Protocol != executionlineage.SoloEnvironmentReviewProtocol || saved.SandboxID != state.SandboxID || saved.DatasetID != permit.DatasetID() || saved.Lineage != permit.Lineage() {
				return executionlineage.ErrConflict
			}
			return json.NewEncoder(output).Encode(map[string]any{"completed": true, "finalized": true, "originalReceipt": true, "sourceRetained": true, "executionUnchanged": true, "receipt": saved})
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if err := storageguard.Enter(ctx, db, state.ID, stoppedRuntimes(opts)); err != nil {
		return err
	}
	failure := func(err error) error {
		return fmt.Errorf("%w; Solo review maintenance %s remains active; resume the original workdir", err, state.ID)
	}
	client, err := sandbox.NewConfiguredGatewayClient(cfg.Sandbox)
	if err != nil {
		return failure(errors.New("original gateway configuration is unavailable"))
	}
	defer func() { _ = client.Close() }()
	provider := &soloReviewGateway{client: client, configuration: state.ConfigurationSHA256}
	authorize := newSoloReviewAuthorization(db, cfg)
	var receipt executionlineage.SoloEnvironmentReview
	if opts.finalize {
		receipt, err = authority.FinalizeSoloEnvironmentReview(ctx, db, state.ID, state.SandboxID, provider, authorize)
	} else {
		receipt, err = authority.ReviewSoloEnvironment(ctx, db, state.ID, state.SandboxID, provider, authorize)
	}
	if err != nil {
		return failure(err)
	}
	if err := writeJSON(filepath.Join(opts.workdir, "solo-environment-review-complete.json"), receipt); err != nil {
		return failure(err)
	}
	return json.NewEncoder(output).Encode(map[string]any{"completed": true, "finalized": receipt.FinalizedAt != nil, "maintenanceActive": receipt.FinalizedAt == nil, "sourceRetained": true, "originalSandboxRetained": true, "executionUnchanged": true, "oldRequestsReplayed": false, "authorizationRebuildRequired": true, "receipt": receipt})
}
