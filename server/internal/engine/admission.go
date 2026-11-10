package engine

import (
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (e *Engine) StartAdmittedRun(input services.RunStart) (*models.Run, error) {
	if e.IsHalted() {
		return nil, errors.New("server is shutting down")
	}
	var def models.WorkflowDef
	var graph models.Graph
	if input.Published {
		loaded, snapshot, err := e.publishedSnapshot(input.WorkflowID)
		if err != nil {
			return nil, err
		}
		def, graph = loaded, snapshot.Graph
		if snapshot.Name != "" {
			def.Name = snapshot.Name
		}
	} else {
		if err := e.db.First(&def, "id = ?", input.WorkflowID).Error; err != nil {
			return nil, err
		}
		graph = def.Graph
	}
	if input.ExpectedVersion > 0 && input.ExpectedVersion != def.Version {
		return nil, &services.AdmissionError{Code: "workflow_version_changed", Message: "工作流发布版本已变化，请重新选择并确认启动配置", Status: http.StatusConflict}
	}
	priority, err := models.ParsePriorityLabel(input.Priority)
	if err != nil {
		return nil, err
	}
	launch := LaunchChoice{Set: input.ModelSet, ConfigurationID: input.ConfigurationID, ModelID: input.ModelID, ReasoningEffort: input.ReasoningEffort}
	return e.startRun(def, graph, input.Inputs, input.Trigger, priority, input.Tags, input.Env, input.Title, input.FirstMessage, launch, input)
}

var errLaunchReplay = errors.New("existing workflow launch")

// reserveLaunch runs inside the Run transaction, before the Run is inserted.
// The admission lock also makes the backlog bound and feature switch atomic.
func reserveLaunch(tx *gorm.DB, run *models.Run, input services.RunStart) (string, error) {
	if err := services.LockWorkflowAdmission(tx); err != nil {
		return "", err
	}
	if err := services.CheckWorkflowsEnabled(tx); err != nil {
		return "", err
	}
	if input.SoloID != "" {
		var solo models.SoloSession
		if err := tx.Select("id").Where("id = ? AND user_id = ?", input.SoloID, input.ActorID).First(&solo).Error; err != nil {
			return "", &services.AdmissionError{Code: "solo_not_found", Message: "Solo 不存在或无权访问", Status: http.StatusNotFound}
		}
	}
	key, fingerprint, err := input.Identity()
	if err != nil {
		return "", err
	}
	if key != "" {
		launch := models.WorkflowLaunch{Key: key, Fingerprint: fingerprint, RunID: run.ID, Source: input.Source, ActorID: input.ActorID, SoloID: input.SoloID}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&launch)
		if result.Error != nil {
			return "", result.Error
		}
		if result.RowsAffected == 0 {
			var previous models.WorkflowLaunch
			if err := tx.First(&previous, "key = ?", key).Error; err != nil {
				return "", err
			}
			if previous.Fingerprint != fingerprint {
				return "", &services.AdmissionError{Code: "idempotency_conflict", Message: "同一启动标识不能用于不同的工作流配置", Status: http.StatusConflict}
			}
			return previous.RunID, errLaunchReplay
		}
	}
	limit := 1000
	if cfg := config.GetConfig(); cfg != nil && cfg.Features.MaxActiveWorkflows > 0 {
		limit = cfg.Features.MaxActiveWorkflows
	}
	var count int64
	if err := tx.Model(&models.Run{}).Where("status IN ?", []string{"queued", "running", "waiting_human", "paused"}).Count(&count).Error; err != nil {
		return "", err
	}
	if count >= int64(limit) {
		return "", &services.AdmissionError{Code: "workflow_capacity", Message: "待处理工作流已达到容量上限，请稍后重试", Status: http.StatusTooManyRequests}
	}
	run.OriginSoloID, run.CreatedBy, run.LaunchSource = input.SoloID, input.ActorID, input.Source
	return "", nil
}
