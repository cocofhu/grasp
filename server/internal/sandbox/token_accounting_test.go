package sandbox

import (
	"context"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/gorilla/websocket"
)

func TestRetryUsageRetainsAllAttemptsWithoutLeakingOldErrors(t *testing.T) {
	first := &ChatResult{OpID: "first", Failed: true, ErrorText: "old auth error", Usage: &models.TokenUsage{InputTokens: 7, LedgerRecorded: true}}
	last := &ChatResult{OpID: "last", Narration: "success", Usage: &models.TokenUsage{OutputTokens: 3, LedgerRecorded: true}}
	merged := MergeAttemptUsage(first, last)
	if merged.Usage.Total() != 10 || !merged.Usage.LedgerRecorded || merged.Failed || merged.ErrorText != "" || merged.Narration != "success" {
		t.Fatalf("merged attempts=%+v", merged)
	}
	missing := MergeAttemptUsage(first, nil)
	if missing == nil || missing.Usage.Total() != 7 || missing.ErrorText != "" {
		t.Fatalf("usage dropped on failed retry: %+v", missing)
	}
}

func TestCancelledTurnPreservesUsageFromAcknowledgement(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_reply", true: "partial_reply"}[partial], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h, p := wsServer(t, func(conn *websocket.Conn, op string, msg map[string]any) {
				switch op {
				case "connect":
					_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "s"})
				case "chat":
					if partial {
						_ = conn.WriteJSON(tagged(chunkFrame("partial"), msg["opId"].(string)))
					}
					cancel()
				case "cancel":
					done := doneFrame()
					data := done["data"].(map[string]any)
					data["stopReason"] = "cancelled"
					data["usage"] = map[string]any{"m": map[string]int64{"inputTokens": 10, "outputTokens": 2, "cacheReadTokens": 3, "cacheWriteTokens": 4}}
					_ = conn.WriteJSON(tagged(done, msg["opId"].(string)))
				}
			})
			c := connectAndClient(t, h, p)
			result, err := c.ChatStructured(ctx, "hi", nil)
			if result == nil || result.Usage == nil || result.Usage.Total() != 19 || !result.Interrupted {
				t.Fatalf("lost cancelled turn usage: result=%+v err=%v", result, err)
			}
			if !partial && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error=%v", err)
			}
		})
	}
}

func TestReplayUsageKeepsIdentityTimeAndDeduplicatesCompletion(t *testing.T) {
	c := &ACPClient{}
	done := `{"type":"prompt_done","opId":"original-op","reportedAt":"2026-08-11T23:59:00Z","stopReason":"timeout","usage":{"m":{"inputTokens":19}}}`
	result, err := c.ReplayTurn(rawFrames(t, done, done))
	if !errors.Is(err, ErrSandboxTurnTimeout) || result == nil || result.Usage == nil || result.Usage.Total() != 19 || result.OpID != "original-op" || result.EndedAt.Format("2006-01-02") != "2026-08-11" {
		t.Fatalf("replay accounting result=%+v err=%v", result, err)
	}
}
