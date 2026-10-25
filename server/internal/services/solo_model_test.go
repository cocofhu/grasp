package services

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"

	"gorm.io/gorm"
)

func TestChatModelIDFallsBackForOldSessions(t *testing.T) {
	legacy := &models.SoloSession{InitialModelID: "aux"}
	if chatModelID(legacy) != "aux" || appliedChatModelID(legacy) != "aux" {
		t.Fatalf("legacy: current=%q applied=%q", chatModelID(legacy), appliedChatModelID(legacy))
	}
	explicit := &models.SoloSession{InitialModelID: "aux", CurrentModelSet: true, AppliedModelSet: true}
	if chatModelID(explicit) != "" || appliedChatModelID(explicit) != "" {
		t.Fatalf("explicit primary must stay empty: current=%q applied=%q", chatModelID(explicit), appliedChatModelID(explicit))
	}
}

func TestSoloSetCurrentModelRejectsOutsideConfiguration(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 3)
	insertSoloModelConfig(t, db)
	beforeAgent, ok := solo.agents.Get("agentA")
	if !ok {
		t.Fatal("missing agent")
	}
	var beforeCfg models.ModelConfiguration
	if err := db.First(&beforeCfg, "id = ?", "sandbox-fixture").Error; err != nil {
		t.Fatal(err)
	}
	row := &models.SoloSession{
		ID: "s-model", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning,
		InitialModelID: "aux", CurrentModelSet: true, CurrentModelID: "aux",
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	_, err := solo.SetCurrentModel(context.Background(), row.ID, "alice", "nope")
	var se *SoloError
	if !errors.As(err, &se) || se.Code != "model_not_in_configuration" {
		t.Fatalf("outside model: %v", err)
	}
	got, err := solo.Require(row.ID, "alice")
	if err != nil || got.CurrentModelID != "aux" {
		t.Fatalf("selection changed on reject: %+v %v", got, err)
	}
	afterAgent, _ := solo.agents.Get("agentA")
	if !reflect.DeepEqual(beforeAgent.ModelBinding, afterAgent.ModelBinding) || !reflect.DeepEqual(beforeAgent.Env, afterAgent.Env) {
		t.Fatal("agent configuration was written")
	}
	var afterCfg models.ModelConfiguration
	if err := db.First(&afterCfg, "id = ?", "sandbox-fixture").Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeCfg.Binding, afterCfg.Binding) {
		t.Fatalf("model configuration changed: %+v", afterCfg.Binding)
	}
	if _, err := solo.SetCurrentModel(context.Background(), row.ID, "alice", "aux2"); err != nil {
		t.Fatal(err)
	}
	got, _ = solo.Require(row.ID, "alice")
	if !got.CurrentModelSet || got.CurrentModelID != "aux2" || got.InitialModelID != "aux" {
		t.Fatalf("stored current: set=%v current=%q initial=%q", got.CurrentModelSet, got.CurrentModelID, got.InitialModelID)
	}
	if err := db.Model(row).Update("status", models.SoloStatusClosed).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SetCurrentModel(context.Background(), row.ID, "alice", "aux"); err == nil {
		t.Fatal("closed session must reject a model change")
	}
}

func TestSoloModelSwitchWaitsForTurnBoundary(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, fg, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	ctx := context.Background()
	beforeAgent, _ := solo.agents.Get("agentA")

	v, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", InitialModelID: "aux"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox on opening model", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning && fg.LastCreateEnv()[runtime.EnvInitialChatModel] == "aux"
	})

	gate := newGateChat(true)
	defer gate.release()
	align := &recordAligner{}
	solo.SetChatterForTest(gate)
	solo.SetModelAlignerForTest(align)

	if _, err := solo.Send(v.ID, "alice", "one", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first turn started", func() bool { return gate.started() })
	if _, err := solo.Send(v.ID, "alice", "two", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SetCurrentModel(ctx, v.ID, "alice", "aux2"); err != nil {
		t.Fatal(err)
	}
	if n := align.count(); n != 0 {
		t.Fatalf("model applied during the current turn: %d", n)
	}
	gate.release()
	waitFor(t, "queued turn", func() bool {
		c := solo.lookup(v.ID)
		return gate.count() >= 2 && c != nil && c.sess.Ready()
	})

	prompts := gate.snapshot()
	if prompts[0] != "one" {
		t.Fatalf("current turn prompt = %q", prompts[0])
	}
	if got := align.snapshot(); len(got) != 1 || got[0] != "aux2" {
		t.Fatalf("applied models: %+v", got)
	}
	second := prompts[1]
	if !strings.Contains(second, "编码模型已更换") || !strings.Contains(second, "[用户] one") {
		t.Fatalf("next turn lost history: %q", second)
	}
	for _, banned := range []string{"沙箱已重新创建", "仓库已重新克隆", "未提交的改动已丢失"} {
		if strings.Contains(second, banned) {
			t.Fatalf("model switch reused restart copy %q in %q", banned, second)
		}
	}
	row, _ := solo.Require(v.ID, "alice")
	if row.InitialModelID != "aux" || row.CurrentModelID != "aux2" || row.AppliedModelID != "aux2" {
		t.Fatalf("persisted models: initial=%q current=%q applied=%q", row.InitialModelID, row.CurrentModelID, row.AppliedModelID)
	}
	afterAgent, _ := solo.agents.Get("agentA")
	if !reflect.DeepEqual(beforeAgent.ModelBinding, afterAgent.ModelBinding) {
		t.Fatal("switching the session model wrote the agent")
	}

	if _, err := solo.Close(ctx, v.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Restart(ctx, v.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restart uses current model", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning && fg.LastCreateEnv()[runtime.EnvInitialChatModel] == "aux2"
	})
	row, _ = solo.Require(v.ID, "alice")
	if row.InitialModelID != "aux" {
		t.Fatalf("restart rewrote the opening model: %q", row.InitialModelID)
	}
}

func TestSoloModelSwitchFailureRollsBack(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	insertSoloModelConfig(t, db)
	ctx := context.Background()
	v, err := solo.Create(ctx, "alice", SoloCreateInput{AgentName: "agentA", InitialModelID: "aux", Text: "keep me"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "opening reply", func() bool {
		msgs, _ := solo.Messages(v.ID, "alice")
		for _, m := range msgs {
			if m.Role == "assistant" {
				return true
			}
		}
		return false
	})
	gate := newGateChat(false)
	solo.SetChatterForTest(gate)
	solo.SetModelAlignerForTest(&recordAligner{err: errors.New("bridge refused")})
	if _, err := solo.SetCurrentModel(ctx, v.ID, "alice", "aux2"); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Send(v.ID, "alice", "next", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "failed switch still chats", func() bool {
		c := solo.lookup(v.ID)
		return gate.count() >= 1 && c != nil && c.sess.Ready()
	})
	prompt := gate.snapshot()[0]
	if prompt != "next" {
		t.Fatalf("rolled-back turn should not rebuild context: %q", prompt)
	}
	row, _ := solo.Require(v.ID, "alice")
	if row.CurrentModelID != "aux" || row.ModelNotice != soloModelSwitchFailed || row.InitialModelID != "aux" {
		t.Fatalf("rollback: current=%q notice=%q initial=%q", row.CurrentModelID, row.ModelNotice, row.InitialModelID)
	}
	msgs, _ := solo.Messages(v.ID, "alice")
	var kept bool
	for _, m := range msgs {
		if m.Content == "keep me" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("existing dialogue was dropped")
	}
}

func TestSoloOldSessionRestartKeepsOpeningModel(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, fg, db := newSoloFixture(t, port, 3)
	row := &models.SoloSession{
		ID: "old-model", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusClosed,
		InitialModelID: "aux",
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := solo.Restart(context.Background(), row.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "legacy opening model", func() bool {
		got, err := solo.Require(row.ID, "alice")
		return err == nil && got.Status == models.SoloStatusRunning && fg.LastCreateEnv()[runtime.EnvInitialChatModel] == "aux" && !got.CurrentModelSet
	})
}

func insertSoloModelConfig(t *testing.T, db *gorm.DB) {
	t.Helper()
	row := models.ModelConfiguration{
		ID: "sandbox-fixture", Name: "Fixture", Backend: "cursor", State: "active",
		Binding: modelrouting.Binding{
			Kind: "platform", CredentialID: "k", ModelID: "primary",
			AuxiliaryModelIDs: []string{"aux", "aux2"},
		},
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

type recordAligner struct {
	mu     sync.Mutex
	models []string
	err    error
}

func (r *recordAligner) AlignChatModel(_ context.Context, _ uint, model string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append(r.models, model)
	if r.err != nil {
		return false, r.err
	}
	return true, nil
}

func (r *recordAligner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.models)
}

func (r *recordAligner) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.models))
	copy(out, r.models)
	return out
}

type gateChat struct {
	mu      sync.Mutex
	prompts []string
	start   chan struct{}
	goOn    chan struct{}
	once    sync.Once
	block   bool
}

func newGateChat(block bool) *gateChat {
	return &gateChat{start: make(chan struct{}), goOn: make(chan struct{}), block: block}
}

func (g *gateChat) ChatWithTimeout(ctx context.Context, _ uint, text string, _ []models.PromptImage, _ time.Duration, _ func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	g.mu.Lock()
	first := len(g.prompts) == 0
	g.prompts = append(g.prompts, text)
	g.mu.Unlock()
	if first && g.block {
		g.once.Do(func() { close(g.start) })
		select {
		case <-g.goOn:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	return &models.TokenUsage{}, nil, nil
}

func (g *gateChat) Cancel(uint) {}

func (g *gateChat) started() bool {
	select {
	case <-g.start:
		return true
	default:
		return false
	}
}

func (g *gateChat) release() {
	select {
	case <-g.goOn:
	default:
		close(g.goOn)
	}
}

func (g *gateChat) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.prompts)
}

func (g *gateChat) snapshot() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, len(g.prompts))
	copy(out, g.prompts)
	return out
}
