package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

// SetRunRecoveryPreparer shares the runtime's spec builder with explicit IDE,
// terminal and queued-reply wakeups, including after a control-plane restart.
func (s *SandboxService) SetRunRecoveryPreparer(preparer runtime.SandboxRecoveryPreparer) {
	s.runRecoveryPreparer = preparer
}

func (s *SandboxService) prepareRecoveryInputs(row *models.Sandbox) (sandbox.Spec, error) {
	if row.Purpose == "run" {
		return s.prepareRunRecoveryInputs(row)
	}
	if s.skills == nil {
		return sandbox.Spec{}, errors.New("agent configuration service is unavailable")
	}
	agent, ok := s.skills.Get(row.Profile)
	if !ok {
		return sandbox.Spec{}, fmt.Errorf("recovery agent %q is unavailable", row.Profile)
	}
	agent = s.effectiveAgent(agent)
	extra := agentContainerExtra{Repos: row.Repos, Restore: func(context.Context, *sandbox.Sandbox) error { return nil }}
	var specs []sandbox.MCPServerSpec
	switch row.Purpose {
	case SandboxPurposeSolo:
		var session models.SoloSession
		if err := s.db.Where("id = ? AND sandbox_id = ?", row.ThreadID, row.ID).First(&session).Error; err != nil {
			return sandbox.Spec{}, fmt.Errorf("load recovery session: %w", err)
		}
		if session.Status == models.SoloStatusClosed {
			return sandbox.Spec{}, errors.New("session has been cancelled")
		}
		var err error
		agent, err = s.appliedSoloAgent(row, nil, agent)
		if err != nil {
			return sandbox.Spec{}, err
		}
		specs = s.soloPlatformSpecs(row)
		extra.LaunchKind, extra.InitialChatModel = "solo", appliedChatModelID(&session)
		extra.InitialChatReasoningEffort = &session.AppliedReasoningEffort
		extra.InitialChatServiceTier = &session.AppliedServiceTier
	case "test":
		specs = s.buildTestSandboxSpecs(agent, s.mcpVars(row.RunID, row.Token))
		extra.LaunchKind = "test"
	default:
		return sandbox.Spec{}, fmt.Errorf("unknown sandbox purpose %q", row.Purpose)
	}
	prepared, err := s.prepareAgentContainer(row.Profile, row.ThreadID, row.RunID, row.Token, row.Name, specs, agent, extra)
	if err != nil {
		return sandbox.Spec{}, err
	}
	row.ModelBindingFingerprint = prepared.modelFingerprint
	row.GitBindingsFingerprint = gitBindingsFingerprint(prepared.spec.GitCredentials)
	return prepared.spec, nil
}

func (s *SandboxService) prepareRunRecoveryInputs(row *models.Sandbox) (sandbox.Spec, error) {
	if s.runRecoveryPreparer == nil {
		return sandbox.Spec{}, errors.New("workflow recovery configuration builder is unavailable")
	}
	var run models.Run
	if err := s.db.First(&run, "id = ?", row.RunID).Error; err != nil {
		return sandbox.Spec{}, fmt.Errorf("load recovery workflow: %w", err)
	}
	if run.Status == "cancelled" {
		return sandbox.Spec{}, errors.New("workflow has been cancelled")
	}
	var node *models.Node
	for i := range run.Graph.Nodes {
		if run.Graph.Nodes[i].ID == row.NodeID {
			node = &run.Graph.Nodes[i]
			break
		}
	}
	if node == nil || models.AgentProfile(node.Config) != row.Profile {
		return sandbox.Spec{}, errors.New("workflow recovery node configuration is missing or changed")
	}
	var variables []models.RunVariable
	if err := s.db.Where("run_id = ?", run.ID).Find(&variables).Error; err != nil {
		return sandbox.Spec{}, err
	}
	vars := make(map[string]any, len(variables))
	for _, variable := range variables {
		vars[variable.Name] = variable.Value
	}
	req := runtime.NodeReq{RunID: run.ID, WorkflowID: run.WorkflowID, WorkflowName: run.WorkflowName,
		NodeID: node.ID, NodeType: node.Type, Token: row.Token, Config: node.Config,
		Vars: vars, Repos: run.Repos, Caps: node.Caps}
	var conversation models.ReactConversation
	err := s.db.Where("run_id = ? AND node_id = ?", run.ID, node.ID).Order("iteration desc, id desc").First(&conversation).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return sandbox.Spec{}, err
	}
	if err == nil && conversation.AppliedModelSet {
		if s.skills == nil {
			return sandbox.Spec{}, errors.New("workflow recovery agent service is unavailable")
		}
		agent, ok := s.skills.Get(row.Profile)
		if !ok {
			return sandbox.Spec{}, errors.New("workflow recovery agent is unavailable")
		}
		selected, err := selectSoloConfiguration(s.db, s.effectiveAgent(agent), conversation.AppliedConfigurationID)
		if err != nil {
			return sandbox.Spec{}, err
		}
		req.ChatRoute = &runtime.ChatRoute{Set: true, Backend: selected.AcpBackend,
			ConfigurationID: selected.ModelBinding.ConfigurationID, ModelID: conversation.AppliedModelID,
			ReasoningEffort: conversation.AppliedReasoningEffort, ServiceTier: conversation.AppliedServiceTier}
		req.AppliedRoute = req.ChatRoute
	}
	return s.runRecoveryPreparer.PrepareSandboxRecovery(req)
}
