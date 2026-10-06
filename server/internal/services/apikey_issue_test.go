package services

import (
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestAPIKeyServiceLifecycle(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "apikey.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-a", Name: "A", Version: 1, PublishedVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewAPIKeyService(db)

	if _, err := svc.Create("missing", "x"); err == nil {
		t.Fatal("expected missing workflow error")
	}
	if _, err := svc.Create("wf-a", "  "); err == nil {
		t.Fatal("expected empty name error")
	}

	res, err := svc.Create("wf-a", "ci")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.Plaintext == "" || res.Key.ID == "" {
		t.Fatalf("empty result: %+v", res)
	}
	wfID, ok := svc.ValidateBearer(res.Plaintext)
	if !ok || wfID != "wf-a" {
		t.Fatalf("ValidateBearer = %q,%v", wfID, ok)
	}
	if _, ok := svc.ValidateBearer("cf_wf_nope"); ok {
		t.Fatal("bad key should fail")
	}

	keys := svc.List("wf-a")
	if len(keys) != 1 {
		t.Fatalf("List = %d", len(keys))
	}
	if !svc.Revoke("wf-a", res.Key.ID) {
		t.Fatal("Revoke should succeed")
	}
	if svc.Revoke("wf-a", res.Key.ID) {
		t.Fatal("second Revoke should be false")
	}
	if _, ok := svc.ValidateBearer(res.Plaintext); ok {
		t.Fatal("revoked key must not validate")
	}
	if len(svc.List("wf-a")) != 0 {
		t.Fatal("List should exclude revoked keys")
	}
}

func TestIssueServiceCRUD(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "issue.db"))
	if err != nil {
		t.Fatal(err)
	}
	run := models.Run{ID: "run-1", WorkflowID: "wf-1", WorkflowName: "Demo", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewIssueService(db)

	iss, err := svc.Create("run-1", "preview", "broken btn", "#ok", 3000, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if iss.ID == "" || iss.WorkflowID != "wf-1" || iss.Status != "open" {
		t.Fatalf("issue = %+v", iss)
	}
	list := svc.ListByRunNode("run-1", "preview")
	if len(list) != 1 {
		t.Fatalf("List = %d", len(list))
	}
	if err := svc.Delete("run-1", "preview", iss.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(svc.ListByRunNode("run-1", "preview")) != 0 {
		t.Fatal("expected empty after delete")
	}
}

func TestIssueServiceMarkResolvedByNode(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "issue_mark.db"))
	if err != nil {
		t.Fatal(err)
	}
	run := models.Run{ID: "run-mr", WorkflowID: "wf-1", WorkflowName: "Demo", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewIssueService(db)

	a, err := svc.Create("run-mr", "gate-a", "open on a", "", 0, nil)
	if err != nil {
		t.Fatalf("Create a: %v", err)
	}
	b, err := svc.Create("run-mr", "gate-b", "open on b", "", 0, nil)
	if err != nil {
		t.Fatalf("Create b: %v", err)
	}
	// Pre-resolved sibling on same node must stay resolved (not re-touched).
	already := models.PreviewIssue{
		ID: "iss-done", RunID: "run-mr", NodeID: "gate-a",
		Body: "already resolved", Status: "resolved",
	}
	if err := db.Create(&already).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.MarkResolvedByNode("run-mr", "gate-a"); err != nil {
		t.Fatalf("MarkResolvedByNode: %v", err)
	}

	var gotA models.PreviewIssue
	if err := db.First(&gotA, "id = ?", a.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotA.Status != "resolved" {
		t.Fatalf("gate-a open issue status = %q, want resolved", gotA.Status)
	}

	var gotB models.PreviewIssue
	if err := db.First(&gotB, "id = ?", b.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotB.Status != "open" {
		t.Fatalf("other node status = %q, want open", gotB.Status)
	}

	var gotDone models.PreviewIssue
	if err := db.First(&gotDone, "id = ?", already.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotDone.Status != "resolved" {
		t.Fatalf("pre-resolved status = %q, want resolved", gotDone.Status)
	}

	// Idempotent: second call must not error or change other nodes.
	if err := svc.MarkResolvedByNode("run-mr", "gate-a"); err != nil {
		t.Fatalf("MarkResolvedByNode idempotent: %v", err)
	}
	if err := db.First(&gotB, "id = ?", b.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotB.Status != "open" {
		t.Fatalf("other node after second mark = %q, want open", gotB.Status)
	}
}
