package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

func (s *SoloService) modelAligner() soloModelAligner {
	if s.modelAlign != nil {
		return s.modelAlign
	}
	if s.sbx != nil {
		return s.sbx
	}
	return nil
}

// acceptInitialModel checks a Solo start selection against the Agent's current
// model configuration. Empty means the primary model. The configuration itself
// is never updated.
func (s *SoloService) acceptInitialModel(agent Agent, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return "", nil
	}
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" || strings.TrimSpace(agent.ModelBinding.ConfigurationID) == "" {
		return "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != strings.TrimSpace(agent.AcpBackend) {
		return "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, requested) {
		return "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	if configuration.Binding.Kind == "connection" {
		connection, connErr := NewModelConnectionService(s.db).Get(configuration.Binding.ConnectionID)
		if connErr != nil || !modelrouting.ModelInCatalog(connection, requested) {
			return "", soloErr(http.StatusBadRequest, "model_unavailable", "所选模型已不可用")
		}
	}
	if requested == strings.TrimSpace(configuration.Binding.ModelID) {
		return "", nil
	}
	return requested, nil
}

// SetCurrentModel stores the model later turns will use. Empty means the
// primary model. The Agent configuration is not written. A turn that is
// already generating keeps its model; the sandbox is updated before the next
// turn starts.
func (s *SoloService) SetCurrentModel(ctx context.Context, id, user, requested string) (*SoloSessionView, error) {
	return s.SetCurrentModelSettings(ctx, id, user, &requested, nil)
}

// SetCurrentModelSettings preserves omitted settings and validates before any
// write. An effort-only request never changes the selected model.
func (s *SoloService) SetCurrentModelSettings(ctx context.Context, id, user string, requested, requestedEffort *string) (*SoloSessionView, error) {
	return s.SetCurrentModelRouteSettings(ctx, id, user, nil, requested, requestedEffort)
}

// SetCurrentModelRouteSettings stores a group-qualified choice without interrupting
// the active turn. Omitted group keeps the current group; empty selects the default.
func (s *SoloService) SetCurrentModelRouteSettings(ctx context.Context, id, user string, requestedConfiguration, requested, requestedEffort *string) (*SoloSessionView, error) {
	row, err := s.Require(id, user)
	if err != nil {
		return nil, err
	}
	if row.Status == models.SoloStatusClosed {
		return nil, soloErr(http.StatusConflict, "session_closed", "会话已关闭，请先重新启动")
	}
	if row.Status == models.SoloStatusError {
		return nil, soloErr(http.StatusConflict, "session_error", "沙箱不可用，请重新启动会话")
	}
	if s.agents == nil {
		return nil, errors.New("solo unavailable")
	}
	agent, ok := s.agents.Get(row.AgentName)
	if !ok {
		return nil, soloErr(http.StatusBadRequest, "agent_not_found", "Agent %q 不存在", row.AgentName)
	}
	if s.sbx != nil {
		agent = s.sbx.effectiveAgent(agent)
	}
	configurationID := chatConfigurationID(row)
	if requestedConfiguration != nil {
		configurationID = *requestedConfiguration
	}
	agent, err = selectSoloConfiguration(s.db, agent, configurationID)
	if err != nil {
		return nil, err
	}
	configurationID = agent.ModelBinding.ConfigurationID
	model := chatModelID(row)
	if requested != nil {
		model = *requested
	} else if requestedConfiguration != nil && configurationID != chatConfigurationID(row) {
		model = ""
	}
	stored, err := s.acceptInitialModel(agent, model)
	if err != nil {
		return nil, err
	}
	effort := strings.TrimSpace(row.CurrentReasoningEffort)
	if pinned, locked := s.lockedEffort(agent, stored); locked {
		if requestedEffort != nil {
			asked := strings.TrimSpace(*requestedEffort)
			if asked != "" && asked != pinned {
				return nil, s.validateReasoningEffort(agent, stored, asked)
			}
		}
		effort = pinned
	} else if requestedEffort != nil {
		effort = strings.TrimSpace(*requestedEffort)
		if err := s.validateSessionReasoningEffort(ctx, row, agent, stored, effort); err != nil {
			return nil, err
		}
	} else if s.validateSessionReasoningEffort(ctx, row, agent, stored, effort) != nil {
		effort = ""
	}
	if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{
		"current_configuration_id": configurationID, "current_model_id": stored, "current_model_set": true, "current_reasoning_effort": effort, "model_notice": "",
	}).Error; err != nil {
		return nil, err
	}
	c := s.acquire(row.ID)
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	view, err := s.Get(ctx, row.ID, user)
	if err != nil {
		return nil, err
	}
	// This only announces the next-turn selection. The active CLI and its
	// applied route stay untouched until alignSessionModel runs at a boundary.
	pendingChange := ""
	if chatConfigurationID(&view.SoloSession) != appliedChatConfigurationID(&view.SoloSession) || chatModelID(&view.SoloSession) != appliedChatModelID(&view.SoloSession) {
		pendingChange = "model"
	} else if view.CurrentReasoningEffort != view.AppliedReasoningEffort {
		pendingChange = "effort"
	}
	c.publish(SoloEventModel, map[string]any{
		"currentConfigurationId": view.CurrentConfigurationID, "currentModelId": view.CurrentModelID, "currentModelSet": view.CurrentModelSet,
		"reasoningEffort": view.CurrentReasoningEffort, "reasoningEfforts": view.ReasoningEfforts,
		"backend": view.Backend, "modelNotice": view.ModelNotice, "pendingChange": pendingChange,
	})
	return view, nil
}

func (s *SoloService) reasoningEfforts(agent Agent, requestedModel string) []string {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" {
		return nil
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != strings.TrimSpace(agent.AcpBackend) {
		return nil
	}
	id := strings.TrimSpace(requestedModel)
	if id == "" {
		id = strings.TrimSpace(configuration.Binding.ModelID)
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, id) {
		return nil
	}
	var connection *modelrouting.Connection
	if configuration.Binding.Kind == "connection" {
		value, err := NewModelConnectionService(s.db).Get(configuration.Binding.ConnectionID)
		if err != nil || value.State != "active" {
			return nil
		}
		connection = &value
	}
	efforts := modelrouting.ReasoningEfforts(configuration.Backend, id, connection)
	if configuration.Backend == "opencode" {
		efforts = modelrouting.SessionEffortChoices(efforts, configuration.Binding.ToolSettings.ModelEfforts[id])
	}
	return efforts
}

// lockedEffort is a custom token saved for this model. It is not a chat menu
// entry: the session keeps it and cannot pick another level.
func (s *SoloService) lockedEffort(agent Agent, model string) (string, bool) {
	pinned := s.configuredOpenCodeEffort(agent, model)
	if pinned == "" {
		return "", false
	}
	for _, option := range s.reasoningEfforts(agent, model) {
		if option == pinned {
			return "", false
		}
	}
	return pinned, true
}

// configuredOpenCodeEffort is the variant saved on the model group. An empty
// result means the run uses OpenCode's default. A session can still change the
// variant afterwards without writing it back here.
func (s *SoloService) configuredOpenCodeEffort(agent Agent, model string) string {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" || strings.TrimSpace(agent.AcpBackend) != "opencode" {
		return ""
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != "opencode" {
		return ""
	}
	id := strings.TrimSpace(model)
	if id == "" {
		id = strings.TrimSpace(configuration.Binding.ModelID)
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, id) {
		return ""
	}
	effort := strings.TrimSpace(configuration.Binding.ToolSettings.ModelEfforts[id])
	if !modelrouting.ValidEffortToken(effort) {
		return ""
	}
	return effort
}

func (s *SoloService) validateReasoningEffort(agent Agent, model, effort string) error {
	if effort == "" {
		return nil
	}
	if pinned := s.configuredOpenCodeEffort(agent, model); pinned != "" && effort == pinned {
		return nil
	}
	for _, option := range s.reasoningEfforts(agent, model) {
		if option == effort {
			return nil
		}
	}
	return soloErr(http.StatusBadRequest, "reasoning_effort_unavailable", "所选模型不支持该推理强度")
}

// liveReasoningEfforts uses a running DSH session's negotiated ACP choices only
// for that exact current model. It never assigns those choices to another model
// or writes them into the shared model catalog.
func (s *SoloService) liveReasoningEfforts(ctx context.Context, row *models.SoloSession, agent Agent, model string) []string {
	if s.sbx == nil || row.SandboxID == 0 || row.Status != models.SoloStatusRunning || agent.AcpBackend != "deepseek_harness" || agent.ModelBinding == nil || chatConfigurationID(row) != appliedChatConfigurationID(row) || agent.ModelBinding.ConfigurationID != appliedChatConfigurationID(row) {
		return nil
	}
	configuration, err := NewModelConfigurationService(s.db).Get(agent.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != agent.AcpBackend {
		return nil
	}
	id := strings.TrimSpace(model)
	if id == "" {
		id = strings.TrimSpace(configuration.Binding.ModelID)
	}
	if !modelrouting.AcceptsConfiguredModel(configuration.Binding, id) {
		return nil
	}
	if configuration.Binding.Kind == "connection" {
		connection, err := NewModelConnectionService(s.db).Get(configuration.Binding.ConnectionID)
		if err != nil || connection.State != "active" {
			return nil
		}
		found := false
		for _, candidate := range connection.Models {
			if candidate.ID == id {
				found = true
				if candidate.Capabilities.Reasoning != nil && !*candidate.Capabilities.Reasoning {
					return nil
				}
			}
		}
		if !found {
			return nil
		}
	}
	state, err := s.sbx.ChatModelSettings(ctx, row.SandboxID)
	if err != nil {
		return nil
	}
	selected, err := s.acceptInitialModel(agent, state.Selected)
	if err != nil {
		return nil
	}
	wanted, err := s.acceptInitialModel(agent, model)
	if err != nil || selected != wanted {
		return nil
	}
	if state.ReasoningEfforts == nil {
		return nil
	}
	return append([]string{}, state.ReasoningEfforts...)
}

func (s *SoloService) validateSessionReasoningEffort(ctx context.Context, row *models.SoloSession, agent Agent, model, effort string) error {
	if effort == "" {
		return nil
	}
	if choices := s.liveReasoningEfforts(ctx, row, agent, model); choices != nil {
		for _, choice := range choices {
			if choice == effort {
				return nil
			}
		}
		return soloErr(http.StatusBadRequest, "reasoning_effort_unavailable", "所选模型不支持该推理强度")
	}
	return s.validateReasoningEffort(agent, model, effort)
}

func (s *SoloService) rememberAppliedRoute(ctx context.Context, expected *models.SoloSession, configuration, model, effort string) error {
	// The caller owns the captured lifecycle. Loading a fresh session here
	// would turn a delayed old native result into authority over a new reopen.
	return s.executionTransaction(ctx, expected.ID, func(tx *gorm.DB) error {
		if _, err := s.requireSoloLifecycle(tx, expected); err != nil {
			return err
		}
		result := soloLifecycleQuery(tx, expected).Where("lifecycle_action = '' AND status <> ?", models.SoloStatusClosed).Updates(map[string]any{
			"applied_configuration_id": configuration, "applied_model_id": model, "applied_model_set": true, "applied_reasoning_effort": effort, "model_notice": "",
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return soloLifecycleChanged()
		}
		return nil
	})
}

func (s *SoloService) markSoloModelContext(ctx context.Context, expected *models.SoloSession) error {
	result := soloLifecycleQuery(s.db.WithContext(ctx), expected).Where("lifecycle_action = '' AND status <> ?", models.SoloStatusClosed).
		Update("model_context_pending", true)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return soloLifecycleChanged()
	}
	return nil
}

// chatModelID is the model the next turn should use. An old row that never
// stored a current model still follows its opening model.
func chatModelID(row *models.SoloSession) string {
	if row == nil {
		return ""
	}
	if row.CurrentModelSet {
		return strings.TrimSpace(row.CurrentModelID)
	}
	return strings.TrimSpace(row.InitialModelID)
}

// appliedChatModelID is the model the sandbox chat was last given.
func appliedChatModelID(row *models.SoloSession) string {
	if row == nil {
		return ""
	}
	if row.AppliedModelSet {
		return strings.TrimSpace(row.AppliedModelID)
	}
	return strings.TrimSpace(row.InitialModelID)
}

// alignSessionModel pushes the session's current model to the sandbox when it
// differs from the model already applied. Failure restores the previous model
// and reports it to the open page. The in-flight turn is left alone; this runs
// only at the start of a turn.
func (s *SoloService) alignSessionModel(ctx context.Context, c *soloChat, sandboxID uint, row *models.SoloSession) (bool, error) {
	desired, applied := chatModelID(row), appliedChatModelID(row)
	desiredConfiguration, appliedConfiguration := chatConfigurationID(row), appliedChatConfigurationID(row)
	defaultConfiguration := s.defaultConfigurationID(row.AgentName)
	if desiredConfiguration == "" {
		desiredConfiguration = defaultConfiguration
	}
	if appliedConfiguration == "" {
		appliedConfiguration = defaultConfiguration
	}
	storedEffort := strings.TrimSpace(row.CurrentReasoningEffort)
	desiredEffort := s.resolveChatEffort(ctx, row, desired, storedEffort)
	appliedEffort := strings.TrimSpace(row.AppliedReasoningEffort)
	var changed bool
	var err error
	if s.sbx != nil && s.modelAlign == nil {
		var agent Agent
		agent, err = s.sessionAgent(row)
		if err != nil {
			return false, err
		}
		changed, err = s.sbx.AlignSoloModelRoute(ctx, sandboxID, agent)
	}
	routeChanged := changed
	if err == nil && !changed && !row.ModelContextPending && desired == applied && desiredEffort == appliedEffort && desiredConfiguration == appliedConfiguration {
		if err := s.persistResolvedEffort(row, storedEffort, desiredEffort); err != nil {
			return false, err
		}
		return false, nil
	}
	if err == nil {
		aligner := s.modelAligner()
		if aligner == nil {
			return changed, nil
		}
		// Mark before the mutation: the bridge may restart its CLI and then lose
		// the response. A retry must still supply the task's existing context.
		if err := s.markSoloModelContext(ctx, row); err != nil {
			return false, err
		}
		var modelChanged bool
		if settings, ok := aligner.(soloSettingsAligner); ok {
			modelChanged, err = settings.AlignChatSettings(ctx, sandboxID, desired, desiredEffort)
		} else if desiredEffort != appliedEffort {
			err = errors.New("sandbox reasoning settings unavailable")
		} else {
			modelChanged, err = aligner.AlignChatModel(ctx, sandboxID, desired)
		}
		changed = changed || modelChanged
	}
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ErrChatModelUnknown) {
			return false, err
		}
		if routeChanged && s.sbx != nil {
			previous := *row
			previous.CurrentConfigurationID = appliedConfiguration
			previous.CurrentModelID, previous.CurrentModelSet = applied, true
			oldAgent, rollbackErr := s.sessionAgent(&previous)
			if rollbackErr == nil {
				_, rollbackErr = s.sbx.AlignSoloModelRoute(ctx, sandboxID, oldAgent)
			}
			if rollbackErr != nil {
				return false, fmt.Errorf("切换失败且恢复原执行环境失败: %w", errors.Join(err, rollbackErr))
			}
		}
		res := soloLifecycleQuery(s.db, row).Where("lifecycle_action = '' AND status <> ? AND current_model_id = ? AND current_model_set = ? AND COALESCE(current_reasoning_effort, '') = ? AND COALESCE(current_configuration_id, '') = ?", models.SoloStatusClosed, row.CurrentModelID, row.CurrentModelSet, row.CurrentReasoningEffort, row.CurrentConfigurationID).Updates(map[string]any{
			"current_configuration_id": appliedConfiguration, "current_model_id": applied, "current_model_set": true, "current_reasoning_effort": appliedEffort, "model_notice": soloModelSwitchFailed,
		})
		if res.Error == nil && res.RowsAffected > 0 {
			c.publish(SoloEventModel, map[string]any{
				"currentConfigurationId": appliedConfiguration, "currentModelId": applied, "currentModelSet": true, "reasoningEffort": appliedEffort,
				"modelNotice": soloModelSwitchFailed, "error": "切换模型设置失败，已恢复原来的设置",
			})
		}
		log.Warn().Err(err).Str("session", row.ID).Str("configuration", desiredConfiguration).Str("model", desired).Msg("solo model switch rolled back")
		return false, err
	}
	changed = changed || row.ModelContextPending || desired != applied || desiredEffort != appliedEffort || desiredConfiguration != appliedConfiguration
	if changed {
		if err := s.markSoloModelContext(ctx, row); err != nil {
			return false, err
		}
	}
	if err := s.persistResolvedEffort(row, storedEffort, desiredEffort); err != nil {
		return false, err
	}
	if err := s.rememberAppliedRoute(ctx, row, desiredConfiguration, desired, desiredEffort); err != nil {
		return false, err
	}
	return changed, nil
}

// resolveChatEffort keeps a saved level and fills an empty one with the last
// advertised choice. Models without a list stay empty, including a custom lock
// that hides the menu.
func (s *SoloService) resolveChatEffort(ctx context.Context, row *models.SoloSession, model, effort string) string {
	effort = strings.TrimSpace(effort)
	if effort != "" || row == nil {
		return effort
	}
	agent, err := s.sessionAgent(row)
	if err != nil {
		return ""
	}
	choices := s.liveReasoningEfforts(ctx, row, agent, model)
	if choices == nil {
		choices = s.reasoningEfforts(agent, model)
	}
	if len(choices) == 0 {
		return ""
	}
	return choices[len(choices)-1]
}

func (s *SoloService) persistResolvedEffort(row *models.SoloSession, stored, resolved string) error {
	if row == nil || resolved == stored {
		return nil
	}
	res := soloLifecycleQuery(s.db, row).Where("lifecycle_action = '' AND status <> ? AND COALESCE(current_reasoning_effort, '') = ?", models.SoloStatusClosed, stored).Update("current_reasoning_effort", resolved)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		row.CurrentReasoningEffort = resolved
	}
	return nil
}
