package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func providerTurnFrames(stop, diagnostic, text string) []map[string]any {
	var frames []map[string]any
	if text != "" {
		frames = append(frames, map[string]any{
			"type": "session_update",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": text},
			},
		})
	}
	if diagnostic != "" {
		frames = append(frames, map[string]any{"type": "error_text", "text": diagnostic})
	}
	return append(frames, map[string]any{
		"type": "prompt_done", "stopReason": stop,
		"usage": map[string]any{"fixture-model": map[string]any{"inputTokens": 7, "outputTokens": 3}},
	})
}

func TestSandboxChatWithTimeoutReportsProviderFailures(t *testing.T) {
	cases := []struct {
		name, stop, diagnostic, text, wantError string
		wantSentinel                            error
	}{
		{name: "failed without diagnostic", stop: "failed", wantError: "stopReason=failed"},
		{name: "refusal without diagnostic", stop: "refusal", wantError: "stopReason=refusal"},
		{name: "refusal with diagnostic", stop: "refusal", diagnostic: "provider authentication failed", wantError: "provider authentication failed"},
		{name: "provider error without failed stop", stop: "end_turn", diagnostic: "provider quota exhausted", wantError: "provider quota exhausted"},
		{name: "failed partial output", stop: "failed", diagnostic: "model request rejected", text: "partial reply", wantError: "model request rejected"},
		{name: "successful output", stop: "end_turn", text: "complete reply"},
		{name: "partial cancelled output", stop: "cancelled", diagnostic: "cancelled by user", text: "partial reply"},
		{name: "partial timeout output", stop: "timeout", diagnostic: "turn timed out", text: "partial reply"},
		{name: "empty timeout keeps error", stop: "timeout", diagnostic: "turn timed out", wantError: "turn timed out", wantSentinel: sandbox.ErrSandboxTurnTimeout},
		{name: "stuck keeps error", stop: "stuck", diagnostic: "agent stopped responding", wantError: "agent stopped responding", wantSentinel: sandbox.ErrAgentStuck},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acp, port := fakeACPServerFrames(t, providerTurnFrames(tc.stop, tc.diagnostic, tc.text)...)
			defer acp.Close()
			db := newTestDB(t)
			s := newSandboxService(t, db, &dockerState{acpPort: port})
			ctx := context.Background()
			row, err := s.Open(ctx, "agentA", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Destroy(ctx, row.ID) })
			if err := s.WaitReady(ctx, row.ID, nil); err != nil {
				t.Fatal(err)
			}
			var streamed strings.Builder
			usage, byModel, err := s.ChatWithTimeout(ctx, row.ID, "hello", nil, time.Minute, func(raw json.RawMessage) {
				streamed.Write(raw)
			})
			if tc.wantError == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			if tc.wantSentinel != nil && !errors.Is(err, tc.wantSentinel) {
				t.Fatalf("error = %v, want sentinel %v", err, tc.wantSentinel)
			}
			// Empty timeout intentionally returns no result in the ACP API.
			if tc.wantSentinel != sandbox.ErrSandboxTurnTimeout {
				if usage == nil || usage.InputTokens != 7 || usage.OutputTokens != 3 || byModel["fixture-model"].InputTokens != 7 {
					t.Fatalf("usage was lost: %+v %+v", usage, byModel)
				}
			}
			if tc.text != "" && !strings.Contains(streamed.String(), tc.text) {
				t.Fatalf("partial output was lost: %s", streamed.String())
			}
			if tc.wantError != "" {
				// The shared Chat wrapper must report the same provider failure.
				if err := s.Chat(ctx, row.ID, "again", nil, nil); err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("Chat error = %v, want %q", err, tc.wantError)
				}
				if tc.wantSentinel == nil {
					var entry models.TokenUsageEvent
					if err := db.Where("sandbox_id = ? AND source = ?", row.ID, models.TokenLedgerSourceStudio).First(&entry).Error; err != nil || entry.Status != models.TokenLedgerStatusFailed {
						t.Fatalf("Studio failure ledger: %+v %v", entry, err)
					}
				}
			}
		})
	}
}

func TestSoloPersistsProviderFailureFromACP(t *testing.T) {
	for _, tc := range []struct{ name, stop, diagnostic, text, wantError string }{
		{name: "failed without diagnostic", stop: "failed", wantError: "stopReason=failed"},
		{name: "refusal without diagnostic", stop: "refusal", wantError: "stopReason=refusal"},
		{name: "error text without failed stop", stop: "end_turn", diagnostic: "model request rejected", wantError: "model request rejected"},
		{name: "failure with partial output", stop: "failed", diagnostic: "quota exhausted", text: "partial reply", wantError: "quota exhausted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acp, port := fakeACPServerFrames(t, providerTurnFrames(tc.stop, tc.diagnostic, tc.text)...)
			defer acp.Close()
			solo, _, _, db := newSoloFixture(t, port, 3)
			ctx := context.Background()
			v, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", Text: "hello"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = solo.Close(ctx, v.ID, "alice") })
			var msgs []models.SoloMessage
			waitFor(t, "failed reply and user status", func() bool {
				msgs, _ = solo.Messages(v.ID, "alice")
				return len(msgs) == 2 && msgs[0].Status == "failed" && msgs[1].Status == "failed"
			})
			reply := msgs[1]
			if reply.Role != "assistant" || reply.Content != tc.text || !strings.Contains(reply.Error, tc.wantError) || len(reply.Events) == 0 {
				t.Fatalf("reply lost failure or partial output: %+v", reply)
			}
			if reply.Usage == nil || reply.Usage.InputTokens != 7 || reply.UsageByModel["fixture-model"].OutputTokens != 3 {
				t.Fatalf("failed turn lost usage: %+v %+v", reply.Usage, reply.UsageByModel)
			}
			var entry models.TokenUsageEvent
			waitFor(t, "failed turn usage ledger", func() bool {
				return db.Where("thread_id = ?", v.ID).First(&entry).Error == nil
			})
			if entry.Status != models.TokenLedgerStatusFailed {
				t.Fatalf("failed turn ledger: %+v", entry)
			}
		})
	}
}
