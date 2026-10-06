package engine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

func feedbackArtifacts(db *gorm.DB, runID string) []models.Artifact {
	var all []models.Artifact
	db.Where("run_id = ?", runID).Order("name").Find(&all)
	out := make([]models.Artifact, 0, len(all))
	for _, a := range all {
		if strings.HasPrefix(a.Name, services.FeedbackArtifactPrefix) {
			out = append(out, a)
		}
	}
	return out
}

// feedbackArtifactsOfKind narrows the ledger to one kind. Reaching a gate goes
// through a clarify「确认并流转」, which is itself a recorded round now, so a test
// about gate or review products must not assert over the whole ledger.
func feedbackArtifactsOfKind(db *gorm.DB, runID, kind string) []models.Artifact {
	out := make([]models.Artifact, 0)
	for _, a := range feedbackArtifacts(db, runID) {
		if strings.HasPrefix(a.Name, services.FeedbackArtifactPrefix+kind+".") {
			out = append(out, a)
		}
	}
	return out
}

func decodeIndex(t *testing.T, db *gorm.DB, runID string) map[string]any {
	t.Helper()
	a, ok := arts(db, runID, services.FeedbackIndexArtifactName)
	if !ok {
		t.Fatalf("%s missing", services.FeedbackIndexArtifactName)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(a.Content), &doc); err != nil {
		t.Fatalf("index is not valid JSON: %v", err)
	}
	return doc
}

func indexRounds(t *testing.T, db *gorm.DB, runID string) []map[string]any {
	t.Helper()
	list, _ := decodeIndex(t, db, runID)["rounds"].([]any)
	out := make([]map[string]any, 0, len(list))
	for _, r := range list {
		m, _ := r.(map[string]any)
		out = append(out, m)
	}
	return out
}

// Three push-backs on one execution must converge on one cumulative product.
// The individual reasoning remains in FeedbackEvent and feedback_index.json.
func TestReviseRoundsProduceOneCumulativeArtifact(t *testing.T) {
	eng, db, _ := setupReviewEngine(t)

	run, err := eng.StartRun("review-wf", map[string]any{"idea": "登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "prop")
	waitRunStatus(t, db, run.ID, "waiting_human")

	opinions := []string{"第一条要补证据", "第二条图表改柱状图", "第三条补上原始链接"}
	anns := []models.ReactAnnotation{{JSONPath: "findings[r1]", Note: "更具体"}}
	for _, text := range opinions {
		if err := eng.ReactReply(run.ID, "prop", text, nil, anns, false); err != nil {
			t.Fatalf("revise %q: %v", text, err)
		}
		if err := eng.waitReviewReadyForTest(run.ID, "prop", 5*time.Second); err != nil {
			t.Fatalf("wait revise %q: %v", text, err)
		}
	}

	rounds := feedbackArtifacts(db, run.ID)
	if len(rounds) != 1 {
		t.Fatalf("want 1 cumulative product, got %d: %+v", len(rounds), rounds)
	}
	a := rounds[0]
	if a.Name != "feedback.review.prop.i1.json" {
		t.Fatalf("unexpected cumulative name %q", a.Name)
	}
	for _, opinion := range opinions {
		if !strings.Contains(a.Content, opinion) {
			t.Fatalf("%s does not carry all rounds' conclusions:\n%s", a.Name, a.Content)
		}
	}
	if a.NodeID != "prop" {
		t.Fatalf("%s should hang off the producer node, got %q", a.Name, a.NodeID)
	}
	if !strings.Contains(a.Content, `"roundCount": 3`) {
		t.Fatalf("cumulative product must record all rounds:\n%s", a.Content)
	}

	idx := decodeIndex(t, db, run.ID)
	list, _ := idx["rounds"].([]any)
	if len(list) != 3 {
		t.Fatalf("index must list all 3 rounds, got %d", len(list))
	}
	for _, r := range list {
		m := r.(map[string]any)
		if m["artifact"] == "" || m["artifact"] == nil {
			t.Fatalf("index row missing artifact pointer: %+v", m)
		}
		if m["artifact"] != a.Name {
			t.Fatalf("index must point every ReAct round at the shared product: %+v", m)
		}
	}
	// The producer's own deliverable survived: feedback products must not be
	// mistaken for "this node already produced something".
	if _, ok := arts(db, run.ID, mcp.ResearchArtifactName); !ok {
		t.Fatal("research.json missing — feedback products suppressed the deliverable")
	}
}

// A reviewer who clicks through without writing anything has not given
// feedback; recording it would flood the run with empty shells.
func TestEmptyGateApprovalWritesNoFeedbackProduct(t *testing.T) {
	eng, db := setupEngine(t)
	run := runToGate(t, eng, db)

	if err := eng.ResumeGate(run.ID, "approve", "approve", map[string]any{}); err != nil {
		t.Fatalf("resume gate: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	if got := feedbackArtifactsOfKind(db, run.ID, "gate"); len(got) != 0 {
		t.Fatalf("empty approval must not produce feedback files, got %+v", got)
	}
	var n int64
	db.Model(&models.FeedbackEvent{}).
		Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindGate).Count(&n)
	if n != 0 {
		t.Fatalf("no row should be written either, got %d", n)
	}
	// The index exists because the clarify confirm round is real feedback, but
	// the empty approval must be absent from it.
	for _, r := range indexRounds(t, db, run.ID) {
		if r["kind"] == models.FeedbackKindGate {
			t.Fatalf("empty approval must not be indexed: %+v", r)
		}
	}
}

// The same click WITH a written comment is real feedback and must be kept,
// attributed to the reviewer who wrote it.
func TestGateApprovalWithCommentRecordsOneRound(t *testing.T) {
	eng, db := setupEngine(t)
	run := runToGate(t, eng, db)

	form := map[string]any{"comment": "同意，但下轮请补上压测数据", "checked": true}
	if err := eng.ResumeGateAs(run.ID, "approve", "approve", form, "alice"); err != nil {
		t.Fatalf("resume gate: %v", err)
	}
	waitRunStatus(t, db, run.ID, "completed")

	rounds := feedbackArtifactsOfKind(db, run.ID, "gate")
	if len(rounds) != 1 {
		t.Fatalf("want exactly one gate round, got %d: %+v", len(rounds), rounds)
	}
	body := rounds[0].Content
	for _, want := range []string{"压测数据", "alice", "\"kind\": \"gate\""} {
		if !strings.Contains(body, want) {
			t.Fatalf("gate round missing %q:\n%s", want, body)
		}
	}
	// A boolean checkbox is a decision, not an opinion — it stays out of the body.
	var doc struct {
		Feedback struct {
			Text string `json:"text"`
		} `json:"feedback"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("round is not valid JSON: %v", err)
	}
	if strings.Contains(doc.Feedback.Text, "checked") {
		t.Fatalf("non-text form values must not enter the opinion body: %q", doc.Feedback.Text)
	}

	// The clarify「确认并流转」round precedes this approval in the same run.
	idx := decodeIndex(t, db, run.ID)
	if got := idx["totalRounds"]; got != float64(2) {
		t.Fatalf("index totalRounds = %v", got)
	}
}

// runToGate drives the standard test workflow up to its pending human gate.
func runToGate(t *testing.T, eng *Engine, db *gorm.DB) models.Run {
	t.Helper()
	run, err := eng.StartRun("clarify-to-design", map[string]any{"idea": "做个登录"}, "test")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waitReactPause(t, db, run.ID, "clarify")
	if err := eng.ReactReply(run.ID, "clarify", "就这样", nil, nil, true); err != nil {
		t.Fatalf("finish clarify: %v", err)
	}
	waitGatePending(t, db, run.ID, "approve")
	return *run
}

// Attachments are blob references. Inlining base64 into a text product would
// bloat every read of the ledger and duplicate bytes the blob store already
// holds, so the payload must never make it to disk.
func TestFeedbackAttachmentsStayReferences(t *testing.T) {
	eng, db := setupEngine(t)
	run := runToGate(t, eng, db)

	const payload = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk"
	eng.recordFeedback(models.FeedbackEvent{
		RunID: run.ID, Kind: models.FeedbackKindReview, NodeID: "review_design",
		Iteration: 1, Actor: "alice", Action: "revise", Text: "看截图里的这处",
		Attachments: []models.PromptImage{{
			Ref: "blob:abc123", Name: "shot.png", MimeType: "image/png",
			SizeBytes: 48213, Data: payload,
		}},
		Turns: []models.ReactMessage{{Role: "human", Text: "看截图",
			Images: []models.PromptImage{{Ref: "blob:abc123", Data: payload}}}},
	})

	rounds := feedbackArtifactsOfKind(db, run.ID, "review")
	if len(rounds) != 1 {
		t.Fatalf("want one round, got %d", len(rounds))
	}
	body := rounds[0].Content
	if strings.Contains(body, payload) || strings.Contains(body, "data:image") {
		t.Fatalf("attachment bytes leaked into the product:\n%s", body)
	}
	if !strings.Contains(body, "blob:abc123") || !strings.Contains(body, "shot.png") {
		t.Fatalf("attachment reference missing from the product:\n%s", body)
	}

	var row models.FeedbackEvent
	if err := db.Where("run_id = ? AND kind = ?", run.ID, models.FeedbackKindReview).
		First(&row).Error; err != nil {
		t.Fatalf("load row: %v", err)
	}
	if row.Attachments[0].Data != "" || row.Turns[0].Images[0].Data != "" {
		t.Fatal("base64 must be stripped before the row is written")
	}
}

// The ledger is evidence of what a human actually said. Letting a reviewer edit
// it through the gate product editor — the same path a share-link holder uses —
// would make it unciteable.
func TestFeedbackProductsAreNotGateEditable(t *testing.T) {
	eng, db := setupEngine(t)
	run := runToGate(t, eng, db)

	eng.recordFeedback(models.FeedbackEvent{
		RunID: run.ID, Kind: models.FeedbackKindGate, NodeID: "approve",
		Iteration: 1, Actor: "alice", Action: "revise", Text: "补上压测数据",
	})
	rounds := feedbackArtifactsOfKind(db, run.ID, "gate")
	if len(rounds) != 1 {
		t.Fatalf("want one round, got %d", len(rounds))
	}

	names := []string{rounds[0].Name, services.FeedbackIndexArtifactName}
	for _, name := range names {
		if _, err := eng.SaveGateArtifact(run.ID, "approve", name, `{"x":1}`, ""); err == nil {
			t.Fatalf("%s must not be editable through the gate editor", name)
		}
	}

	// It is also absent from what a share-link holder is offered at all.
	items, err := eng.ListGatePrimaryProducts(run.ID, "approve")
	if err != nil {
		t.Fatalf("list primaries: %v", err)
	}
	for _, it := range items {
		if services.IsFeedbackArtifactName(it.Name) {
			t.Fatalf("feedback product exposed as a gate primary: %q", it.Name)
		}
	}
}

func TestRecordFeedbackIgnoresEventsWithoutSubstance(t *testing.T) {
	eng, db := setupEngine(t)
	// No run row needed: the guard must reject before any write happens.
	eng.recordFeedback(models.FeedbackEvent{RunID: "run-x", Kind: models.FeedbackKindGate,
		NodeID: "approve", Iteration: 1, Action: "pass"})
	eng.recordFeedback(models.FeedbackEvent{})

	var n int64
	db.Model(&models.FeedbackEvent{}).Count(&n)
	if n != 0 {
		t.Fatalf("substance-free events must never reach the table, got %d", n)
	}
}

func TestDiffDigestsMarksOnlyRealChanges(t *testing.T) {
	before := map[string]string{"a.json": "1", "gone.json": "9"}
	after := map[string]string{"a.json": "2", "new.json": "3"}
	got := diffDigests(before, after)
	if len(got) != 3 {
		t.Fatalf("want 3 targets, got %+v", got)
	}
	byName := map[string]models.FeedbackTarget{}
	for _, tg := range got {
		byName[tg.Name] = tg
	}
	if !byName["a.json"].Changed || byName["a.json"].Before != "1" || byName["a.json"].After != "2" {
		t.Fatalf("edited product = %+v", byName["a.json"])
	}
	if !byName["new.json"].Changed || byName["new.json"].Before != "" {
		t.Fatalf("added product = %+v", byName["new.json"])
	}
	if !byName["gone.json"].Changed || byName["gone.json"].After != "" {
		t.Fatalf("removed product = %+v", byName["gone.json"])
	}
	// Identical snapshots report no change.
	same := diffDigests(map[string]string{"a.json": "1"}, map[string]string{"a.json": "1"})
	if len(same) != 1 || same[0].Changed {
		t.Fatalf("unchanged product must not be marked changed: %+v", same)
	}
}

func TestGateFormTextJoinsOnlyWrittenValues(t *testing.T) {
	if got := gateFormText(map[string]any{"ok": true, "n": 3, "blank": "  "}); got != "" {
		t.Fatalf("no written text should yield an empty body, got %q", got)
	}
	got := gateFormText(map[string]any{"b": "第二", "a": "第一", "ok": true})
	if got != "a: 第一\nb: 第二" {
		t.Fatalf("form body = %q", got)
	}
}

func TestLastAgentQuestionsPicksMostRecentTurn(t *testing.T) {
	msgs := []models.ReactMessage{
		{Role: "agent", Questions: []models.ReactQuestion{{ID: "q1"}}},
		{Role: "human", Text: "答"},
		{Role: "agent", Questions: []models.ReactQuestion{{ID: "q2"}}},
	}
	got := lastAgentQuestions(msgs)
	if len(got) != 1 || got[0].ID != "q2" {
		t.Fatalf("want the latest agent turn's questions, got %+v", got)
	}
	if lastAgentQuestions([]models.ReactMessage{{Role: "human"}}) != nil {
		t.Fatal("no agent turn ⇒ no questions")
	}
}
