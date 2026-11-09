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
	mu                     sync.Mutex
	status, driver         string
	stops, forbidden       int
	loseStop               bool
	cold                   bool
	infoCalls              int
	dropColdAfterPreflight bool
	afterFirstInfo         func()
}

func (f *reviewGatewayFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v2/info":
		f.infoCalls++
		if f.infoCalls == 1 && f.afterFirstInfo != nil {
			f.afterFirstInfo()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"version": 2, "driver": f.driver, "capabilities": map[string]any{"stopMode": "stop", "diskMode": "retained", "coldConfiguration": f.cold && (!f.dropColdAfterPreflight || f.infoCalls == 1)}})
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
	f := &reviewGatewayFixture{status: "running", driver: "kubernetes", cold: true}
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

func TestOfflineSoloReviewCLILostStopRetainsMaintenanceAndOnlyObserves(t *testing.T) {
	opts, _, db, _, provider := offlineReviewCLI(t)
	provider.mu.Lock()
	provider.loseStop = true
	provider.mu.Unlock()
	if err := run(t.Context(), opts, io.Discard); err == nil {
		t.Fatal("unconfirmed stop completed")
	}
	var control storageguard.Control
	if err := db.First(&control, "name = 'primary'").Error; err != nil || control.Owner == "" {
		t.Fatal("failed review cleared maintenance", err)
	}
	opts.resume = true
	if err := run(t.Context(), opts, io.Discard); err != nil {
		t.Fatal("lost stop response was not reconciled", err)
	}
	if provider.stops != 1 {
		t.Fatal("reconciliation repeated stop", provider.stops)
	}
}

func TestOfflineSoloReviewUnsupportedPreflightLeavesSQLAndProviderUnchanged(t *testing.T) {
	for _, kind := range []string{"docker", "e2b", "kubernetes_without_capability"} {
		t.Run(kind, func(t *testing.T) {
			opts, _, db, row, provider := offlineReviewCLI(t)
			var before storageguard.Control
			if err := db.First(&before, "name = 'primary'").Error; err != nil {
				t.Fatal(err)
			}
			provider.mu.Lock()
			if kind == "kubernetes_without_capability" {
				provider.cold = false
			} else {
				provider.driver = kind
			}
			provider.mu.Unlock()
			if err := run(t.Context(), opts, io.Discard); err == nil {
				t.Fatal("unsupported legacy provider was accepted")
			}
			var after storageguard.Control
			if err := db.First(&after, "name = 'primary'").Error; err != nil || after != before {
				t.Fatal("unsupported preflight entered or changed SQL maintenance", err)
			}
			if provider.stops != 0 || provider.forbidden != 0 || provider.infoCalls != 1 {
				t.Fatal("unsupported preflight dispatched or probed a guest", provider)
			}
			var original models.Sandbox
			if err := db.First(&original, row.ID).Error; err != nil || original.Name != row.Name || original.Status != "running" || original.AuthorizationFingerprint != "" {
				t.Fatal("unsupported preflight changed the original binding", err)
			}
			if db.Migrator().HasTable(&executionlineage.SoloEnvironmentReview{}) {
				var count int64
				if err := db.Model(&executionlineage.SoloEnvironmentReview{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatal("unsupported preflight created a dispatched SQL review receipt", err)
				}
			}
		})
	}
}

func TestOfflineSoloReviewBindingChangeDuringPreflightNeverStopsReplacement(t *testing.T) {
	opts, _, db, row, provider := offlineReviewCLI(t)
	provider.mu.Lock()
	provider.afterFirstInfo = func() {
		if err := db.Table("sandboxes").Where("id = ?", row.ID).Update("name", "different-native-resource").Error; err != nil {
			t.Error(err)
		}
	}
	provider.mu.Unlock()
	if err := run(t.Context(), opts, io.Discard); err == nil {
		t.Fatal("changed original binding was admitted")
	}
	if provider.stops != 0 || provider.forbidden != 0 || provider.infoCalls != 1 {
		t.Fatal("replacement binding was observed or stopped", provider)
	}
	var control storageguard.Control
	if err := db.First(&control, "name = 'primary'").Error; err != nil || control.Owner == "" {
		t.Fatal("post-admission identity failure cleared maintenance", err)
	}
	var pending int64
	if err := db.Table("execution_quarantines").Where("entity_kind = 'sandbox' AND entity_id = ? AND review_state = 'pending'", fmt.Sprint(row.ID)).Count(&pending).Error; err != nil || pending != 1 {
		t.Fatal("changed source cleared original quarantine", err)
	}
}

func TestOfflineSoloReviewRechecksCapabilityBeforeStop(t *testing.T) {
	opts, _, db, _, provider := offlineReviewCLI(t)
	provider.mu.Lock()
	provider.dropColdAfterPreflight = true
	provider.mu.Unlock()
	if err := run(t.Context(), opts, io.Discard); err == nil {
		t.Fatal("changed capability was accepted after maintenance admission")
	}
	var control storageguard.Control
	if err := db.First(&control, "name = 'primary'").Error; err != nil || control.Owner == "" {
		t.Fatal("admitted failure did not preserve maintenance", err)
	}
	if provider.stops != 0 || provider.forbidden != 0 || provider.infoCalls < 2 {
		t.Fatal("capability was not rechecked before Stop", provider)
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
