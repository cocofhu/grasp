package engine

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

type mapSkills map[string]services.Agent

func (m mapSkills) Get(name string) (services.Agent, bool) {
	agent, ok := m[name]
	return agent, ok
}

func launchModelGraph() models.Graph {
	return models.Graph{
		Nodes: []models.Node{
			{ID: "input", Type: "input"},
			{ID: "predev", Type: "agent", Caps: capsClarify, Config: map[string]any{"agent_profile": "pm"}},
			{ID: "next", Type: "agent", Caps: capsClarify, Config: map[string]any{"agent_profile": "dev"}},
			{ID: "output", Type: "output"},
		},
		Edges: []models.Edge{
			{ID: "e1", Source: "input", Target: "predev"},
			{ID: "e2", Source: "predev", Target: "next"},
			{ID: "e3", Source: "next", Target: "output"},
		},
	}
}

func seedLaunchModels(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&models.ModelConnection{
		ID: "conn", State: "active", ProviderID: "openai", Protocol: modelrouting.ProtocolChat,
		Models: []modelrouting.Model{
			{ID: "m1", Name: "Primary", ReasoningEfforts: []string{"low", "high"}},
			{ID: "m2", Name: "Aux", ReasoningEfforts: []string{"low", "high"}},
		},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ModelConfiguration{
		ID: "group", Name: "Codex", Backend: "codex", State: "active",
		Binding: modelrouting.Binding{Kind: "connection", ConnectionID: "conn", ModelID: "m1", AuxiliaryModelIDs: []string{"m2"}},
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func launchAgents() mapSkills {
	binding := &modelrouting.Binding{Kind: "configuration", ConfigurationID: "group"}
	return mapSkills{
		"pm":  {Name: "pm", AcpBackend: "codex", Capabilities: capsClarify, ModelBinding: binding},
		"dev": {Name: "dev", AcpBackend: "codex", Capabilities: capsClarify, ModelBinding: binding},
	}
}

func countRuns(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&models.Run{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLaunchModelRejectsIllegalChoiceWithoutCreatingRun(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, launchModelGraph())
	seedLaunchModels(t, db)
	eng.SetAgents(launchAgents())

	_, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", nil, LaunchChoice{
		Set: true, ConfigurationID: "group", ModelID: "nope", ReasoningEffort: "high",
	})
	if err == nil {
		t.Fatal("unbound model was accepted")
	}
	if countRuns(t, db) != 0 {
		t.Fatal("illegal model created a run")
	}

	_, err = eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", nil, LaunchChoice{
		Set: true, ConfigurationID: "group", ModelID: "", ReasoningEffort: "ultra",
	})
	if err == nil {
		t.Fatal("unsupported effort was accepted")
	}
	if countRuns(t, db) != 0 {
		t.Fatal("illegal effort created a run")
	}
}

func TestLaunchModelOmittedLeavesVisitUnset(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, clarifyOnlyGraph())
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", nil, LaunchChoice{})
	if err != nil {
		t.Fatal(err)
	}
	waitReactPause(t, db, run.ID, "predev")
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	if conv.CurrentModelSet || conv.CurrentModelID != "" || conv.CurrentReasoningEffort != "" {
		t.Fatalf("omitted launch wrote %+v", conv)
	}
}

func TestLaunchModelStampsFirstVisitAndReplyRoute(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, clarifyOnlyGraph())
	seedLaunchModels(t, db)
	eng.SetAgents(launchAgents())
	p.mu.Lock()
	p.reactOpenRouteApplied = true
	p.mu.Unlock()

	run, err := eng.StartRunFromPublishedWithFirstMessage("wf", nil, "test", "normal", nil, nil, "", &models.CompositeText{Text: "把登录做清楚"}, LaunchChoice{
		Set: true, ConfigurationID: "group", ModelID: "m1", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored models.Run
	if err := db.First(&stored, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !stored.LaunchModelSet || stored.LaunchModelID != "" || stored.LaunchConfigurationID != "group" || stored.LaunchReasoningEffort != "high" {
		t.Fatalf("stored launch = set=%v cfg=%q model=%q effort=%q", stored.LaunchModelSet, stored.LaunchConfigurationID, stored.LaunchModelID, stored.LaunchReasoningEffort)
	}

	waitFirstHumanTurn(t, db, run.ID, "predev")
	var conv models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&conv).Error; err != nil {
		t.Fatal(err)
	}
	if !conv.CurrentModelSet || conv.CurrentModelID != "" || conv.CurrentConfigurationID != "group" || conv.CurrentReasoningEffort != "high" {
		t.Fatalf("visit = set=%v cfg=%q model=%q effort=%q notice=%q", conv.CurrentModelSet, conv.CurrentConfigurationID, conv.CurrentModelID, conv.CurrentReasoningEffort, conv.ModelNotice)
	}

	deadline := time.Now().Add(waitPollTimeout)
	var reply *chatRouteSnap
	for time.Now().Before(deadline) {
		for _, snap := range p.chatRouteSnaps() {
			if snap.Kind == "reply" && snap.NodeID == "predev" {
				copy := snap
				reply = &copy
				break
			}
		}
		if reply != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reply == nil || !reply.Set || reply.Reset || reply.ModelID != "" || reply.Effort != "high" || reply.ConfigurationID != "group" {
		t.Fatalf("reply route = %+v", reply)
	}
}

func TestLaunchModelSkipsLaterNode(t *testing.T) {
	eng, db, p := setupEngineGraphP(t, launchModelGraph())
	seedLaunchModels(t, db)
	eng.SetAgents(launchAgents())
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", &models.CompositeText{Text: "开始"}, LaunchChoice{
		Set: true, ConfigurationID: "group", ModelID: "m2", ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFirstHumanTurn(t, db, run.ID, "predev")
	waitApproveReady(t, eng, db, run.ID, "predev")
	if err := eng.ReactReply(run.ID, "predev", "确认并流转", nil, nil, true); err != nil {
		t.Fatal(err)
	}
	waitReactPause(t, db, run.ID, "next")

	var first, next models.ReactConversation
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if first.CurrentModelID != "m2" || first.CurrentReasoningEffort != "low" || !first.CurrentModelSet {
		t.Fatalf("first visit = %+v", first)
	}
	if err := db.Where("run_id = ? AND node_id = ?", run.ID, "next").First(&next).Error; err != nil {
		t.Fatal(err)
	}
	if next.CurrentModelSet || next.CurrentModelID != "" || next.CurrentReasoningEffort != "" {
		t.Fatalf("later visit inherited launch: %+v", next)
	}
	var opened *chatRouteSnap
	for _, snap := range p.chatRouteSnaps() {
		if snap.Kind == "open" && snap.NodeID == "next" {
			copy := snap
			opened = &copy
		}
	}
	if opened == nil || !opened.Reset || opened.Set || opened.ModelID != "" {
		t.Fatalf("later open route = %+v", opened)
	}
}

func TestLaunchModelReappliesOnEmptyResumeAndKeepsHumanVisit(t *testing.T) {
	eng, db, _ := setupEngineGraphP(t, clarifyOnlyGraph())
	seedLaunchModels(t, db)
	eng.SetAgents(launchAgents())
	run, err := eng.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", nil, LaunchChoice{
		Set: true, ConfigurationID: "group", ModelID: "", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitReactPause(t, db, run.ID, "predev")
	if err := eng.Cancel(run.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db, run.ID, "cancelled")
	if err := eng.ResumeFrom(run.ID, "predev"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(waitPollTimeout)
	var fresh models.ReactConversation
	for {
		// First() treats a non-zero primary key on dest as a condition.
		fresh = models.ReactConversation{}
		err := db.Where("run_id = ? AND node_id = ?", run.ID, "predev").Order("iteration desc").First(&fresh).Error
		if err == nil && fresh.Iteration >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("empty resume did not open a new visit: %+v %v", fresh, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitRunStatus(t, db, run.ID, "waiting_human")
	if err := db.Where("run_id = ? AND node_id = ? AND iteration = ?", run.ID, "predev", fresh.Iteration).First(&fresh).Error; err != nil {
		t.Fatal(err)
	}
	if !fresh.CurrentModelSet || fresh.CurrentModelID != "" || fresh.CurrentReasoningEffort != "high" || hasHumanMessages(fresh.Messages) {
		t.Fatalf("empty resume = iter=%d set=%v model=%q effort=%q humans=%v", fresh.Iteration, fresh.CurrentModelSet, fresh.CurrentModelID, fresh.CurrentReasoningEffort, hasHumanMessages(fresh.Messages))
	}

	eng2, db2, _ := setupEngineGraphP(t, clarifyOnlyGraph())
	seedLaunchModels(t, db2)
	eng2.SetAgents(launchAgents())
	run2, err := eng2.StartRunWithFirstMessage("wf", nil, "test", "", nil, nil, "", &models.CompositeText{Text: "先说一句"}, LaunchChoice{
		Set: true, ConfigurationID: "group", ModelID: "", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFirstHumanTurn(t, db2, run2.ID, "predev")
	if err := db2.Model(&models.ReactConversation{}).Where("run_id = ? AND node_id = ?", run2.ID, "predev").Updates(map[string]any{
		"current_model_id": "m2", "current_model_set": true, "current_configuration_id": "group", "current_reasoning_effort": "low",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := eng2.Cancel(run2.ID); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, db2, run2.ID, "cancelled")
	if err := eng2.ResumeFrom(run2.ID, "predev"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(waitPollTimeout)
	var carried models.ReactConversation
	for {
		carried = models.ReactConversation{}
		err := db2.Where("run_id = ? AND node_id = ?", run2.ID, "predev").Order("iteration desc").First(&carried).Error
		if err == nil && carried.Iteration >= 2 && hasHumanMessages(carried.Messages) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("carried visit did not open: %+v %v", carried, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !carried.CurrentModelSet || carried.CurrentModelID != "m2" || carried.CurrentReasoningEffort != "low" {
		t.Fatalf("human resume was overwritten: set=%v model=%q effort=%q", carried.CurrentModelSet, carried.CurrentModelID, carried.CurrentReasoningEffort)
	}
}
