package engine

import (
	"context"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

type fixedAligner struct {
	runtime.ExecProvider
	notice  string
	aligned bool
}

func (a fixedAligner) AlignChatRoute(context.Context, runtime.NodeReq) (string, bool, error) {
	return a.notice, a.aligned, nil
}

func TestPrepareVisitModelKeepsChoiceWhenSandboxIsLost(t *testing.T) {
	e := &Engine{provider: fixedAligner{}}
	conv := &models.ReactConversation{CurrentModelSet: true, CurrentConfigurationID: "group", CurrentModelID: "aux", CurrentReasoningEffort: "high"}
	req := runtime.NodeReq{}
	e.prepareVisitModel(context.Background(), &req, conv, "run", "clarify")
	if !conv.CurrentModelSet || conv.CurrentConfigurationID != "group" || conv.CurrentModelID != "aux" || conv.CurrentReasoningEffort != "high" || conv.ModelNotice != "" || conv.AppliedModelSet {
		t.Fatalf("unaligned visit changed the stored choice: %+v", conv)
	}
	if req.ChatRoute == nil || req.ChatRoute.ModelID != "aux" || req.ChatRoute.ReasoningEffort != "high" {
		t.Fatalf("turn lost the chosen route: %+v", req.ChatRoute)
	}
	// The rebuilt turn aligns and records the route as applied.
	e.rememberTurnRoute(conv, &req, runtime.ReactTurn{RouteApplied: true})
	if !conv.AppliedModelSet || conv.AppliedModelID != "aux" || conv.CurrentModelID != "aux" || conv.ModelNotice != "" {
		t.Fatalf("rebuilt turn did not record the chosen route: %+v", conv)
	}
}

func TestBindChatRouteKeepsVisitsIndependent(t *testing.T) {
	e := &Engine{}
	chosen := &models.ReactConversation{CurrentModelSet: true, CurrentConfigurationID: "group", CurrentModelID: "aux", CurrentReasoningEffort: "high", AppliedModelSet: true, AppliedModelID: "old"}
	req := runtime.NodeReq{}
	e.bindChatRoute(&req, chosen)
	if req.ResetToDefaultModel || req.ChatRoute == nil || req.ChatRoute.ModelID != "aux" || req.ChatRoute.ReasoningEffort != "high" {
		t.Fatalf("chat route = %+v reset=%v", req.ChatRoute, req.ResetToDefaultModel)
	}
	if req.AppliedRoute == nil || req.AppliedRoute.ModelID != "old" {
		t.Fatalf("applied = %+v", req.AppliedRoute)
	}
	fresh := &models.ReactConversation{Messages: []models.ReactMessage{{Role: "agent", ReplyMetadata: models.ReplyMetadata{Execution: &models.ChatExecution{ModelID: "then"}}}}}
	auto := runtime.NodeReq{}
	e.bindChatRoute(&auto, fresh)
	if !auto.ResetToDefaultModel || auto.ChatRoute != nil {
		t.Fatalf("unset visit: %+v reset=%v", auto.ChatRoute, auto.ResetToDefaultModel)
	}
	e.noteChatRoute(chosen, &req, "model_switch_failed")
	if chosen.CurrentModelID != "old" || chosen.CurrentReasoningEffort != "" || chosen.ModelNotice != "model_switch_failed" {
		t.Fatalf("rollback stored %+v", chosen)
	}
	if fresh.Messages[0].Execution.ModelID != "then" {
		t.Fatal("history execution was rewritten")
	}
	req.ChatRoute = &runtime.ChatRoute{ConfigurationID: "group", ModelID: "aux", Set: true}
	e.noteChatRoute(chosen, &req, "")
	if chosen.ModelNotice != "" || !chosen.AppliedModelSet || chosen.AppliedModelID != "aux" || chosen.Messages != nil && len(chosen.Messages) > 0 {
		t.Fatalf("applied = %+v", chosen)
	}
}
