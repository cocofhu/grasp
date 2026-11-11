package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRunPayloadsExplainAFailedWorkspaceSave(t *testing.T) {
	hn := newHarness(t)
	started := time.Now().UTC()
	if err := hn.db.Create(&models.Run{ID: "run-save", WorkflowID: "wf", WorkflowName: "wf", Status: "paused", PauseFromStatus: "workspace_checkpoint:cp-save", StartedAt: started}).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Create(&models.WorkflowVisitCheckpoint{ID: "cp-save", RunID: "run-save", NodeID: "code", FileState: "blocked", RouteState: "sealed", Reason: "snapshot_failed", SnapshotID: "snap", CaptureStartedAt: &started}).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Create(&models.WorkspaceSnapshotJob{ID: "job", State: "failed", Error: "git_unsupported", Deadline: started}).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Create(&models.WorkspaceSnapshot{ID: "snap", JobID: "job"}).Error; err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"checkpointId": "cp-save", "nodeId": "code", "state": "failed", "reason": "snapshot_failed", "detail": "git_unsupported", "action": "retry"}
	check := func(save any) {
		t.Helper()
		got, _ := save.(map[string]any)
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("workspaceSave.%s = %v, want %v (%v)", k, got[k], v, save)
			}
		}
	}

	w := hn.do("GET", "/api/runs/run-save", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var detail map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &detail)
	check(detail["workspaceSave"])

	w = hn.do("GET", "/api/runs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var list struct{ Items []map[string]any }
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 {
		t.Fatalf("list: %s", w.Body.String())
	}
	check(list.Items[0]["workspaceSave"])
}
