package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const workflowEventSourceVersion = 1

type workflowEventRecord struct {
	ID, RunID, SoloID, UserID, Kind, Payload, Disposition string
	Version                                               uint64
	SourceVersion, SourceCount                            int
	SourceFingerprint                                     string
	Attempts                                              int
	NextAttemptAt                                         *time.Time
	LastError                                             string
	DeliveredAt                                           *time.Time
	CreatedAt                                             time.Time
}

func (workflowEventRecord) TableName() string { return "workflow_conversation_events" }

type workflowSourceRecord struct {
	ID, EventID, Kind, Metadata                     string
	Position                                        int
	TextRef, JSONRef, BytesRef, BinaryRef, ImageRef string
	CreatedAt                                       time.Time
}

func (workflowSourceRecord) TableName() string { return "workflow_event_sources" }

type workflowSourceMetadata struct {
	Header      *workflowEventHeader        `json:"header,omitempty"`
	Artifact    *WorkflowArtifactRef        `json:"artifact,omitempty"`
	Instruction *models.WorkflowInstruction `json:"instruction,omitempty"`
	InlineBody  []byte                      `json:"inlineBody,omitempty"`
}

type workflowEventHeader struct {
	RunID, WorkflowName, Title, Status string
}

func workflowSource(eventID, kind string, position int, metadata workflowSourceMetadata, raw string) (workflowSourceRecord, error) {
	row := workflowSourceRecord{ID: uuid.NewString(), EventID: eventID, Kind: kind, Position: position}
	if filestore.IsRef(raw) {
		encoding, _, _ := filestore.Parse(raw)
		switch encoding {
		case filestore.Text:
			row.TextRef = raw
		case filestore.JSON:
			row.JSONRef = raw
		case filestore.Bytes:
			row.BytesRef = raw
		case filestore.Binary:
			row.BinaryRef = raw
		case filestore.Image:
			row.ImageRef = raw
		}
	} else {
		// This is explicitly inline provenance, not a location supplied to
		// Load. Preserve even reference-looking/invalid UTF-8 legacy bytes.
		metadata.InlineBody = []byte(raw)
	}
	data, err := json.Marshal(metadata)
	row.Metadata = string(data)
	return row, err
}

func workflowSourceFingerprint(event workflowEventRecord, sources []workflowSourceRecord) (string, error) {
	h := sha256.New()
	encoder := json.NewEncoder(h)
	if err := encoder.Encode([]any{event.ID, event.RunID, event.Version, event.SoloID, event.UserID, event.Kind, event.SourceVersion, event.SourceCount}); err != nil {
		return "", err
	}
	for _, row := range sources {
		if err := encoder.Encode([]any{row.ID, row.EventID, row.Position, row.Kind, row.Metadata, row.TextRef, row.JSONRef, row.BytesRef, row.BinaryRef, row.ImageRef}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// EnqueueWorkflowEvent shares the Run transition's most outer transaction.
// Capture raw cells only: terminal sealing, event version and immutable sources
// either all commit or all roll back. Materialization never reads live sources.
func EnqueueWorkflowEvent(tx *gorm.DB, runID, status string) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return errors.New("workflow event capture requires the Run transition transaction")
	}
	var run models.Run
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "origin_solo_id", "created_by", "workflow_name", "title").First(&run, "id = ?", runID).Error; err != nil {
		return err
	}
	if workflowHistoryTerminal(status) {
		if err := sealRunInstructions(tx, runID); err != nil {
			return err
		}
	}
	if run.OriginSoloID == "" {
		return nil
	}
	if err := tx.Model(&models.Run{}).Where("id = ?", runID).Update("event_version", gorm.Expr("COALESCE(event_version, 0) + 1")).Error; err != nil {
		return err
	}
	if err := tx.Select("event_version").First(&run, "id = ?", runID).Error; err != nil {
		return err
	}
	event := workflowEventRecord{ID: uuid.NewString(), RunID: runID, Version: run.EventVersion, SoloID: run.OriginSoloID, UserID: run.CreatedBy,
		Kind: status, Disposition: "pending", SourceVersion: workflowEventSourceVersion}
	header, err := workflowSource(event.ID, "header", 0, workflowSourceMetadata{Header: &workflowEventHeader{RunID: runID, WorkflowName: run.WorkflowName, Title: run.Title, Status: status}}, "")
	if err != nil {
		return err
	}
	sources := []workflowSourceRecord{header}
	if workflowHistoryTerminal(status) {
		sources, err = captureWorkflowResultSources(tx, event, sources)
		if err != nil {
			return err
		}
	}
	event.SourceCount = len(sources)
	event.SourceFingerprint, err = workflowSourceFingerprint(event, sources)
	if err != nil {
		return err
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	return tx.CreateInBatches(&sources, 100).Error
}

// Progress events carry only their immutable header. Results and receipts are
// frozen once at a terminal transition, never copied for every pause/resume.
func captureWorkflowResultSources(tx *gorm.DB, event workflowEventRecord, sources []workflowSourceRecord) ([]workflowSourceRecord, error) {
	var artifacts []struct {
		ID, Name, Content string
		Revision          int
	}
	if err := tx.Table("artifacts").Select("id", "name", "revision", "content").Where("run_id = ?", event.RunID).Order("id asc").Find(&artifacts).Error; err != nil {
		return nil, err
	}
	for _, artifact := range artifacts {
		row, err := workflowSource(event.ID, "artifact", len(sources), workflowSourceMetadata{Artifact: &WorkflowArtifactRef{ID: artifact.ID, Name: artifact.Name, Version: artifact.Revision}}, artifact.Content)
		if err != nil {
			return nil, err
		}
		sources = append(sources, row)
	}
	var instructions []models.WorkflowInstruction
	if err := tx.Omit("text").Where("run_id = ?", event.RunID).Order("created_at,id").Find(&instructions).Error; err != nil {
		return nil, err
	}
	var bodies []struct{ ID, Text string }
	if err := tx.Table("workflow_instructions").Select("id", "text").Where("run_id = ?", event.RunID).Find(&bodies).Error; err != nil {
		return nil, err
	}
	textByID := make(map[string]string, len(bodies))
	for _, body := range bodies {
		textByID[body.ID] = body.Text
	}
	for i := range instructions {
		body, ok := textByID[instructions[i].ID]
		if !ok {
			return nil, errors.New("captured workflow instruction source is missing")
		}
		row, err := workflowSource(event.ID, "instruction", len(sources), workflowSourceMetadata{Instruction: &instructions[i]}, body)
		if err != nil {
			return nil, err
		}
		sources = append(sources, row)
	}
	return sources, nil
}

func workflowSourceBody(row workflowSourceRecord, metadata workflowSourceMetadata) (string, string, error) {
	ref, encoding := "", ""
	for _, field := range []struct{ value, encoding string }{
		{row.TextRef, filestore.Text}, {row.JSONRef, filestore.JSON}, {row.BytesRef, filestore.Bytes}, {row.BinaryRef, filestore.Binary}, {row.ImageRef, filestore.Image},
	} {
		if field.value == "" {
			continue
		}
		if ref != "" || len(metadata.InlineBody) > 0 {
			return "", "", errors.New("workflow source has conflicting body locations")
		}
		actual, _, err := filestore.Parse(field.value)
		if err != nil || actual != field.encoding {
			return "", "", errors.New("workflow source reference has an invalid encoding")
		}
		ref, encoding = field.value, field.encoding
	}
	return ref, encoding, nil
}

func materializeWorkflowSources(ctx context.Context, db *gorm.DB, event workflowEventRecord) ([]byte, error) {
	if event.SourceVersion != workflowEventSourceVersion || event.SourceCount < 1 {
		return nil, errors.New("unsupported workflow source version or count")
	}
	var sources []workflowSourceRecord
	if err := db.WithContext(ctx).Where("event_id = ?", event.ID).Order("position asc").Find(&sources).Error; err != nil {
		return nil, err
	}
	fingerprint, err := workflowSourceFingerprint(event, sources)
	if err != nil {
		return nil, err
	}
	if len(sources) != event.SourceCount || fingerprint != event.SourceFingerprint {
		return nil, errors.New("workflow immutable sources failed their count/fingerprint check")
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return nil, err
	}
	payload := WorkflowEventPayload{Artifacts: []WorkflowArtifactRef{}}
	for i, row := range sources {
		if row.EventID != event.ID || row.Position != i {
			return nil, errors.New("workflow immutable source order changed")
		}
		var metadata workflowSourceMetadata
		if err := json.Unmarshal([]byte(row.Metadata), &metadata); err != nil {
			return nil, err
		}
		ref, encoding, err := workflowSourceBody(row, metadata)
		if err != nil {
			return nil, err
		}
		switch row.Kind {
		case "header":
			if i != 0 || metadata.Header == nil || metadata.Artifact != nil || metadata.Instruction != nil || ref != "" || len(metadata.InlineBody) != 0 || metadata.Header.RunID != event.RunID || metadata.Header.Status != event.Kind {
				return nil, errors.New("workflow immutable header does not match its event")
			}
			payload.RunID, payload.WorkflowName, payload.Title, payload.Status = metadata.Header.RunID, metadata.Header.WorkflowName, metadata.Header.Title, metadata.Header.Status
		case "artifact":
			if i == 0 || metadata.Artifact == nil || metadata.Instruction != nil || metadata.Header != nil {
				return nil, errors.New("workflow immutable artifact metadata is invalid")
			}
			content := string(metadata.InlineBody)
			if ref != "" {
				if encoding == filestore.Binary {
					if err := files.Verify(ctx, ref); err != nil {
						return nil, err
					}
				}
				content, err = loadWorkflowArtifact(ctx, files, ref)
				if err != nil {
					return nil, err
				}
			}
			metadata.Artifact.Content = content
			payload.Artifacts = append(payload.Artifacts, *metadata.Artifact)
		case "instruction":
			if i == 0 || metadata.Instruction == nil || metadata.Artifact != nil || metadata.Header != nil || (ref != "" && encoding != filestore.Text) || metadata.Instruction.RunID != event.RunID {
				return nil, errors.New("workflow immutable instruction metadata is invalid")
			}
			text := metadata.InlineBody
			if ref != "" {
				text, err = files.LoadBounded(ctx, ref, filestore.Text, 16<<10)
				if err != nil {
					return nil, err
				}
			}
			metadata.Instruction.Text = string(text)
			payload.Instructions = append(payload.Instructions, *metadata.Instruction)
		default:
			return nil, errors.New("workflow immutable source kind is unsupported")
		}
	}
	return json.Marshal(payload)
}

// preparePendingWorkflowEvent resolves a legacy frozen body or materializes
// immutable SQL sources. Every GET/PUT happens after all SQL cursors close.
func preparePendingWorkflowEvent(ctx context.Context, db *gorm.DB, event workflowEventRecord, execution ...*executionlineage.Controller) (workflowEventRecord, []byte, error) {
	if err := messageOutsideTransaction(db); err != nil {
		return event, nil, err
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return event, nil, err
	}
	if event.Payload != "" || event.SourceVersion == 0 {
		body, err := files.Load(ctx, event.Payload, filestore.Text)
		return event, body, err
	}
	body, err := materializeWorkflowSources(ctx, db, event)
	if err != nil {
		return event, nil, err
	}
	prepared, err := files.Prepare(ctx, strings.NewReader(string(body)), filestore.Text)
	if err != nil {
		return event, nil, err
	}
	publish := func(tx *gorm.DB) error {
		store, err := filestore.FromDB(tx)
		if err != nil {
			return err
		}
		ref, err := store.PreparedRef(prepared)
		if err != nil {
			return err
		}
		result := tx.Model(&workflowEventRecord{}).Where("id = ? AND disposition = ? AND source_version = ? AND source_count = ? AND source_fingerprint = ? AND (payload = ? OR payload IS NULL)",
			event.ID, "pending", event.SourceVersion, event.SourceCount, event.SourceFingerprint, "").UpdateColumn("payload", ref)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			event.Payload = ref
			return nil
		}
		// A competing materializer can publish the same immutable body first.
		// Revalidate the exact generation/ref without reloading objects in TX.
		var current workflowEventRecord
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&current, "id = ?", event.ID).Error; err != nil {
			return err
		}
		if current.RunID != event.RunID || current.Version != event.Version || current.SoloID != event.SoloID || current.UserID != event.UserID || current.Kind != event.Kind || current.SourceVersion != event.SourceVersion || current.SourceCount != event.SourceCount || current.SourceFingerprint != event.SourceFingerprint || current.Payload != ref {
			return errors.New("workflow event changed while materializing its immutable sources")
		}
		event = current
		return nil
	}
	if len(execution) > 0 && execution[0] != nil {
		err = execution[0].Transaction(ctx, db, workflowExecutionEntities(event), publish)
	} else {
		err = db.WithContext(ctx).Transaction(publish)
	}
	if err != nil {
		return event, nil, fmt.Errorf("publish frozen workflow payload: %w", err)
	}
	return event, body, nil
}
