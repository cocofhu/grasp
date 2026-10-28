package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func TestReactReplyFreezesParkedExecutionDespiteAgentDefaultChanges(t *testing.T) {
	p, _, _, _, req := reactSetup(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "continue", questions: []models.ReactQuestion{{ID: "q", Prompt: "next?"}}}
		}
	})
	root := writeAgent(t, "react-agent", `{"inheritModel":false,"acpBackend":"cursor","modelBinding":{"kind":"configuration","configurationId":"original-group"}}`)
	p.opts.ProfilesRoot = root
	selected := "original-model"
	p.opts.ModelBinding = func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		return modelrouting.Resolved{Backend: "cursor", CredentialID: "selected-key", Secret: "fake-test-key", Binding: modelrouting.Binding{Kind: "platform", CredentialID: "selected-key", ModelID: selected}}, nil
	}
	if open := p.ReactOpen(context.Background(), req); open.SetupErr != nil {
		t.Fatal(open.SetupErr)
	}
	selected = "new-default-model"
	if err := os.WriteFile(filepath.Join(root, "react-agent", "agent.json"), []byte(`{"inheritModel":false,"acpBackend":"cursor","modelBinding":{"kind":"configuration","configurationId":"new-default-group"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	history := []models.ReactMessage{{Role: "human", Text: "keep going"}}
	for range 2 {
		reply := p.ReactReply(context.Background(), req, history, "keep going", nil, false)
		if reply.Err != nil || reply.Execution == nil {
			t.Fatalf("missing execution after successful real ACP turn: %+v", reply)
		}
		got := reply.Execution
		if got.AgentName != "react-agent" || got.Backend != "cursor" || got.ConfigurationID != "original-group" || got.ModelID != "original-model" || got.StartedAt.IsZero() {
			t.Fatalf("parked execution relabeled from mutable defaults: %+v", got)
		}
		// A consumer annotating its copy cannot alter the next turn's route.
		got.ModelID = "caller-mutated"
	}
}
