package services

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// nameShape mirrors pmArtifactNameRe: every ledger name must satisfy the
// platform's citation name shape or it cannot be cited or read back.
var nameShape = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*\.[a-z0-9]{1,16}$`)

func TestFeedbackArtifactNameShapeAndNodeIterationUniqueness(t *testing.T) {
	cases := []struct{ kind, node string }{
		{"review", "research-1"},
		{"clarify", "clarify_1"},
		{"gate", "Gate/One"},
		{"preview", "  "},
		{"weird", strings.Repeat("x", 80)},
	}
	seen := map[string]string{}
	for _, c := range cases {
		got := FeedbackArtifactName(c.kind, c.node, 2, 3)
		if !nameShape.MatchString(got) {
			t.Fatalf("name %q does not match the citation shape", got)
		}
		if prev, dup := seen[got]; dup {
			t.Fatalf("name collision: %q from %q and %q", got, prev, c.node)
		}
		seen[got] = c.node
		if !IsFeedbackArtifactName(got) {
			t.Fatalf("%q should be recognized as a ledger name", got)
		}
	}

	// The readable case stays readable — no digest suffix when nothing is lost.
	if got := FeedbackArtifactName("review", "research-1", 2, 3); got != "feedback.review.research-1.i2.json" {
		t.Fatalf("clean node id should not be mangled, got %q", got)
	}
	if FeedbackArtifactName("review", "research-1", 2, 1) != FeedbackArtifactName("review", "research-1", 2, 3) {
		t.Fatal("ReAct rounds in one iteration must share one product name")
	}
	if FeedbackArtifactName("gate", "research-1", 2, 1) == FeedbackArtifactName("gate", "research-1", 2, 3) {
		t.Fatal("gate rounds must keep their distinct product names")
	}
	// Case folding and separator replacement must not merge distinct ids.
	if FeedbackArtifactName("review", "Foo", 1, 1) == FeedbackArtifactName("review", "foo", 1, 1) {
		t.Fatal("Foo and foo must not share an artifact name")
	}
	if FeedbackArtifactName("review", "a/b", 1, 1) == FeedbackArtifactName("review", "a-b", 1, 1) {
		t.Fatal("a/b and a-b must not share an artifact name")
	}
}

func TestFeedbackArtifactNameNormalizesOrdinals(t *testing.T) {
	if got := FeedbackArtifactName("review", "n", 0, 0); got != "feedback.review.n.i1.json" {
		t.Fatalf("non-positive ordinals should floor to 1, got %q", got)
	}
	if !IsFeedbackArtifactName(FeedbackIndexArtifactName) {
		t.Fatal("index must be recognized as a ledger name")
	}
	if !IsFeedbackArtifactName("feedback.review.n.i1r1.json") {
		t.Fatal("legacy per-round feedback must remain readable")
	}
	if IsFeedbackArtifactName("research.json") {
		t.Fatal("a normal product must not be treated as ledger")
	}
}

func TestAppendRejectsEventsWithoutSubstance(t *testing.T) {
	svc := NewFeedbackService(newTestDB(t))
	// A gate approval clicked through with an empty form.
	ev := models.FeedbackEvent{RunID: "r1", Kind: models.FeedbackKindGate, NodeID: "gate1", Iteration: 1, Action: "pass"}
	if err := svc.Append(&ev); err != ErrFeedbackNoSubstance {
		t.Fatalf("empty approval should be rejected, got %v", err)
	}
	if got := svc.Events("r1"); len(got) != 0 {
		t.Fatalf("no row should exist, got %d", len(got))
	}
	if err := svc.Append(nil); err == nil {
		t.Fatal("nil event should error")
	}
}

func TestAppendAssignsSeqAndRoundPerNodeIteration(t *testing.T) {
	svc := NewFeedbackService(newTestDB(t))
	mk := func(node string, iter int, text string) models.FeedbackEvent {
		return models.FeedbackEvent{RunID: "r1", Kind: models.FeedbackKindReview,
			NodeID: node, Iteration: iter, Text: text}
	}
	for _, text := range []string{"一", "二", "三"} {
		ev := mk("research-1", 2, text)
		if err := svc.Append(&ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	other := mk("impl-1", 1, "别的节点")
	if err := svc.Append(&other); err != nil {
		t.Fatalf("append other: %v", err)
	}

	events := svc.Events("r1")
	if len(events) != 4 {
		t.Fatalf("want 4 events, got %d", len(events))
	}
	// Seq is run-global and monotonic; Round restarts per (node, iteration).
	names := map[string]bool{}
	for i, ev := range events {
		if ev.Seq != i+1 {
			t.Fatalf("event %d has seq %d", i, ev.Seq)
		}
		names[ev.ArtifactName] = true
	}
	for i, want := range []int{1, 2, 3} {
		if events[i].Round != want {
			t.Fatalf("round %d = %d, want %d", i, events[i].Round, want)
		}
	}
	if events[3].Round != 1 {
		t.Fatalf("a different node starts a fresh round counter, got %d", events[3].Round)
	}
	// ReAct rounds share a node+iteration product; another node gets its own.
	if len(names) != 2 {
		t.Fatalf("want 2 distinct artifact names, got %d: %v", len(names), names)
	}
}

func TestAppendKeepsIndexOnlyRoundsWithoutArtifact(t *testing.T) {
	svc := NewFeedbackService(newTestDB(t))
	ev := models.FeedbackEvent{RunID: "r1", Kind: models.FeedbackKindClarify, NodeID: "c1",
		Iteration: 1, Action: "auto_answer", Text: "缓存=Redis", IndexOnly: true}
	if err := svc.Append(&ev); err != nil {
		t.Fatalf("append: %v", err)
	}
	if ev.ArtifactName != "" {
		t.Fatalf("index-only round must not claim a product, got %q", ev.ArtifactName)
	}
	if ev.Round != 1 || ev.Seq != 1 {
		t.Fatalf("ordinals still assigned: seq=%d round=%d", ev.Seq, ev.Round)
	}
}

func TestAppendStripsInlineAttachmentData(t *testing.T) {
	svc := NewFeedbackService(newTestDB(t))
	ev := models.FeedbackEvent{
		RunID: "r1", Kind: models.FeedbackKindReview, NodeID: "n1", Iteration: 1,
		Text:        "看截图",
		Attachments: []models.PromptImage{{Ref: "blob:abc", Name: "s.png", MimeType: "image/png", Data: "AAAABBBB"}},
		Turns: []models.ReactMessage{
			{Role: "human", Text: "看截图", Images: []models.PromptImage{{Ref: "blob:abc", Data: "AAAABBBB"}}},
		},
	}
	if err := svc.Append(&ev); err != nil {
		t.Fatalf("append: %v", err)
	}
	stored := svc.Events("r1")[0]
	if stored.Attachments[0].Data != "" || stored.Turns[0].Images[0].Data != "" {
		t.Fatal("inline base64 must never be persisted; only blob refs")
	}
	if stored.Attachments[0].Ref != "blob:abc" {
		t.Fatalf("ref lost: %+v", stored.Attachments[0])
	}

	body, err := MarshalRoundJSON(stored, nil, "r1", NodeRef{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(body, "AAAABBBB") || strings.Contains(body, "data:image") {
		t.Fatalf("rendered product must not inline attachment bytes:\n%s", body)
	}
}

func TestAppendTruncatesOversizeText(t *testing.T) {
	svc := NewFeedbackService(newTestDB(t))
	ev := models.FeedbackEvent{RunID: "r1", Kind: models.FeedbackKindReview, NodeID: "n1", Iteration: 1,
		Text:  strings.Repeat("x", feedbackTextMax+500),
		Turns: []models.ReactMessage{{Role: "human", Text: strings.Repeat("y", feedbackTurnMax+500)}},
	}
	if err := svc.Append(&ev); err != nil {
		t.Fatalf("append: %v", err)
	}
	stored := svc.Events("r1")[0]
	if len(stored.Text) > feedbackTextMax+8 {
		t.Fatalf("text not truncated: %d", len(stored.Text))
	}
	if len(stored.Turns[0].Text) > feedbackTurnMax+8 {
		t.Fatalf("turn not truncated: %d", len(stored.Turns[0].Text))
	}
}

func TestMarshalRoundJSONWritesAgentSummaryWhenPresent(t *testing.T) {
	at := time.Date(2026, 8, 13, 15, 7, 22, 0, time.UTC)
	cur := models.FeedbackEvent{
		RunID: "run-1", Kind: models.FeedbackKindReview, NodeID: "research-1", Iteration: 1, Round: 1, Seq: 1,
		OccurredAt: at, Action: "revise", Text: "除了聊天记录上面需要总结一下",
		AgentSummary: "用户希望在单轮卡片最前增加 Agent 对反馈的总结。",
		Turns:        []models.ReactMessage{{Role: "human", Text: "需要总结"}, {Role: "agent", Text: "已加总结区"}},
	}
	body, err := MarshalRoundJSON(cur, nil, "run-1", NodeRef{Label: "调研", Type: "agent"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc["agentSummary"] != "用户希望在单轮卡片最前增加 Agent 对反馈的总结。" {
		t.Fatalf("agentSummary = %v", doc["agentSummary"])
	}
	// Must not confuse with index gist (FeedbackSummary).
	if FeedbackSummary(cur) == doc["agentSummary"] {
		t.Fatal("agentSummary must stay distinct from FeedbackSummary gist")
	}
}

func TestMarshalRoundJSONOmitsEmptyAgentSummary(t *testing.T) {
	at := time.Now()
	cur := models.FeedbackEvent{
		RunID: "run-1", Kind: models.FeedbackKindClarify, NodeID: "c1", Iteration: 1, Round: 1, Seq: 1,
		OccurredAt: at, Text: "选 Redis",
	}
	body, err := MarshalRoundJSON(cur, nil, "run-1", NodeRef{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := doc["agentSummary"]; ok {
		t.Fatalf("empty agentSummary must be omitted, got %v", doc["agentSummary"])
	}
}

func TestMarshalFeedbackSummaryJSONCoversAllRoundsWithoutTranscript(t *testing.T) {
	at := time.Date(2026, 8, 13, 15, 7, 22, 0, time.UTC)
	events := []models.FeedbackEvent{
		{RunID: "run-1", Kind: models.FeedbackKindReview, NodeID: "research-1", Iteration: 1, Round: 1,
			OccurredAt: at, Text: "补充竞品对比", Turns: []models.ReactMessage{{Role: "human", Text: "补充竞品对比"}}},
		{RunID: "run-1", Kind: models.FeedbackKindReview, NodeID: "research-1", Iteration: 1, Round: 2,
			OccurredAt: at.Add(time.Minute), AgentSummary: "图表改为柱状图"},
	}
	body, err := MarshalFeedbackSummaryJSON(events, "run-1", NodeRef{Label: "调研", Type: "agent"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	summary, _ := doc["summary"].(string)
	for _, want := range []string{"补充竞品对比", "图表改为柱状图"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
	if doc["roundCount"] != float64(2) || doc["latestRound"] != float64(2) {
		t.Fatalf("round coverage = %#v", doc)
	}
	if _, ok := doc["transcript"]; ok {
		t.Fatalf("cumulative product must not force a full transcript: %s", body)
	}
}

// Ordinary ReAct turns carry no summary; the confirm round is the one that
// does, so the cumulative product must surface it at the top level for the
// card's「Agent 总结」section — and must stay silent when no round produced one.
func TestMarshalFeedbackSummaryJSONSurfacesConfirmRoundSummary(t *testing.T) {
	at := time.Date(2026, 8, 13, 15, 7, 22, 0, time.UTC)
	rounds := []models.FeedbackEvent{
		{RunID: "run-1", Kind: models.FeedbackKindClarify, NodeID: "approve-1", Iteration: 1, Round: 1,
			OccurredAt: at, Text: "视觉跟截图"},
		{RunID: "run-1", Kind: models.FeedbackKindClarify, NodeID: "approve-1", Iteration: 1, Round: 2,
			OccurredAt: at.Add(time.Minute), Text: "就这样",
			AgentSummary: "用户确认按截图保留视觉,去掉下拉与紫色选中。",
			Detail:       map[string]any{"confirm": true}},
	}
	doc := decodeSummaryDoc(t, rounds)
	if doc["agentSummary"] != "用户确认按截图保留视觉,去掉下拉与紫色选中。" {
		t.Fatalf("agentSummary = %v", doc["agentSummary"])
	}
	// The per-round gists remain the body, so the dialogue is still readable.
	if s, _ := doc["summary"].(string); !strings.Contains(s, "视觉跟截图") {
		t.Fatalf("summary must keep every round's gist: %s", s)
	}

	// An interrupted round appended after the confirm must not hide the summary.
	rounds = append(rounds, models.FeedbackEvent{RunID: "run-1", Kind: models.FeedbackKindClarify,
		NodeID: "approve-1", Iteration: 1, Round: 3, OccurredAt: at.Add(2 * time.Minute),
		Text: "再看一眼", Interrupted: true})
	if got := decodeSummaryDoc(t, rounds)["agentSummary"]; got != "用户确认按截图保留视觉,去掉下拉与紫色选中。" {
		t.Fatalf("agentSummary lost behind a later round: %v", got)
	}

	// No round produced one (summary turn skipped / unparseable) ⇒ no key, so
	// the card hides the section instead of showing an invented induction.
	plain := decodeSummaryDoc(t, []models.FeedbackEvent{{RunID: "run-1",
		Kind: models.FeedbackKindClarify, NodeID: "approve-1", Iteration: 1, Round: 1,
		OccurredAt: at, Text: "视觉跟截图"}})
	if _, ok := plain["agentSummary"]; ok {
		t.Fatalf("empty agentSummary must be omitted, got %v", plain["agentSummary"])
	}
}

func decodeSummaryDoc(t *testing.T, events []models.FeedbackEvent) map[string]any {
	t.Helper()
	body, err := MarshalFeedbackSummaryJSON(events, "run-1", NodeRef{Label: "需求对齐", Type: "agent"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	return doc
}

func TestMarshalIndexJSONStillUsesFeedbackSummaryNotAgentSummary(t *testing.T) {
	at := time.Now()
	events := []models.FeedbackEvent{{
		Seq: 1, Round: 1, Kind: models.FeedbackKindReview, NodeID: "n1", Iteration: 1, OccurredAt: at,
		Text: "第一行正文\n第二行", AgentSummary: "这是卡片 Agent 总结,不应进索引",
		ArtifactName: "feedback.review.n1.i1r1.json",
	}}
	body, err := MarshalIndexJSON("run-1", events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(body, "这是卡片 Agent 总结") {
		t.Fatalf("index must keep FeedbackSummary gist, not agentSummary:\n%s", body)
	}
	if !strings.Contains(body, "第一行正文") {
		t.Fatalf("index gist missing:\n%s", body)
	}
}

func TestMarshalRoundJSONCarriesPriorRoundsAndPrevPointer(t *testing.T) {
	at := time.Date(2026, 8, 13, 15, 7, 22, 0, time.UTC)
	prior := []models.FeedbackEvent{
		{Round: 1, Kind: "review", Text: "要求补充竞品对比", OccurredAt: at, ArtifactName: "feedback.review.r1.i2r1.json"},
		{Round: 2, Kind: "clarify", Text: "图表改用柱状图", OccurredAt: at, IndexOnly: true},
	}
	cur := models.FeedbackEvent{
		RunID: "run-1", Kind: models.FeedbackKindReview, NodeID: "research-1", Iteration: 2, Round: 3, Seq: 7,
		OccurredAt: at, Actor: "alice", CallerKind: "pm", Action: "revise",
		Text:        "第 3 条结论证据不足",
		Annotations: []models.ReactAnnotation{{JSONPath: "$.findings[2]", Note: "补链接"}},
		Targets:     []models.FeedbackTarget{{Name: "research.json", Before: "ab", After: "cd", Changed: true}},
		Detail:      map[string]any{"source": "gate"},
		Turns:       []models.ReactMessage{{Role: "human", Text: "补链接"}, {Role: "agent", Text: "已补充"}},
	}
	body, err := MarshalRoundJSON(cur, prior, "run-1", NodeRef{Label: "调研", Type: "agent"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// prev must skip the index-only round rather than dangle on it.
	if doc["prev"] != "feedback.review.r1.i2r1.json" {
		t.Fatalf("prev = %v", doc["prev"])
	}
	if got := doc["priorRounds"].([]any); len(got) != 2 {
		t.Fatalf("priorRounds = %v", got)
	}
	node := doc["node"].(map[string]any)
	if node["id"] != "research-1" || node["label"] != "调研" {
		t.Fatalf("node = %v", node)
	}
	if doc["index"] != FeedbackIndexArtifactName {
		t.Fatalf("index pointer = %v", doc["index"])
	}
	for _, k := range []string{"feedback", "transcript", "targets", "detail", "actor"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("missing %q in:\n%s", k, body)
		}
	}
}

func TestMarshalIndexJSONListsEveryRound(t *testing.T) {
	at := time.Now()
	events := []models.FeedbackEvent{
		{Seq: 1, Round: 1, Kind: models.FeedbackKindClarify, NodeID: "c1", Iteration: 1, OccurredAt: at,
			Action: "auto_answer", Actor: "system", Text: "缓存=Redis"},
		{Seq: 2, Round: 1, Kind: models.FeedbackKindReview, NodeID: "r1", Iteration: 1, OccurredAt: at,
			Text: "证据不足", ArtifactName: "feedback.review.r1.i1r1.json",
			Attachments: []models.PromptImage{{Ref: "blob:x"}}, Interrupted: true},
	}
	body, err := MarshalIndexJSON("run-1", events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var doc struct {
		TotalRounds int            `json:"totalRounds"`
		Counts      map[string]int `json:"counts"`
		Rounds      []struct {
			Kind        string `json:"kind"`
			Summary     string `json:"summary"`
			Artifact    string `json:"artifact"`
			Attachments int    `json:"attachments"`
			Interrupted bool   `json:"interrupted"`
		} `json:"rounds"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.TotalRounds != 2 || len(doc.Rounds) != 2 {
		t.Fatalf("index must list every round, got %d", doc.TotalRounds)
	}
	if doc.Counts["clarify"] != 1 || doc.Counts["review"] != 1 {
		t.Fatalf("counts = %v", doc.Counts)
	}
	// The auto-answered round is listed but carries no product pointer.
	if doc.Rounds[0].Artifact != "" {
		t.Fatalf("index-only round must not cite an artifact: %q", doc.Rounds[0].Artifact)
	}
	if doc.Rounds[1].Artifact == "" || doc.Rounds[1].Attachments != 1 || !doc.Rounds[1].Interrupted {
		t.Fatalf("round 2 = %+v", doc.Rounds[1])
	}
	if doc.Rounds[1].Summary != "证据不足" {
		t.Fatalf("summary = %q", doc.Rounds[1].Summary)
	}
}

func TestFeedbackSummaryFallsBackThroughSources(t *testing.T) {
	cases := []struct {
		name string
		ev   models.FeedbackEvent
		want string
	}{
		{"text wins", models.FeedbackEvent{Text: "\n\n第一行\n第二行"}, "第一行"},
		{"annotation note", models.FeedbackEvent{
			Annotations: []models.ReactAnnotation{{Note: "补链接"}}}, "补链接"},
		{"human turn", models.FeedbackEvent{
			Turns: []models.ReactMessage{{Role: "agent", Text: "机器"}, {Role: "human", Text: "人说的"}}}, "人说的"},
		{"attachment only", models.FeedbackEvent{
			Attachments: []models.PromptImage{{Ref: "blob:x"}}}, "(仅附件)"},
		{"nothing", models.FeedbackEvent{}, "(无正文)"},
	}
	for _, c := range cases {
		if got := FeedbackSummary(c.ev); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestFeedbackDigestDetectsChange(t *testing.T) {
	if FeedbackDigest("") != "" {
		t.Fatal("empty content has no digest")
	}
	a, b := FeedbackDigest("x"), FeedbackDigest("y")
	if a == b || len(a) != 16 {
		t.Fatalf("digest a=%q b=%q", a, b)
	}
	if FeedbackDigest("x") != a {
		t.Fatal("digest must be stable")
	}
}

func TestRunServiceFeedbackEventsSatisfiesHistoryProvider(t *testing.T) {
	db := newTestDB(t)
	ev := models.FeedbackEvent{RunID: "r1", Kind: models.FeedbackKindReview, NodeID: "n1", Iteration: 1, Text: "改"}
	if err := NewFeedbackService(db).Append(&ev); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got := NewRunService(db).FeedbackEvents("r1"); len(got) != 1 || got[0].Text != "改" {
		t.Fatalf("provider read = %+v", got)
	}
}
