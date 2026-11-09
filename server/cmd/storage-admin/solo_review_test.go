package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
)

type reviewGatewayFixture struct {
	mu               sync.Mutex
	status, driver   string
	stops, forbidden int
	loseStop         bool
}

func (f *reviewGatewayFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/info":
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "driver": f.driver, "capabilities": map[string]string{"stopMode": "stop", "diskMode": "retained"}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sandboxes/original-provider":
		_ = json.NewEncoder(w).Encode(sandbox.GWSandbox{ID: "original-provider", Name: "original-native-workload", Namespace: "owned-test", Image: "existing-image", Status: f.status, Labels: map[string]string{"owner": "original-owner"}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sandboxes/original-provider/status":
		_ = json.NewEncoder(w).Encode(map[string]string{"status": f.status})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sandboxes/original-provider/stop":
		f.stops++
		f.status = "stopped"
		if f.loseStop {
			w.WriteHeader(http.StatusBadGateway)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		f.forbidden++
		w.WriteHeader(http.StatusNotFound)
	}
}

func offlineReviewCLI(t *testing.T) (options, *config.Config, *gorm.DB, models.Sandbox, *reviewGatewayFixture) {
	t.Helper()
	opts, cfg := executionOptions(t)
	if err := os.MkdirAll(filepath.Dir(cfg.Database.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg.Security.SecretsKey = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	crypto.SetKeySource(cfg.SecretsKey)
	cfg.Engine.ProfilesRoot = filepath.Join(filepath.Dir(opts.config), "profiles")
	db, err := database.OpenWithFiles(cfg.Database, filepath.Join(cfg.Storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	credential, err := services.NewCredentialService(db).Create(services.CredentialInput{Purpose: "tool_auth", ToolID: "cursor", Name: "Synthetic Cursor", Value: "synthetic-private-key"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModelConfiguration{ID: "original-config", Name: "Original", Backend: "cursor", State: "active", Binding: modelrouting.Binding{Kind: "platform", CredentialID: credential.ID, ModelID: "fixture"}}).Error; err != nil {
		t.Fatal(err)
	}
	agents := services.NewAgentService(cfg.Engine.ProfilesRoot)
	if err := agents.Save(services.Agent{Name: "original-agent", AcpBackend: "cursor", ModelBinding: modelrouting.ConfigurationReference("original-config")}); err != nil {
		t.Fatal(err)
	}
	row := models.Sandbox{Name: "original-provider", Profile: "original-agent", Purpose: "solo", ThreadID: "original-solo", Status: "running"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: row.ThreadID, UserID: "original-owner", AgentName: row.Profile, SandboxID: row.ID, Status: models.SoloStatusRunning, InitialConfigurationID: "original-config", CurrentConfigurationID: "original-config", AppliedConfigurationID: "original-config"}).Error; err != nil {
		t.Fatal(err)
	}
	f := &reviewGatewayFixture{status: "running", driver: "kubernetes"}
	upstream := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(upstream.Close)
	cfg.Sandbox.GatewayURL = upstream.URL
	writeYAML(t, opts.config, cfg)
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	opts.mode = "review-solo-environment"
	opts.workdir = filepath.Join(filepath.Dir(opts.config), "review-one")
	opts.sandboxID = row.ID
	return opts, cfg, db, row, f
}

func TestOfflineSoloReviewCLIRequiresExactManualScopeAndCompletesOriginalWorkdir(t *testing.T) {
	opts, _, db, row, provider := offlineReviewCLI(t)
	for _, bad := range []options{{mode: opts.mode, workdir: opts.workdir, sandboxID: row.ID}, {mode: opts.mode, offline: true, workdir: opts.workdir}, {mode: opts.mode, offline: true, sandboxID: row.ID}, {mode: opts.mode, offline: true, workdir: opts.workdir, sandboxID: row.ID, finalize: true}} {
		if err := validateOptions(bad); err == nil {
			t.Fatal("review accepted incomplete operator scope", bad)
		}
	}
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	if provider.stops != 1 || provider.forbidden != 0 {
		t.Fatal("review created, deleted, started, or repeated a native action", provider)
	}
	var current models.Sandbox
	if err := db.First(&current, row.ID).Error; err != nil || current.Name != row.Name || current.Status != "stopped" || current.AuthorizationFingerprint != "" {
		t.Fatal("review invented old authorization or replaced environment", err)
	}
	bad := opts
	bad.resume = true
	bad.sandboxID++
	if err := run(t.Context(), bad, io.Discard); err == nil {
		t.Fatal("another sandbox reused original review nonce")
	}
	if provider.stops != 1 {
		t.Fatal("mismatched review called provider")
	}
	opts.resume, opts.finalize = true, true
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal(err)
	}
	var control storageguard.Control
	if err := db.First(&control, "name = 'primary'").Error; err != nil || control.Owner != "" {
		t.Fatal("explicit finalize did not release original owner", err)
	}
	// The same lost HTTP/CLI acknowledgement remains a receipt read after
	// the user later resumes the provider. It must never stop it again.
	provider.mu.Lock()
	provider.status = "running"
	provider.mu.Unlock()
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("lost final response could not be read", err)
	}
	if provider.stops != 1 || provider.forbidden != 0 {
		t.Fatal("final receipt replayed the stop", provider)
	}
	controller, err := executionlineage.NewController(t.Context(), db, mustReviewAuthority(t, opts, db))
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Check(t.Context(), executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: fmt.Sprint(row.ID)}); err != nil {
		t.Fatal(err)
	}
}

func mustReviewAuthority(t *testing.T, opts options, db *gorm.DB) *executionlineage.Authority {
	t.Helper()
	if err := config.Load(opts.config); err != nil {
		t.Fatal(err)
	}
	a, err := openExecutionControl(t.Context(), opts, config.GetConfig())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestOfflineSoloReviewCLILostStopAndUnsupportedPauseRetainMaintenance(t *testing.T) {
	for _, pause := range []bool{false, true} {
		t.Run(fmt.Sprint(pause), func(t *testing.T) {
			opts, _, db, _, provider := offlineReviewCLI(t)
			provider.loseStop = !pause
			if pause {
				provider.driver = "e2b"
			}
			if err := run(t.Context(), opts, io.Discard); err == nil {
				t.Fatal("unconfirmed stop completed")
			}
			var control storageguard.Control
			if err := db.First(&control, "name = 'primary'").Error; err != nil || control.Owner == "" {
				t.Fatal("failed review cleared maintenance", err)
			}
			if pause {
				if provider.stops != 0 || provider.forbidden != 0 {
					t.Fatal("pause-only provider was unpaused or altered", provider)
				}
				return
			}
			opts.resume = true
			if err := run(t.Context(), opts, io.Discard); err != nil {
				t.Fatal("lost stop response was not reconciled", err)
			}
			if provider.stops != 1 {
				t.Fatal("reconciliation repeated stop", provider.stops)
			}
		})
	}
}

func TestOfflineSoloReviewGrantRejectsMissingOrRevokedOriginalProfile(t *testing.T) {
	opts, cfg, _, _, provider := offlineReviewCLI(t)
	if err := os.RemoveAll(filepath.Join(cfg.Engine.ProfilesRoot, "original-agent")); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), opts, io.Discard); err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("unavailable current grant accepted", err)
	}
	if provider.stops != 0 {
		t.Fatal("revoked Agent called Stop", provider.stops)
	}
}
