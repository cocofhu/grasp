package services

import (
	"context"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// steerSandboxChat answers steers with status; a delivered steer finishes the
// running turn.
type steerSandboxChat struct {
	*fakeSandboxChat
	status sandbox.SteerStatus
	steers []string
}

func (f *steerSandboxChat) Steer(_ context.Context, _ uint, text string, _ []models.PromptImage) (sandbox.SteerStatus, error) {
	f.mu.Lock()
	f.steers = append(f.steers, text)
	f.mu.Unlock()
	if f.status == sandbox.SteerDelivered {
		f.release <- struct{}{}
	}
	return f.status, nil
}

func TestSandboxChatsSteer(t *testing.T) {
	cases := []struct {
		name    string
		status  sandbox.SteerStatus
		content string
		want    chatsession.SteerOutcome
		steered int
		ran     string
	}{
		{"delivered", sandbox.SteerDelivered, "two", chatsession.SteerOutcomeJoined, 1, ""},
		{"unsupported", sandbox.SteerUnsupported, "two", chatsession.SteerOutcomeNext, 1, "two"},
		{"command", sandbox.SteerDelivered, "/compact", chatsession.SteerOutcomeNext, 0, "/compact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &steerSandboxChat{fakeSandboxChat: newFakeSandboxChat(), status: tc.status}
			c := NewSandboxChats(nil)
			c.SetChatterForTest(f)
			if _, err := c.Steer(5, "x"); !errors.Is(err, chatsession.ErrNotFound) {
				t.Fatalf("steer without a session = %v", err)
			}
			_, _ = c.Enqueue(5, SandboxChatItem{Content: "one"})
			waitStarted(t, f.fakeSandboxChat, "one")
			_, _ = c.Enqueue(5, SandboxChatItem{ID: "it-2", Content: tc.content})
			out, err := c.Steer(5, "it-2")
			if err != nil || out != tc.want {
				t.Fatalf("steer = %q %v", out, err)
			}
			f.mu.Lock()
			steered := len(f.steers)
			f.mu.Unlock()
			if steered != tc.steered {
				t.Fatalf("steers = %d, want %d", steered, tc.steered)
			}
			if tc.ran != "" {
				waitStarted(t, f.fakeSandboxChat, tc.ran)
				close(f.release)
			}
		})
	}
}

func TestSteerSandboxTurn(t *testing.T) {
	ctx := context.Background()
	if got := steerSandboxTurn(ctx, newFakeSandboxChat(), 1, "x", nil); got != chatsession.SteerRejected {
		t.Fatalf("backend without steer = %v", got)
	}
	steerer := &steerSandboxChat{fakeSandboxChat: newFakeSandboxChat(), status: sandbox.SteerNoTurn}
	if got := steerSandboxTurn(ctx, steerer, 0, "x", nil); got != chatsession.SteerRejected {
		t.Fatalf("no sandbox = %v", got)
	}
	if got := steerSandboxTurn(ctx, steerer, 1, "x", nil); got != chatsession.SteerDeferred {
		t.Fatalf("no turn = %v", got)
	}
	svc := &SandboxService{live: map[uint]*liveSandbox{}}
	if st, err := svc.Steer(ctx, 1, "x", nil); st != sandbox.SteerNoTurn || err != nil {
		t.Fatalf("no live sandbox = %q %v", st, err)
	}
	svc.live[1] = &liveSandbox{switching: true}
	if st, _ := svc.Steer(ctx, 1, "x", nil); st != sandbox.SteerNoTurn {
		t.Fatalf("switching sandbox = %q", st)
	}
}
