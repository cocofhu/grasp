package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type restoreAPIProbe struct {
	row                       models.WorkspaceRestore
	actor                     string
	input                     services.WorkspaceRestoreInput
	admissions, reads, checks int
}

func (p *restoreAPIProbe) Admit(_ context.Context, actor string, input services.WorkspaceRestoreInput) (models.WorkspaceRestore, error) {
	p.actor, p.input, p.admissions = actor, input, p.admissions+1
	return p.row, nil
}

func (p *restoreAPIProbe) Get(_ context.Context, actor, _ string) (models.WorkspaceRestore, error) {
	p.actor, p.reads = actor, p.reads+1
	return p.row, nil
}

func (p *restoreAPIProbe) Reconcile(_ context.Context, actor, _ string) (models.WorkspaceRestore, error) {
	p.actor, p.checks = actor, p.checks+1
	return p.row, nil
}

func (p *restoreAPIProbe) Resolve(ctx context.Context, actor, _, _ string) (models.WorkspaceRestore, error) {
	return p.Get(ctx, actor, p.row.ID)
}

func (p *restoreAPIProbe) List(context.Context, string, string, string, int) (services.WorkspaceRestoreHistoryPage, error) {
	return services.WorkspaceRestoreHistoryPage{Items: []models.WorkspaceRestore{p.row}}, nil
}

func restoreAPIFixture(t *testing.T) (*Handlers, *restoreAPIProbe, *bool) {
	t.Helper()
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "restore-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	allowed := true
	catalog, err := services.NewWorkspaceSnapshotCatalog(db, func(_ context.Context, _ *gorm.DB, actor, kind, id string) error {
		if !allowed || actor != "alice" || kind != "solo" || id != "solo-original" {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	probe := &restoreAPIProbe{row: models.WorkspaceRestore{ID: "restore-original", SoloID: "solo-original", SnapshotID: "snapshot-original", State: "unknown", Destination: "/private/materialized/copy", SourceSpec: "private-source-spec", SourceReceipt: "private-native-receipt", LifecycleAttempt: "private-attempt", Error: "private-upstream-diagnostic"}}
	return &Handlers{Auth: &auth.Service{}, WorkspaceSnapshots: catalog, WorkspaceRestore: probe}, probe, &allowed
}

func restoreAPIContext(method, body string, embedded bool) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/solo/sessions/solo-original/workspace-restores/restore-original", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "solo-original"}, {Key: "restoreId", Value: "restore-original"}}
	c.Set("auth_session", &models.Session{Username: "alice"})
	if embedded {
		c.Set(soloEmbedUserKey, "alice")
	}
	return c, w
}

func TestWorkspaceRestoreAPIRecordsOriginalIntentAndRefusesCallerPaths(t *testing.T) {
	h, probe, _ := restoreAPIFixture(t)
	body := `{"clientRequestId":"request-original","snapshotId":"snapshot-original","expectedBinding":"opaque-original-binding","durationMillis":60000}`
	c, w := restoreAPIContext(http.MethodPost, body, false)
	h.AdmitWorkspaceRestore(c)
	if w.Code != http.StatusAccepted || probe.admissions != 1 || probe.actor != "alice" || probe.input.ClientRequestID != "request-original" || probe.input.SoloID != "solo-original" || probe.input.ExpectedBinding != "opaque-original-binding" || probe.input.Duration != time.Minute || probe.checks != 0 {
		t.Fatal("admission changed original identity or executed a request-scoped worker", w.Code, probe)
	}
	for _, invalid := range []string{body + `{}`, strings.TrimSuffix(body, "}") + `,"directory":"/caller/target"}`, strings.TrimSuffix(body, "}") + `,"actorId":"bob"}`, strings.Replace(body, "60000", "0", 1)} {
		c, w = restoreAPIContext(http.MethodPost, invalid, false)
		h.AdmitWorkspaceRestore(c)
		if w.Code != http.StatusBadRequest || probe.admissions != 1 {
			t.Fatal("invalid or caller-controlled target reached admission", w.Code, invalid, probe.admissions)
		}
	}
	c, w = restoreAPIContext(http.MethodPost, body, true)
	h.AdmitWorkspaceRestore(c)
	if w.Code != http.StatusForbidden || probe.admissions != 1 {
		t.Fatal("embedded identity acquired restore authority", w.Code, probe.admissions)
	}
}

func TestWorkspaceRestoreAPIHistoryNeverReplaysAndReconcileChecksOriginalScope(t *testing.T) {
	h, probe, allowed := restoreAPIFixture(t)
	c, w := restoreAPIContext(http.MethodGet, "", false)
	h.GetWorkspaceRestore(c)
	if w.Code != http.StatusOK || probe.reads != 1 || probe.checks != 0 || probe.admissions != 0 || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), `"state":"unknown"`) {
		t.Fatal("history executed or exposed a private receipt", w.Code, w.Body, probe)
	}
	c, w = restoreAPIContext(http.MethodPost, "", false)
	h.ReconcileWorkspaceRestore(c)
	if w.Code != http.StatusOK || probe.checks != 1 || probe.admissions != 0 {
		t.Fatal("explicit reconciliation did not inspect the original operation", w.Code, probe)
	}
	probe.row.SoloID = "different-solo"
	c, w = restoreAPIContext(http.MethodPost, "", false)
	h.ReconcileWorkspaceRestore(c)
	if w.Code != http.StatusNotFound || probe.checks != 1 {
		t.Fatal("cross-Solo receipt was reconciled", w.Code, probe)
	}
	*allowed = false
	before := probe.reads
	c, w = restoreAPIContext(http.MethodGet, "", false)
	h.GetWorkspaceRestore(c)
	if w.Code != http.StatusNotFound || probe.reads != before {
		t.Fatal("revoked current scope read reached the receipt", w.Code, probe)
	}
}
