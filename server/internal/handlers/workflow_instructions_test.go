package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/google/uuid"
)

func TestWorkflowInstructionRoutesPinTargetAndKeepPrivateOriginUnread(t *testing.T) {
	h := newHarness(t)
	run := models.Run{ID: "instruction-api", Status: "running", OriginSoloID: "private-origin", Graph: models.Graph{Nodes: []models.Node{{ID: "work", Type: "agent", Caps: &models.AgentCapabilities{}}}}}
	if err := h.db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	visit := models.StateRun{RunID: run.ID, NodeID: "work", NodeType: "agent", Iteration: 2, Status: "running"}
	if err := h.db.Create(&visit).Error; err != nil {
		t.Fatal(err)
	}
	generation := uuid.NewString()
	if err := h.db.Create(&models.WorkflowInstructionMailbox{ID: visit.ID, RunID: run.ID, NodeID: "work", Iteration: 2, Generation: generation, State: "open"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&models.SoloMessage{ID: "private-text", SessionID: "private-origin", Role: "user", Content: "PRIVATE_ORIGIN_TRANSCRIPT"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&models.Setting{Key: services.KeyWorkflowsEnabled, Value: "false"}).Error; err != nil {
		t.Fatal(err)
	}
	path := "/api/runs/instruction-api/nodes/work/instructions"
	input := map[string]any{"iteration": 2, "generation": generation, "clientMessageId": uuid.NewString(), "text": "明确提交给此任务的要求", "actorId": "forged-user"}
	response := h.do(http.MethodPost, path, input)
	if response.Code != http.StatusOK {
		t.Fatalf("existing task management must work when launches are disabled: %d %s", response.Code, response.Body.String())
	}
	var receipt models.WorkflowInstruction
	if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil || receipt.State != "queued" || receipt.ActorID == "forged-user" || receipt.Iteration != 2 {
		t.Fatalf("invalid trusted receipt: %+v %v", receipt, err)
	}
	repeated := h.do(http.MethodPost, path, input)
	var second models.WorkflowInstruction
	if err := json.Unmarshal(repeated.Body.Bytes(), &second); err != nil || second.ID != receipt.ID {
		t.Fatalf("HTTP retry duplicated an instruction: %+v %v", second, err)
	}
	history := h.do(http.MethodGet, path, nil)
	if history.Code != http.StatusOK || !strings.Contains(history.Body.String(), receipt.ID) || strings.Contains(history.Body.String(), "PRIVATE_ORIGIN_TRANSCRIPT") {
		t.Fatalf("shared instruction history crossed origin privacy: %d %s", history.Code, history.Body.String())
	}
	input["generation"] = uuid.NewString()
	if stale := h.do(http.MethodPost, path, input); stale.Code != http.StatusConflict {
		t.Fatalf("stale target was accepted: %d %s", stale.Code, stale.Body.String())
	}
	h.cookie = ""
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		if denied := h.do(method, path, input); denied.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s accepted: %d", method, denied.Code)
		}
	}
}
