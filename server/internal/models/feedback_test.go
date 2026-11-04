package models

import (
	"strings"
	"testing"
)

// HasSubstance is the single gate deciding whether a round is recorded at all,
// so each of the qualifying inputs is pinned individually.
func TestFeedbackEventHasSubstance(t *testing.T) {
	cases := []struct {
		name string
		ev   FeedbackEvent
		want bool
	}{
		{"empty gate approval", FeedbackEvent{Kind: FeedbackKindGate, Action: "pass"}, false},
		{"blank text only", FeedbackEvent{Text: "   \n\t "}, false},
		{"empty turns", FeedbackEvent{Turns: []ReactMessage{{Role: "agent", Text: "  "}}}, false},
		{"opinion text", FeedbackEvent{Text: "证据不足"}, true},
		{"annotation", FeedbackEvent{Annotations: []ReactAnnotation{{JSONPath: "$.a"}}}, true},
		{"attachment", FeedbackEvent{Attachments: []PromptImage{{Ref: "blob:x"}}}, true},
		{"react turn", FeedbackEvent{Turns: []ReactMessage{{Role: "human", Text: "改这里"}}}, true},
		{"turn with questions only", FeedbackEvent{
			Turns: []ReactMessage{{Role: "agent", Questions: []ReactQuestion{{Prompt: "选哪个?"}}}}}, true},
		{"turn with image only", FeedbackEvent{
			Turns: []ReactMessage{{Role: "human", Images: []PromptImage{{Ref: "blob:x"}}}}}, true},
	}
	for _, c := range cases {
		if got := c.ev.HasSubstance(); got != c.want {
			t.Errorf("%s: HasSubstance() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFeedbackEventWantsArtifact(t *testing.T) {
	if !(FeedbackEvent{}).WantsArtifact() {
		t.Error("a normal round gets its own product")
	}
	if (FeedbackEvent{IndexOnly: true}).WantsArtifact() {
		t.Error("an index-only round must not claim a product")
	}
}

func TestFeedbackHeaderForSubstitutesCount(t *testing.T) {
	got := FeedbackHeaderFor(3)
	if want := "已收到 3 轮人工反馈"; !strings.Contains(got, want) {
		t.Fatalf("FeedbackHeaderFor(3) missing %q:\n%s", want, got)
	}
	if strings.Contains(got, "{n}") {
		t.Fatal("placeholder left unsubstituted")
	}
	if !strings.Contains(got, "list_run_history") || !strings.Contains(got, "read_artifact") {
		t.Fatal("the clause must name both tools an agent needs to act on it")
	}
}
