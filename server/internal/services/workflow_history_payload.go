package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type preparedWorkflowHistory struct {
	runID, soloID, userID, status, linkID string
	eventVersion                          uint64
	controlVersion                        uint64
	attempt                               int
	message                               preparedSoloMessage
}

type workflowHistorySource struct {
	run         models.Run
	runBodies   struct{ Inputs, FirstMessage string }
	states      []models.StateRun
	stateBodies []struct {
		ID              uint
		Outputs, Events string
	}
	conversations      []models.ReactConversation
	conversationBodies []struct {
		ID       uint
		Messages string
	}
	artifacts []struct {
		ID, Name, Content string
		Revision          int
	}
	instructions      []models.WorkflowInstruction
	instructionBodies []struct{ ID, Text string }
	booting           []models.Sandbox
}

// captureWorkflowHistory reads only SQL cells. It never invokes a file
// serializer, including through ConversationChain's legacy loading helpers.
func captureWorkflowHistory(ctx context.Context, db *gorm.DB, runID string) (workflowHistorySource, error) {
	var source workflowHistorySource
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("id", "workflow_name", "title", "status", "graph", "event_version", "control_version", "attempt").First(&source.run, "id = ?", runID).Error; err != nil {
			return err
		}
		if !workflowHistoryTerminal(source.run.Status) {
			return soloErr(409, "workflow_still_running", "工作流状态已变化，请刷新后重试")
		}
		if err := tx.Table("runs").Select("inputs", "first_message").Where("id = ?", runID).Take(&source.runBodies).Error; err != nil {
			return err
		}
		columns := []string{}
		for _, column := range chainStateColumns {
			if column != "outputs" && column != "vars_before" {
				columns = append(columns, column)
			}
		}
		if err := tx.Select(columns).Where("run_id = ?", runID).Order("id asc").Find(&source.states).Error; err != nil {
			return err
		}
		if err := tx.Table("state_runs").Select("id", "outputs", "events").Where("run_id = ?", runID).Find(&source.stateBodies).Error; err != nil {
			return err
		}
		if err := tx.Select("id", "node_id", "iteration").Where("run_id = ?", runID).Order("id asc").Find(&source.conversations).Error; err != nil {
			return err
		}
		if err := tx.Table("react_conversations").Select("id", "messages").Where("run_id = ?", runID).Find(&source.conversationBodies).Error; err != nil {
			return err
		}
		if err := tx.Table("artifacts").Select("id", "name", "revision", "content").Where("run_id = ?", runID).Order("created_at asc, id asc").Find(&source.artifacts).Error; err != nil {
			return err
		}
		if err := tx.Omit("text").Where("run_id = ?", runID).Order("created_at,id").Find(&source.instructions).Error; err != nil {
			return err
		}
		if err := tx.Table("workflow_instructions").Select("id", "text").Where("run_id = ?", runID).Find(&source.instructionBodies).Error; err != nil {
			return err
		}
		return tx.Select("node_id", "status").Where("run_id = ? AND purpose = ? AND status IN ?", runID, "run", []string{"pulling", "creating"}).Find(&source.booting).Error
	})
	return source, err
}

func loadWorkflowJSON(ctx context.Context, files *filestore.Store, raw string, target any) error {
	if raw == "" || raw == "null" {
		return nil
	}
	body, err := files.Load(ctx, raw, filestore.JSON)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, target)
}

// Preserve the historical Artifact serializer's image/binary representation:
// image context carries base64; a binary file retains its opaque reference.
func loadWorkflowArtifact(ctx context.Context, files *filestore.Store, raw string) (string, error) {
	if !filestore.IsRef(raw) {
		body, err := files.Load(ctx, raw, filestore.Text)
		return string(body), err
	}
	encoding, _, err := filestore.Parse(raw)
	if err != nil {
		return "", err
	}
	if encoding == filestore.Binary {
		return raw, nil
	}
	body, err := files.Load(ctx, raw, encoding)
	if encoding == filestore.Image {
		return base64.StdEncoding.EncodeToString(body), err
	}
	return string(body), err
}

func prepareWorkflowHistory(ctx context.Context, db *gorm.DB, session *models.SoloSession, runID string) (*preparedWorkflowHistory, error) {
	if runID == "" {
		return nil, nil
	}
	if err := messageOutsideTransaction(db); err != nil {
		return nil, err
	}
	source, err := captureWorkflowHistory(ctx, db, runID)
	if err != nil {
		return nil, err
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return nil, err
	}
	if err := loadWorkflowJSON(ctx, files, source.runBodies.Inputs, &source.run.Inputs); err != nil {
		return nil, err
	}
	if err := loadWorkflowJSON(ctx, files, source.runBodies.FirstMessage, &source.run.FirstMessage); err != nil {
		return nil, err
	}
	counts := map[uint]int{}
	stateIndex := map[uint]int{}
	for i, body := range source.stateBodies {
		stateIndex[body.ID] = i
	}
	for i := range source.states {
		index, ok := stateIndex[source.states[i].ID]
		if !ok {
			return nil, errors.New("captured workflow execution payload is missing")
		}
		body := source.stateBodies[index]
		if err := loadWorkflowJSON(ctx, files, body.Outputs, &source.states[i].Outputs); err != nil {
			return nil, err
		}
		var events []models.AcpEvent
		if err := loadWorkflowJSON(ctx, files, body.Events, &events); err != nil {
			return nil, err
		}
		counts[body.ID] = len(events)
	}
	conversationIndex := map[uint]int{}
	for i, body := range source.conversationBodies {
		conversationIndex[body.ID] = i
	}
	for i := range source.conversations {
		index, ok := conversationIndex[source.conversations[i].ID]
		if !ok {
			return nil, errors.New("captured workflow conversation payload is missing")
		}
		if err := loadWorkflowJSON(ctx, files, source.conversationBodies[index].Messages, &source.conversations[i].Messages); err != nil {
			return nil, err
		}
	}
	instructionIndex := map[string]int{}
	for i, body := range source.instructionBodies {
		instructionIndex[body.ID] = i
	}
	for i := range source.instructions {
		index, ok := instructionIndex[source.instructions[i].ID]
		if !ok {
			return nil, errors.New("captured workflow instruction payload is missing")
		}
		data, err := files.Load(ctx, source.instructionBodies[index].Text, filestore.Text)
		if err != nil {
			return nil, err
		}
		source.instructions[i].Text = string(data)
	}
	booting := map[string]string{}
	for _, row := range source.booting {
		booting[row.NodeID] = row.Status
	}
	segments, _, err := buildConversationChain(source.run, source.states, counts, source.conversations, booting, "", 0)
	if err != nil {
		return nil, err
	}
	results := make([]WorkflowArtifactRef, 0, len(source.artifacts))
	for _, artifact := range source.artifacts {
		content, err := loadWorkflowArtifact(ctx, files, artifact.Content)
		if err != nil {
			return nil, err
		}
		results = append(results, WorkflowArtifactRef{ID: artifact.ID, Name: artifact.Name, Version: artifact.Revision, Content: content})
	}
	payload, err := json.Marshal(struct {
		RunID        string                       `json:"runId"`
		Workflow     string                       `json:"workflow"`
		Title        string                       `json:"title"`
		Status       string                       `json:"status"`
		FirstMessage *models.CompositeText        `json:"firstMessage"`
		Inputs       map[string]any               `json:"inputs"`
		Instructions []models.WorkflowInstruction `json:"instructions,omitempty"`
		Segments     []ChainSegment               `json:"segments"`
		Artifacts    []WorkflowArtifactRef        `json:"artifacts"`
	}{source.run.ID, source.run.WorkflowName, source.run.Title, source.run.Status, source.run.FirstMessage, source.run.Inputs, source.instructions, segments, results})
	if err != nil {
		return nil, err
	}
	message, err := prepareSoloMessage(ctx, db, models.SoloMessage{ID: uuid.NewString(), SessionID: session.ID, Role: "system", WorkflowRunID: runID,
		Content: "已关联历史工作流。当前是独立新环境，尚未恢复原工作区文件或进程。", WorkflowContext: string(payload)})
	if err != nil {
		return nil, err
	}
	return &preparedWorkflowHistory{runID: runID, soloID: session.ID, userID: session.UserID, status: source.run.Status,
		eventVersion: source.run.EventVersion, controlVersion: source.run.ControlVersion, attempt: source.run.Attempt, linkID: uuid.NewString(), message: message}, nil
}

func commitWorkflowHistoryTx(tx *gorm.DB, session *models.SoloSession, prepared *preparedWorkflowHistory) error {
	if prepared == nil || prepared.soloID != session.ID || prepared.userID != session.UserID {
		return errors.New("historical context was not prepared for this conversation")
	}
	var run models.Run
	// A locking read observes the current committed row under MySQL's default
	// repeatable-read isolation and holds retry/control changes until the link
	// commits. Creation already owns Compute→Admission; Run has no path back
	// to either lock. Only SQL metadata is read while this fence is held.
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "status", "event_version", "control_version", "attempt").First(&run, "id = ?", prepared.runID).Error; err != nil {
		return err
	}
	if !workflowHistoryTerminal(run.Status) || run.Status != prepared.status || run.EventVersion != prepared.eventVersion || run.ControlVersion != prepared.controlVersion || run.Attempt != prepared.attempt {
		return soloErr(409, "workflow_history_changed", "工作流状态已变化，请刷新后重试")
	}
	if err := tx.Create(&models.SoloWorkflowLink{ID: prepared.linkID, SoloID: session.ID, RunID: run.ID, UserID: session.UserID, Kind: "reference"}).Error; err != nil {
		return err
	}
	if _, err := insertPreparedSoloMessageTx(tx, prepared.message); err != nil {
		return err
	}
	session.ModelContextPending = true
	return tx.Model(session).Update("model_context_pending", true).Error
}
