package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

type snapshotJobAPI struct {
	row        models.WorkspaceSnapshotJob
	input      services.WorkspaceJobInput
	actor      string
	admitted   int
	reconciled int
}

func (s *snapshotJobAPI) Admit(_ context.Context, actor string, input services.WorkspaceJobInput) (models.WorkspaceSnapshotJob, error) {
	s.actor, s.input, s.admitted = actor, input, s.admitted+1
	return s.row, nil
}
func (s *snapshotJobAPI) Get(_ context.Context, actor, _ string) (models.WorkspaceSnapshotJob, error) {
	s.actor = actor
	return s.row, nil
}
func (s *snapshotJobAPI) Reconcile(_ context.Context, actor, _ string) (models.WorkspaceSnapshotJob, error) {
	s.actor, s.reconciled = actor, s.reconciled+1
	return s.row, nil
}

func snapshotJobContext(method, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/solo/sessions/solo-original/workspace-jobs/job-original", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "solo-original"}, {Key: "jobId", Value: "job-original"}}
	return c, w
}

func TestWorkspaceSnapshotJobHandlerUsesCookieScopeAndHidesRuntimeCapability(t *testing.T) {
	service := &snapshotJobAPI{row: models.WorkspaceSnapshotJob{ID: "job-original", ScopeKind: "solo", ScopeID: "solo-original", State: "queued", Deadline: time.Now().Add(time.Minute), RuntimeOwner: "secret-runtime-capability", Owner: "secret-sql-capability", Lineage: "private-lineage", Body: `{"secret":"private-spec"}`}}
	h := &WorkspaceSnapshotJobHandler{Jobs: service, Identity: func(*gin.Context) (string, bool) { return "alice", true }}
	c, w := snapshotJobContext(http.MethodPost, `{"clientRequestId":"request-original","sandboxId":12,"kind":"control_probe","durationMillis":1000,"body":{"probe":true},"actorId":"bob","scopeId":"other"}`)
	h.Admit(c)
	if w.Code != http.StatusAccepted || service.admitted != 1 || service.actor != "alice" || service.input.ScopeID != "solo-original" || service.input.ScopeKind != "solo" {
		t.Fatalf("admission lost canonical actor/scope: %d %+v", w.Code, service)
	}
	var view map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || len(view) != 4 || view["id"] != "job-original" || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("runtime owner/spec leaked through browser DTO: %s %v", w.Body, err)
	}
	c, w = snapshotJobContext(http.MethodPost, `{}`)
	c.Set(soloEmbedUserKey, "alice")
	h.Admit(c)
	if w.Code != http.StatusForbidden || service.admitted != 1 {
		t.Fatal("embedded preview became filesystem authority")
	}
}

func TestWorkspaceSnapshotJobHandlerReconcilesOnlyExplicitOwnedReceipt(t *testing.T) {
	service := &snapshotJobAPI{row: models.WorkspaceSnapshotJob{ID: "job-original", ScopeKind: "solo", ScopeID: "solo-original", State: "unknown"}}
	h := &WorkspaceSnapshotJobHandler{Jobs: service, Identity: func(*gin.Context) (string, bool) { return "alice", true }}
	c, w := snapshotJobContext(http.MethodGet, "")
	h.Get(c)
	if w.Code != http.StatusOK || service.reconciled != 0 {
		t.Fatal("GET implicitly reconciled or replayed a job")
	}
	c, w = snapshotJobContext(http.MethodPost, "")
	h.Reconcile(c)
	if w.Code != http.StatusOK || service.reconciled != 1 {
		t.Fatal("explicit check did not reconcile")
	}
	service.row.ScopeID = "some-other-solo"
	c, w = snapshotJobContext(http.MethodPost, "")
	h.Reconcile(c)
	if w.Code != http.StatusNotFound || service.reconciled != 1 {
		t.Fatal("cross-Solo receipt was exposed/reconciled")
	}
	h.Identity = nil
	c, w = snapshotJobContext(http.MethodGet, "")
	h.Get(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("unconfigured auth was treated as ready")
	}
}
