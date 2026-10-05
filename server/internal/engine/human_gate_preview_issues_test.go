package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TestHumanGateResumeSnapshotsPreviewIssues: Fail/resume on human_gate (HtmlPreview
// path) must write vars.preview_issues with selector context + screenshot images,
// same protocol as the preview review agent — not form.comment. After a successful snapshot the
// open issues for this node must become resolved.
func TestHumanGateResumeSnapshotsPreviewIssues(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title": "网页预览", "output_var": "action",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions": []any{
					map[string]any{"id": "approve", "label": "通过"},
					map[string]any{"id": "revise", "label": "退回"},
				},
			}},
			{ID: "done", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e_revise_1", Source: "gate", Target: "done", SourceHandle: "revise"},
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "done", SourceHandle: "approve", Kind: models.EdgeSuccess},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")
	waitRunStatus(t, db, run.ID, "waiting_human")

	img := models.PromptImage{Data: "iVBORw0KGgo=", MimeType: "image/png"}
	issue := models.PreviewIssue{
		ID: "iss-hg1", RunID: run.ID, NodeID: "gate",
		Body: "标题间距过大", Selector: "h1.title", Port: 0,
		Images: []models.PromptImage{img},
		Status: "open", CreatedAt: time.Now(),
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatalf("create issue: %v", err)
	}
	other := models.PreviewIssue{
		ID: "iss-other", RunID: run.ID, NodeID: "other_gate",
		Body: "other node", Status: "open", CreatedAt: time.Now(),
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("create other: %v", err)
	}

	if err := eng.ResumeGate(run.ID, "gate", "revise", map[string]any{"comment": ""}); err != nil {
		t.Fatalf("resume revise: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var countVar models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues_count").First(&countVar).Error; err != nil {
		t.Fatalf("preview_issues_count missing: %v", err)
	}
	if fmtNum(countVar.Value) != 1 {
		t.Fatalf("preview_issues_count = %v, want 1", countVar.Value)
	}

	var issuesVar models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues").First(&issuesVar).Error; err != nil {
		t.Fatalf("preview_issues missing: %v", err)
	}
	ct := models.AsCompositeText(issuesVar.Value)
	if ct == nil {
		t.Fatalf("preview_issues not composite: %#v", issuesVar.Value)
	}
	if !strings.Contains(ct.Text, "[选中: h1.title]") {
		t.Fatalf("preview_issues text missing selector context: %q", ct.Text)
	}
	if !strings.Contains(ct.Text, "标题间距过大") {
		t.Fatalf("preview_issues text missing body: %q", ct.Text)
	}
	if len(ct.Images) != 1 || ct.Images[0].Data != img.Data {
		t.Fatalf("preview_issues images = %+v, want 1 screenshot", ct.Images)
	}

	var got models.PreviewIssue
	if err := db.First(&got, "id = ?", issue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "resolved" {
		t.Fatalf("after snapshot status = %q, want resolved", got.Status)
	}
	var gotOther models.PreviewIssue
	if err := db.First(&gotOther, "id = ?", other.ID).Error; err != nil {
		t.Fatal(err)
	}
	if gotOther.Status != "open" {
		t.Fatalf("other node status = %q, want open", gotOther.Status)
	}
}

// TestCommentOnlyHumanGateDoesNotWipePreviewIssues: a non-page.html human_gate
// resume must not snapshot (and thus must not clear) vars.preview_issues left by
// an earlier HtmlPreview Fail snapshot in the same run.
func TestCommentOnlyHumanGateDoesNotWipePreviewIssues(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "visual_gate", Type: "human_gate", Config: map[string]any{
				"title": "网页预览", "output_var": "action",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions": []any{
					map[string]any{"id": "approve", "label": "通过"},
					map[string]any{"id": "revise", "label": "退回"},
				},
			}},
			{ID: "review", Type: "human_gate", Config: map[string]any{
				"title": "文字评审", "output_var": "review_action",
				"body_template": "{{nodes.research.outputs.research}}",
				"actions": []any{
					map[string]any{"id": "approve", "label": "通过"},
					map[string]any{"id": "revise", "label": "退回"},
				},
				"form": []any{
					map[string]any{"key": "comment", "label": "意见"},
				},
			}},
			{ID: "done", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e_revise_2", Source: "visual_gate", Target: "review", SourceHandle: "revise"},
			{ID: "e1", Source: "input", Target: "visual_gate"},
			{ID: "e3", Source: "review", Target: "done"},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "visual_gate")
	waitRunStatus(t, db, run.ID, "waiting_human")

	img := models.PromptImage{Data: "iVBORw0KGgo=", MimeType: "image/png"}
	issue := models.PreviewIssue{
		ID: "iss-keep", RunID: run.ID, NodeID: "visual_gate",
		Body: "按钮太小", Selector: "#cta", Port: 0,
		Images: []models.PromptImage{img},
		Status: "open", CreatedAt: time.Now(),
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatalf("create issue: %v", err)
	}
	// Fail/revise snapshots vars then MarkResolved — comment-only must keep that snapshot.
	if err := eng.ResumeGate(run.ID, "visual_gate", "revise", nil); err != nil {
		t.Fatalf("resume visual_gate revise: %v", err)
	}
	waitGatePending(t, db, run.ID, "review")

	var before models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues").First(&before).Error; err != nil {
		t.Fatalf("preview_issues after visual gate: %v", err)
	}
	beforeCT := models.AsCompositeText(before.Value)
	if beforeCT == nil || !strings.Contains(beforeCT.Text, "按钮太小") {
		t.Fatalf("expected visual snapshot before comment gate: %#v", before.Value)
	}
	var marked models.PreviewIssue
	if err := db.First(&marked, "id = ?", issue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if marked.Status != "resolved" {
		t.Fatalf("after Fail snapshot status = %q, want resolved", marked.Status)
	}

	if err := eng.ResumeGate(run.ID, "review", "approve", map[string]any{"comment": "LGTM"}); err != nil {
		t.Fatalf("resume review: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var after models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues").First(&after).Error; err != nil {
		t.Fatalf("preview_issues after comment gate: %v", err)
	}
	afterCT := models.AsCompositeText(after.Value)
	if afterCT == nil || !strings.Contains(afterCT.Text, "[选中: #cta]") || !strings.Contains(afterCT.Text, "按钮太小") {
		t.Fatalf("comment-only gate wiped preview_issues: %#v", after.Value)
	}
	var countVar models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues_count").First(&countVar).Error; err != nil {
		t.Fatalf("preview_issues_count: %v", err)
	}
	if fmtNum(countVar.Value) != 1 {
		t.Fatalf("preview_issues_count = %v, want 1 (unchanged)", countVar.Value)
	}
}

// TestSnapshotOnlyIncludesOpenIssues: resolved rows must not re-enter the
// preview_issues snapshot on a later Fail/revise.
func TestSnapshotOnlyIncludesOpenIssues(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title": "网页预览", "output_var": "action",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions": []any{
					map[string]any{"id": "approve", "label": "通过"},
					map[string]any{"id": "revise", "label": "退回"},
				},
			}},
			{ID: "done", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e_revise_3", Source: "gate", Target: "done", SourceHandle: "revise"},
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "done", SourceHandle: "approve", Kind: models.EdgeSuccess},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")
	waitRunStatus(t, db, run.ID, "waiting_human")

	resolved := models.PreviewIssue{
		ID: "iss-old", RunID: run.ID, NodeID: "gate",
		Body: "already handled", Status: "resolved", CreatedAt: time.Now().Add(-time.Minute),
	}
	open := models.PreviewIssue{
		ID: "iss-new", RunID: run.ID, NodeID: "gate",
		Body: "new problem", Status: "open", CreatedAt: time.Now(),
	}
	if err := db.Create(&resolved).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&open).Error; err != nil {
		t.Fatal(err)
	}

	if err := eng.ResumeGate(run.ID, "gate", "revise", nil); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var issuesVar models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues").First(&issuesVar).Error; err != nil {
		t.Fatal(err)
	}
	text := ""
	if ct := models.AsCompositeText(issuesVar.Value); ct != nil {
		text = ct.Text
	} else if s, ok := issuesVar.Value.(string); ok {
		text = s
	}
	if strings.Contains(text, "already handled") {
		t.Fatalf("resolved issue leaked into snapshot: %q", text)
	}
	if !strings.Contains(text, "new problem") {
		t.Fatalf("open issue missing from snapshot: %q", text)
	}
	var countVar models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues_count").First(&countVar).Error; err != nil {
		t.Fatal(err)
	}
	if fmtNum(countVar.Value) != 1 {
		t.Fatalf("count = %v, want 1", countVar.Value)
	}
}

// TestPassForceClearsPreviewIssueVars: Pass/approve must clear snapshot vars and
// resolve residual open issues even when empty-list skip would otherwise keep them.
func TestPassForceClearsPreviewIssueVars(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title": "网页预览", "output_var": "action",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions": []any{
					map[string]any{"id": "approve", "label": "通过"},
					map[string]any{"id": "revise", "label": "退回"},
				},
			}},
			{ID: "done", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e_revise_4", Source: "gate", Target: "done", SourceHandle: "revise"},
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "done", SourceHandle: "approve", Kind: models.EdgeSuccess},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")
	waitRunStatus(t, db, run.ID, "waiting_human")

	// Seed stale vars as if a prior Fail left them (re-review with only resolved history).
	eng.persistVar(run.ID, "preview_issues", "1. stale fail text")
	eng.persistVar(run.ID, "preview_issues_count", 1)
	// Residual open that Pass must also resolve.
	residual := models.PreviewIssue{
		ID: "iss-residual", RunID: run.ID, NodeID: "gate",
		Body: "leftover", Status: "open", CreatedAt: time.Now(),
	}
	if err := db.Create(&residual).Error; err != nil {
		t.Fatal(err)
	}

	if err := eng.ResumeGate(run.ID, "gate", "approve", nil); err != nil {
		t.Fatalf("resume approve: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var countVar models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues_count").First(&countVar).Error; err != nil {
		t.Fatal(err)
	}
	if fmtNum(countVar.Value) != 0 {
		t.Fatalf("Pass preview_issues_count = %v, want 0", countVar.Value)
	}
	var issuesVar models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues").First(&issuesVar).Error; err != nil {
		t.Fatal(err)
	}
	if previewIssuesVarNonEmpty(issuesVar.Value) {
		t.Fatalf("Pass left non-empty preview_issues: %#v", issuesVar.Value)
	}
	var got models.PreviewIssue
	if err := db.First(&got, "id = ?", residual.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "resolved" {
		t.Fatalf("residual open after Pass = %q, want resolved", got.Status)
	}
}

// TestFailThenRereviewWithOnlyResolvedCanPass: after Fail→MarkResolved, a second
// visit with no new open issues can Pass even though vars still hold prior text
// until Pass clears them.
func TestFailThenRereviewWithOnlyResolvedCanPass(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title": "网页预览", "output_var": "action",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions": []any{
					map[string]any{"id": "approve", "label": "通过"},
					map[string]any{"id": "revise", "label": "退回"},
				},
			}},
			{ID: "done", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e_revise_5", Source: "gate", Target: "gate", SourceHandle: "revise"},
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "done", SourceHandle: "approve", Kind: models.EdgeSuccess},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")
	waitRunStatus(t, db, run.ID, "waiting_human")

	issue := models.PreviewIssue{
		ID: "iss-loop", RunID: run.ID, NodeID: "gate",
		Body: "fix spacing", Status: "open", CreatedAt: time.Now(),
	}
	if err := db.Create(&issue).Error; err != nil {
		t.Fatal(err)
	}
	if err := eng.ResumeGate(run.ID, "gate", "revise", nil); err != nil {
		t.Fatalf("first revise: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")
	waitRunStatus(t, db, run.ID, "waiting_human")

	var afterFail models.PreviewIssue
	if err := db.First(&afterFail, "id = ?", issue.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterFail.Status != "resolved" {
		t.Fatalf("after Fail status = %q, want resolved", afterFail.Status)
	}
	var midCount models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues_count").First(&midCount).Error; err != nil {
		t.Fatal(err)
	}
	if fmtNum(midCount.Value) != 1 {
		t.Fatalf("after Fail count = %v, want 1 (downstream still has snapshot)", midCount.Value)
	}

	// Re-review: no new open issues → Pass must succeed and clear vars.
	if err := eng.ResumeGate(run.ID, "gate", "approve", nil); err != nil {
		t.Fatalf("re-review approve: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	var finalCount models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues_count").First(&finalCount).Error; err != nil {
		t.Fatal(err)
	}
	if fmtNum(finalCount.Value) != 0 {
		t.Fatalf("after Pass count = %v, want 0", finalCount.Value)
	}
	var finalIssues models.RunVariable
	if err := db.Where("run_id = ? AND name = ?", run.ID, "preview_issues").First(&finalIssues).Error; err != nil {
		t.Fatal(err)
	}
	if previewIssuesVarNonEmpty(finalIssues.Value) {
		t.Fatalf("after Pass preview_issues still set: %#v", finalIssues.Value)
	}
}

// TestPreviewPathSkipsRequireFormValidation: page.html human_gate must skip
// validateGateForm even when the action sets requireForm and form is empty.
func TestPreviewPathSkipsRequireFormValidation(t *testing.T) {
	g := models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"title": "网页预览", "output_var": "action",
				"body_template": "{{nodes.visual.outputs.page}}",
				"actions": []any{
					map[string]any{"id": "approve", "label": "通过", "requireForm": true},
					map[string]any{"id": "revise", "label": "退回", "requireForm": true},
				},
				"form": []any{
					map[string]any{"key": "comment", "label": "评审意见", "required": true},
				},
			}},
			{ID: "done", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e_revise_6", Source: "gate", Target: "done", SourceHandle: "revise"},
			{ID: "e1", Source: "input", Target: "gate"},
			{ID: "e2", Source: "gate", Target: "done", SourceHandle: "approve", Kind: models.EdgeSuccess},
		},
	}
	eng, db := setupEngineGraph(t, g)
	run, err := eng.StartRun("wf", nil, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitGatePending(t, db, run.ID, "gate")
	waitRunStatus(t, db, run.ID, "waiting_human")

	if err := eng.ResumeGate(run.ID, "gate", "revise", map[string]any{"comment": ""}); err != nil {
		t.Fatalf("preview path revise with empty form should succeed: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")
}

func TestShouldSnapshotPreviewIssues(t *testing.T) {
	if shouldSnapshotPreviewIssues(&models.Node{Type: "agent", Caps: capsPreview}) {
		t.Fatal("preview review agent should not snapshot via Gate resume")
	}
	if !shouldSnapshotPreviewIssues(&models.Node{
		Type:   "human_gate",
		Config: map[string]any{"body_template": "{{nodes.visual.outputs.page}}"},
	}) {
		t.Fatal("page.html human_gate should snapshot")
	}
	if shouldSnapshotPreviewIssues(&models.Node{
		Type:   "human_gate",
		Config: map[string]any{"body_template": "{{nodes.research.outputs.research}}"},
	}) {
		t.Fatal("research human_gate should not snapshot")
	}
	if shouldSnapshotPreviewIssues(&models.Node{Type: "human_gate", Config: map[string]any{}}) {
		t.Fatal("empty body human_gate should not snapshot")
	}
}

func fmtNum(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return -1
	}
}
