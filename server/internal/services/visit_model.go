package services

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// VisitModelSwitchFailed is the stable notice code after a sandbox align rolls back.
const VisitModelSwitchFailed = "model_switch_failed"

// VisitModelChoice is the selection stored for one node visit. Empty model
// follows that visit's Agent primary model. It is not an Agent binding.
type VisitModelChoice struct {
	RunID                  string `json:"runId"`
	NodeID                 string `json:"nodeId"`
	Iteration              int    `json:"iteration"`
	CurrentConfigurationID string `json:"currentConfigurationId,omitempty"`
	CurrentModelID         string `json:"currentModelId,omitempty"`
	CurrentModelSet        bool   `json:"currentModelSet"`
	ReasoningEffort        string `json:"reasoningEffort,omitempty"`
	ModelNotice            string `json:"modelNotice,omitempty"`
	Backend                string `json:"backend,omitempty"`
}

func visitModelChoice(conv models.ReactConversation, backend string) VisitModelChoice {
	return VisitModelChoice{
		RunID: conv.RunID, NodeID: conv.NodeID, Iteration: conv.Iteration,
		CurrentConfigurationID: conv.CurrentConfigurationID, CurrentModelID: conv.CurrentModelID,
		CurrentModelSet: conv.CurrentModelSet, ReasoningEffort: conv.CurrentReasoningEffort,
		ModelNotice: conv.ModelNotice, Backend: backend,
	}
}

// GetVisitModel reads the choice for one visit. A missing conversation is not found.
func GetVisitModel(db *gorm.DB, runID, nodeID string, iteration int) (VisitModelChoice, error) {
	conv, err := loadVisitConversation(db, runID, nodeID, iteration)
	if err != nil {
		return VisitModelChoice{}, err
	}
	return visitModelChoice(conv, ""), nil
}

// SetVisitModel stores the next chat turn's model. It does not touch the
// sandbox, the Agent binding, or the workflow definition. Omitted pointers
// keep the stored value. An empty model selects the primary model.
func SetVisitModel(db *gorm.DB, agent Agent, runID, nodeID string, iteration int, configurationID, modelID, effort *string) (VisitModelChoice, error) {
	conv, err := loadVisitConversation(db, runID, nodeID, iteration)
	if err != nil {
		return VisitModelChoice{}, err
	}
	if conv.Done {
		return VisitModelChoice{}, soloErr(http.StatusConflict, "session_closed", "这次访问已经结束")
	}
	nextConfig := conv.CurrentConfigurationID
	if !conv.CurrentModelSet {
		nextConfig = ""
	}
	if configurationID != nil {
		nextConfig = strings.TrimSpace(*configurationID)
	}
	nextModel := conv.CurrentModelID
	if !conv.CurrentModelSet {
		nextModel = ""
	}
	if modelID != nil {
		nextModel = strings.TrimSpace(*modelID)
	}
	nextEffort := conv.CurrentReasoningEffort
	if !conv.CurrentModelSet {
		nextEffort = ""
	}
	if effort != nil {
		nextEffort = strings.TrimSpace(*effort)
	}
	storedConfig, storedModel, storedEffort, backend, err := acceptVisitSelection(db, agent, nextConfig, nextModel, nextEffort)
	if err != nil {
		return VisitModelChoice{}, err
	}
	if err := db.Model(&models.ReactConversation{}).Where("id = ?", conv.ID).Updates(map[string]any{
		"current_configuration_id": storedConfig,
		"current_model_id":         storedModel,
		"current_model_set":        true,
		"current_reasoning_effort": storedEffort,
		"model_notice":             "",
	}).Error; err != nil {
		return VisitModelChoice{}, err
	}
	conv.CurrentConfigurationID = storedConfig
	conv.CurrentModelID = storedModel
	conv.CurrentModelSet = true
	conv.CurrentReasoningEffort = storedEffort
	conv.ModelNotice = ""
	return visitModelChoice(conv, backend), nil
}

func loadVisitConversation(db *gorm.DB, runID, nodeID string, iteration int) (models.ReactConversation, error) {
	if iteration < 1 {
		iteration = 1
	}
	var conv models.ReactConversation
	err := db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iteration).Order("id desc").First(&conv).Error
	if err != nil {
		return models.ReactConversation{}, soloErr(http.StatusNotFound, "session_not_found", "这次访问还没有对话")
	}
	return conv, nil
}

// acceptVisitSelection checks a choice against the Agent's bound groups.
// The primary model is stored as empty. The Agent row is not modified.
func acceptVisitSelection(db *gorm.DB, agent Agent, configurationID, modelID, effort string) (storedConfig, storedModel, storedEffort, backend string, err error) {
	selected, err := selectSoloConfiguration(db, agent, configurationID)
	if err != nil {
		return "", "", "", "", err
	}
	if selected.ModelBinding == nil {
		return "", "", "", "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	configuration, err := NewModelConfigurationService(db).Get(selected.ModelBinding.ConfigurationID)
	if err != nil || configuration.State != "active" || configuration.Backend != strings.TrimSpace(selected.AcpBackend) {
		return "", "", "", "", soloErr(http.StatusBadRequest, "model_unavailable", "所选模型组不存在或已停用")
	}
	modelID = strings.TrimSpace(modelID)
	if modelID != "" && !modelrouting.AcceptsConfiguredModel(configuration.Binding, modelID) {
		return "", "", "", "", soloErr(http.StatusBadRequest, "model_not_in_configuration", "所选模型不在该 Agent 的模型配置中")
	}
	if modelID != "" && configuration.Binding.Kind == "connection" {
		connection, connErr := NewModelConnectionService(db).Get(configuration.Binding.ConnectionID)
		if connErr != nil || connection.State != "active" || !modelrouting.ModelInCatalog(connection, modelID) {
			return "", "", "", "", soloErr(http.StatusBadRequest, "model_unavailable", "所选模型已不可用")
		}
	}
	if modelID == strings.TrimSpace(configuration.Binding.ModelID) {
		modelID = ""
	}
	effort = strings.TrimSpace(effort)
	if err := acceptVisitEffort(db, configuration, modelID, effort); err != nil {
		return "", "", "", "", err
	}
	return configuration.ID, modelID, effort, configuration.Backend, nil
}

func acceptVisitEffort(db *gorm.DB, configuration modelrouting.Configuration, modelID, effort string) error {
	if effort == "" {
		return nil
	}
	id := modelID
	if id == "" {
		id = strings.TrimSpace(configuration.Binding.ModelID)
	}
	var connection *modelrouting.Connection
	if configuration.Binding.Kind == "connection" {
		loaded, err := NewModelConnectionService(db).Get(configuration.Binding.ConnectionID)
		if err != nil || loaded.State != "active" {
			return soloErr(http.StatusBadRequest, "reasoning_effort_unavailable", "所选模型不支持该推理强度")
		}
		connection = &loaded
	}
	for _, choice := range modelrouting.ReasoningEfforts(configuration.Backend, id, connection) {
		if choice == effort {
			return nil
		}
	}
	return soloErr(http.StatusBadRequest, "reasoning_effort_unavailable", "所选模型不支持该推理强度")
}
