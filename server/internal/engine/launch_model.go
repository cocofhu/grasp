package engine

import (
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

// LaunchChoice is the optional model selection on a start-run request.
// Set is false when the caller omitted it. An empty ModelID is the primary model.
type LaunchChoice struct {
	Set             bool
	ConfigurationID string
	ModelID         string
	ReasoningEffort string
}

// normalizeLaunchModel checks a launch choice against the first clarify
// Agent. An omitted choice is left unset. A present choice that does not
// match that Agent is rejected before any run row is written.
func (e *Engine) normalizeLaunchModel(graph *models.Graph, launch LaunchChoice) (LaunchChoice, error) {
	if !launch.Set {
		return LaunchChoice{}, nil
	}
	node := firstClarifySuccessNode(graph)
	if node == nil {
		return LaunchChoice{}, fmt.Errorf("这次工作流不是从澄清对话开始，不能指定启动模型")
	}
	if e.skills == nil {
		return LaunchChoice{}, fmt.Errorf("无法校验启动模型")
	}
	profile := models.AgentProfile(node.Config)
	agent, ok := e.skills.Get(profile)
	if !ok {
		return LaunchChoice{}, fmt.Errorf("节点 %s 的 Agent %s 不存在", node.ID, profile)
	}
	cfg, modelID, effort, _, err := services.AcceptVisitSelection(e.db, agent, launch.ConfigurationID, launch.ModelID, launch.ReasoningEffort)
	if err != nil {
		return LaunchChoice{}, err
	}
	return LaunchChoice{Set: true, ConfigurationID: cfg, ModelID: modelID, ReasoningEffort: effort}, nil
}

// firstClarifySuccessNode is the clarify Agent on the input node's success
// path: the first unguarded success edge, or the only success edge.
func firstClarifySuccessNode(graph *models.Graph) *models.Node {
	if graph == nil {
		return nil
	}
	var inputID string
	for i := range graph.Nodes {
		if graph.Nodes[i].Type == "input" {
			inputID = graph.Nodes[i].ID
			break
		}
	}
	if inputID == "" {
		return nil
	}
	var success []models.Edge
	for _, edge := range graph.Edges {
		if edge.Source == inputID && edge.KindOrDefault() == models.EdgeSuccess {
			success = append(success, edge)
		}
	}
	if len(success) == 0 {
		return nil
	}
	picked := -1
	for i := range success {
		if strings.TrimSpace(success[i].When) == "" {
			picked = i
			break
		}
	}
	if picked < 0 {
		if len(success) != 1 {
			return nil
		}
		picked = 0
	}
	node := graph.FindNode(success[picked].Target)
	if node == nil || !node.Caps.Clarify() {
		return nil
	}
	return node
}

func hasHumanMessages(msgs []models.ReactMessage) bool {
	for _, msg := range msgs {
		if msg.Role == "human" {
			return true
		}
	}
	return false
}

// applyVisitLaunchModel writes the run's launch choice onto a brand-new
// clarify visit that has no human turn yet. A carried dialogue keeps the
// model it already had. rememberTurnRoute must run first: opening the sandbox
// still resets to the Agent primary, and that record must not clear this write.
func (e *Engine) applyVisitLaunchModel(conv *models.ReactConversation, run *models.Run, node *models.Node, iter int, msgs []models.ReactMessage) {
	if conv == nil || run == nil || node == nil {
		return
	}
	if hasHumanMessages(msgs) {
		if prev, ok := e.previousOpenConversation(run.ID, node.ID, iter); ok && prev.CurrentModelSet {
			conv.CurrentConfigurationID = prev.CurrentConfigurationID
			conv.CurrentModelID = prev.CurrentModelID
			conv.CurrentModelSet = true
			conv.CurrentReasoningEffort = prev.CurrentReasoningEffort
		}
		return
	}
	if !run.LaunchModelSet || !node.Caps.Clarify() {
		return
	}
	target := firstClarifySuccessNode(&run.Graph)
	if target == nil || target.ID != node.ID {
		return
	}
	conv.CurrentConfigurationID = run.LaunchConfigurationID
	conv.CurrentModelID = run.LaunchModelID
	conv.CurrentModelSet = true
	conv.CurrentReasoningEffort = run.LaunchReasoningEffort
	conv.ModelNotice = ""
}

// previousOpenConversation is the dialogue carried into this visit when the
// previous one was cancelled or failed before it finished.
func (e *Engine) previousOpenConversation(runID, nodeID string, iter int) (models.ReactConversation, bool) {
	var prev models.StateRun
	if err := e.db.Select("iteration", "status").
		Where("run_id = ? AND node_id = ? AND iteration < ?", runID, nodeID, iter).
		Order("iteration desc, id desc").First(&prev).Error; err != nil {
		return models.ReactConversation{}, false
	}
	if prev.Status != "failed" && prev.Status != "cancelled" {
		return models.ReactConversation{}, false
	}
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, prev.Iteration).
		Order("id desc").First(&conv).Error; err != nil || conv.Done {
		return models.ReactConversation{}, false
	}
	return conv, true
}
