package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

const providerQuotaErr = "The free trial quota for the service has been exhausted and postpaid billing is not enabled, so the service cannot be accessed. Please go to Console > Online Inference Service to enable postpaid billing."

func TestChatFailureHelper(t *testing.T) {
	if chatFailure(nil) != "" {
		t.Fatal("nil result must be empty")
	}
	if chatFailure(&sandbox.ChatResult{}) != "" {
		t.Fatal("clean result must be empty")
	}
	got := chatFailure(&sandbox.ChatResult{Failed: true})
	if got != "stopReason=failed" {
		t.Fatalf("Failed without body: got %q", got)
	}
	got = chatFailure(&sandbox.ChatResult{ErrorText: "  quota gone  "})
	if got != "quota gone" {
		t.Fatalf("ErrorText trim: got %q", got)
	}
	long := strings.Repeat("x", chatFailureMaxLen+50)
	got = chatFailure(&sandbox.ChatResult{ErrorText: long})
	if len(got) != chatFailureMaxLen+len("…") {
		t.Fatalf("truncate len=%d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncate suffix: %q", got[len(got)-3:])
	}
}

func TestWithFailureBanner(t *testing.T) {
	if got := withFailureBanner("", "澄清回复失败", "boom"); got != "(澄清回复失败:boom)" {
		t.Fatalf("empty narration: %q", got)
	}
	if got := withFailureBanner("partial", "澄清回复失败", "boom"); got != "partial\n(澄清回复失败:boom)" {
		t.Fatalf("append: %q", got)
	}
}

// Oneshoot-style failure: bridge emits error_text then prompt_done{stopReason:failed}
// with a nil Go error. ReactReply must surface the provider body, not an empty card.
func TestReactReplySurfacesProviderErrorText(t *testing.T) {
	p, _, _, _, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{
					narration: "need info",
					questions: []models.ReactQuestion{
						{ID: "q1", Prompt: "?", Options: []models.ReactOption{{ID: "a", Label: "A"}}},
					},
				}
			}
			return turnAction{errorText: providerQuotaErr, failed: true}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("opening question must pause")
	}
	hist := []models.ReactMessage{
		{Role: "agent", Text: "need info"},
		{Role: "human", Text: "answer"},
	}
	reply := p.ReactReply(context.Background(), req, hist, "answer", nil, false)
	if reply.Done {
		t.Fatalf("provider failure must keep dialogue open, got %+v", reply)
	}
	if !strings.Contains(reply.Msg, "澄清回复失败") {
		t.Fatalf("missing failure banner: %q", reply.Msg)
	}
	if !strings.Contains(reply.Msg, "free trial quota") {
		t.Fatalf("missing provider error body: %q", reply.Msg)
	}
	if !p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("session must stay parked after provider failure")
	}
}

// ReactOpen used to finishReact on empty narration even when the provider failed.
// That silently closed the node; now it must keep the dialogue open with the banner.
func TestReactOpenSurfacesProviderErrorText(t *testing.T) {
	p, _, _, _, req := reactSetup(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{errorText: providerQuotaErr, failed: true}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatalf("provider failure on open must not finish the node, got %+v", open)
	}
	if !strings.Contains(open.Msg, "澄清开场失败") {
		t.Fatalf("missing open failure banner: %q", open.Msg)
	}
	if !strings.Contains(open.Msg, "free trial quota") {
		t.Fatalf("missing provider error body: %q", open.Msg)
	}
	if !p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("session must stay parked after open failure")
	}
}

func TestReviseInPlaceSurfacesProviderErrorText(t *testing.T) {
	p, _, _, _, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{
					narration: "draft",
					questions: []models.ReactQuestion{
						{ID: "q1", Prompt: "?", Options: []models.ReactOption{{ID: "a", Label: "A"}}},
					},
				}
			}
			return turnAction{errorText: providerQuotaErr, failed: true}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("opening question must pause")
	}
	hist := []models.ReactMessage{
		{Role: "agent", Text: "draft"},
		{Role: "human", Text: "please revise"},
	}
	rev := p.ReviseInPlace(context.Background(), req, hist, "please revise", nil)
	if rev.Done {
		t.Fatalf("revise provider failure must not Done, got %+v", rev)
	}
	if rev.Err == nil {
		t.Fatal("revise provider failure must set Err")
	}
	if !strings.Contains(rev.Msg, "复审修改失败") {
		t.Fatalf("missing revise failure banner: %q", rev.Msg)
	}
	if !strings.Contains(rev.Msg, "free trial quota") {
		t.Fatalf("missing provider error body: %q", rev.Msg)
	}
}

// Top-level {op:error} after the agent already produced content used to be
// swallowed (hasContent → success). It must still land on the failure card.
func TestReactReplySurfacesTopLevelErrorWithContent(t *testing.T) {
	p, _, _, _, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{
					narration: "need info",
					questions: []models.ReactQuestion{
						{ID: "q1", Prompt: "?", Options: []models.ReactOption{{ID: "a", Label: "A"}}},
					},
				}
			}
			return turnAction{narration: "partial thought", sendError: providerQuotaErr}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("opening question must pause")
	}
	hist := []models.ReactMessage{
		{Role: "agent", Text: "need info"},
		{Role: "human", Text: "answer"},
	}
	reply := p.ReactReply(context.Background(), req, hist, "answer", nil, false)
	if reply.Done {
		t.Fatalf("top-level error with content must keep dialogue open, got %+v", reply)
	}
	if !strings.Contains(reply.Msg, "澄清回复失败") {
		t.Fatalf("missing failure banner: %q", reply.Msg)
	}
	if !strings.Contains(reply.Msg, "free trial quota") {
		t.Fatalf("missing provider error body: %q", reply.Msg)
	}
	if !strings.Contains(reply.Msg, "partial thought") {
		t.Fatalf("partial narration must be preserved: %q", reply.Msg)
	}
}
