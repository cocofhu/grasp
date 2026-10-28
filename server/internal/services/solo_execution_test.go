package services

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

type executionSnapshotChat struct {
	started chan struct{}
	release chan struct{}
	fail    bool
}

func (g *executionSnapshotChat) ChatWithTimeout(ctx context.Context, _ uint, _ string, _ []models.PromptImage, _ time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	close(g.started)
	onEvent(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"kept reply"}}`))
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-g.release:
	}
	if g.fail {
		return nil, nil, errors.New("fixture turn failed")
	}
	return &models.TokenUsage{InputTokens: 7}, nil, nil
}

func (*executionSnapshotChat) Cancel(uint) {}

func TestSoloExecutionSnapshotSurvivesSelectionChangesAndFailures(t *testing.T) {
	for _, status := range []string{"ok", "failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			acp, port := fakeACPServer(t)
			defer acp.Close()
			solo, _, _, db := newSoloFixture(t, port, 3)
			insertSoloModelConfig(t, db)
			view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _, _ = solo.Close(context.Background(), view.ID, "alice") }()
			waitFor(t, "running snapshot sandbox", func() bool {
				row, err := solo.Require(view.ID, "alice")
				return err == nil && row.Status == models.SoloStatusRunning
			})
			gate := &executionSnapshotChat{started: make(chan struct{}), release: make(chan struct{}), fail: status == "failed"}
			solo.SetChatterForTest(gate)
			solo.SetModelAlignerForTest(&recordAligner{})
			frames, unsubscribe, err := solo.Subscribe(view.ID, "alice", 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unsubscribe()
			if _, err := solo.Send(view.ID, "alice", "test route metadata", nil); err != nil {
				t.Fatal(err)
			}
			select {
			case <-gate.started:
			case <-time.After(5 * time.Second):
				t.Fatal("turn did not start")
			}
			var execution *models.ChatExecution
			deadline := time.After(3 * time.Second)
			for execution == nil {
				select {
				case frame := <-frames:
					if frame.Event == SoloEventExecution {
						execution, _ = frame.Payload["execution"].(*models.ChatExecution)
					}
				case <-deadline:
					t.Fatal("aligned execution frame missing")
				}
			}
			if execution.AgentName != "agentA" || execution.Backend != "cursor" || execution.ConfigurationID != "sandbox-fixture" || execution.ModelID != "primary" || execution.StartedAt.IsZero() {
				t.Fatalf("execution = %+v", execution)
			}
			// This selection is for the following turn. It must not relabel the
			// active response or change its reload representation.
			if _, err := solo.SetCurrentModel(context.Background(), view.ID, "alice", "aux2"); err != nil {
				t.Fatal(err)
			}
			replayed, unReplay, err := solo.Subscribe(view.ID, "alice", 0)
			if err != nil {
				t.Fatal(err)
			}
			first := <-replayed
			unReplay()
			if first.Event != chatsession.EventQueueState || !reflect.DeepEqual(first.Payload["execution"], execution) {
				t.Fatal("active reconnect snapshot lost the original execution")
			}
			if status == "stopped" {
				if err := solo.Cancel(view.ID, "alice"); err != nil {
					t.Fatal(err)
				}
			} else {
				close(gate.release)
			}
			var reply models.SoloMessage
			waitFor(t, "persisted execution snapshot", func() bool {
				var messages []models.SoloMessage
				messages, _ = solo.Messages(view.ID, "alice")
				for _, message := range messages {
					if message.Role == "assistant" {
						reply = message
						return true
					}
				}
				return false
			})
			if reply.Status != status || !reflect.DeepEqual(reply.Execution, execution) {
				t.Fatalf("persisted status/execution = %s %+v", reply.Status, reply.Execution)
			}
			if err := db.Model(&models.ModelConfiguration{}).Where("id = ?", "sandbox-fixture").Update("backend", "claude_code").Error; err != nil {
				t.Fatal(err)
			}
			var reloaded models.SoloMessage
			if err := db.First(&reloaded, "id = ?", reply.ID).Error; err != nil || !reflect.DeepEqual(reloaded.Execution, execution) {
				t.Fatal("later configuration edit relabeled the saved execution")
			}
		})
	}
}

func TestSoloExecutionSnapshotUsesActualPreparedPrimary(t *testing.T) {
	solo, sbx, _, _ := newSoloFixture(t, 0, 3)
	row := &models.SoloSession{AgentName: "agentA", CurrentModelSet: true, CurrentConfigurationID: "sandbox-fixture", CurrentReasoningEffort: "high"}
	sbx.live[41] = &liveSandbox{prepared: &preparedAgentContainer{backend: runtime.BackendCodex,
		spec: sandbox.Spec{Env: map[string]string{runtime.EnvACPBridgeModel: "actual-applied-primary"}}}}
	actual := solo.turnExecutionSnapshot(row, 41, time.Now())
	if actual.Backend != "codex" || actual.ModelID != "actual-applied-primary" || actual.ReasoningEffort != "high" {
		t.Fatalf("prepared route snapshot = %+v", actual)
	}
	row.CurrentModelID = "selected-aux"
	if actual := solo.turnExecutionSnapshot(row, 41, time.Now()); actual.ModelID != "selected-aux" {
		t.Fatal("explicit aligned chat model was replaced by the group primary")
	}
}

func TestSoloLegacyMessageDoesNotInventExecution(t *testing.T) {
	db := newTestDB(t)
	legacy := models.SoloMessage{ID: "legacy-execution", SessionID: "legacy", Role: "assistant", Content: "old reply"}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	var loaded models.SoloMessage
	if err := db.First(&loaded, "id = ?", legacy.ID).Error; err != nil || loaded.Execution != nil {
		t.Fatal("legacy execution must remain unknown")
	}
}
