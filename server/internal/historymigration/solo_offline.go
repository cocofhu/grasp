package historymigration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func checkOffline(ctx context.Context, db *gorm.DB, owner string) error {
	if err := storageguard.Check(ctx, db, owner); err != nil {
		return err
	}
	var count int64
	if err := db.WithContext(ctx).Model(&storageguard.Runtime{}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("legacy history import requires every serving runtime to be stopped")
	}
	return nil
}

func fenceOfflineTx(tx *gorm.DB, owner string) error {
	fenced := tx.Model(&storageguard.Control{}).Where("name = ? AND owner = ?", "primary", owner).
		UpdateColumn("revision", gorm.Expr("revision + 1"))
	if fenced.Error != nil {
		return fenced.Error
	}
	if owner == "" || fenced.RowsAffected != 1 {
		return errors.New("legacy history import lost its offline maintenance fence")
	}
	return checkOffline(tx.Statement.Context, tx, owner)
}

type preparedChunk struct {
	row  chatsession.JournalChunkRecord
	body filestore.Prepared
}

// ImportSoloEvents converts and verifies stored Events arrays. It must be
// called after schema/filestore normalization and before final reference
// verification. Maintenance is retained on both success and failure. No SQL
// cursor/transaction remains open during a content load or publication.
func ImportSoloEvents(ctx context.Context, db *gorm.DB, owner string) (Report, error) {
	return scanSoloEvents(ctx, db, owner, true)
}

// VerifySoloEvents checks an existing complete import without publishing
// objects, creating indexes, or filling missing message pointers. It is safe
// to use as an upgrade-receipt precondition for a later explicit activation.
func VerifySoloEvents(ctx context.Context, db *gorm.DB, owner string) (Report, error) {
	return scanSoloEvents(ctx, db, owner, false)
}

func scanSoloEvents(ctx context.Context, db *gorm.DB, owner string, convert bool) (Report, error) {
	report := Report{Protocol: Protocol}
	if err := outsideTransaction(db); err != nil {
		return report, err
	}
	if err := checkOffline(ctx, db, owner); err != nil {
		return report, err
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return report, err
	}
	hash := sha256.New()
	var cursor *sourceMessage
	for {
		var rows []sourceMessage
		// Keep SQLite's original timestamp representation for keyset binds.
		// Reformatting time.Time can compare differently to an old text cell.
		query := db.WithContext(ctx).Table("solo_messages").Select("id, session_id, role, event_log, created_at, CAST(created_at AS CHAR) AS history_order_time").
			Where("role = ? AND events IS NOT NULL AND events NOT IN ?", "assistant", []string{"", "null"})
		if cursor != nil {
			query = query.Where("session_id > ? OR (session_id = ? AND (created_at > ? OR (created_at = ? AND id > ?)))",
				cursor.SessionID, cursor.SessionID, cursor.OrderTime, cursor.OrderTime, cursor.ID)
		}
		if err := query.Order("session_id, created_at, id").Limit(64).Find(&rows).Error; err != nil {
			return report, err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if row.OrderTime == "" {
				return report, fmt.Errorf("message %s: %w: missing original message timestamp", row.ID, ErrInvalidHistory)
			}
			pointer, err := parsePointer(row.EventLog)
			if err != nil {
				return report, fmt.Errorf("message %s: %w", row.ID, err)
			}
			if pointer != nil && pointer.Legacy == nil {
				continue // Existing native journal history is outside this migration.
			}
			if !convert && pointer == nil {
				return report, fmt.Errorf("message %s: %w: history has not been imported", row.ID, ErrInvalidHistory)
			}
			plan, err := importMessage(ctx, db, files, owner, row.ID, row.SessionID)
			if err != nil {
				return report, fmt.Errorf("message %s: %w", row.ID, err)
			}
			record := struct {
				SessionID string              `json:"sessionId"`
				Pointer   models.ChatEventLog `json:"pointer"`
			}{row.SessionID, plan.pointer}
			encoded, err := json.Marshal(record)
			if err != nil {
				return report, err
			}
			_, _ = hash.Write(encoded)
			_, _ = hash.Write([]byte{'\n'})
			report.Messages++
			report.Events += int64(plan.pointer.Count)
			if plan.pointer.Count == 0 {
				report.EmptyMessages++
			}
			if plan.pointer.Legacy.PossiblyTruncated {
				report.PossiblyTruncated++
			}
		}
		last := rows[len(rows)-1]
		cursor = &last
	}
	if err := checkOffline(ctx, db, owner); err != nil {
		return report, err
	}
	report.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return report, nil
}

func importMessage(ctx context.Context, db *gorm.DB, files *filestore.Store, owner, id, session string) (importPlan, error) {
	var zero importPlan
	if err := checkOffline(ctx, db, owner); err != nil {
		return zero, err
	}
	source, err := readSource(ctx, db, id, session)
	if err != nil || source.Events == nil {
		return zero, errors.Join(ErrInvalidHistory, err)
	}
	var parents int64
	if err := db.WithContext(ctx).Table("solo_sessions").Where("id = ?", session).Count(&parents).Error; err != nil || parents != 1 {
		return zero, errors.Join(ErrInvalidHistory, err)
	}
	data, err := loadOfflineSource(ctx, files, *source.Events)
	if err != nil {
		return zero, err
	}
	pointer, err := parsePointer(source.EventLog)
	if err != nil {
		return zero, err
	}
	var before models.ChatQueue
	err = db.WithContext(ctx).First(&before, "id = ?", scope(session).ID()).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, err
	}
	if err == nil && (before.Surface != "solo" || before.SessionKey != session) {
		return zero, ErrInvalidHistory
	}
	first := before.NextEventSequence
	if pointer != nil {
		if err := validateOrigin(source, pointer); err != nil {
			return zero, err
		}
		first = uint64(pointer.From)
	}
	plan, err := normalize(source, data, first)
	if err != nil {
		return zero, err
	}
	if pointer != nil {
		return plan, verifyPlan(ctx, db, files, source, plan)
	}
	// Preserve the exact original array bytes as a typed backup root. This
	// proof and every chunk proof are obtained before the outer SQL TX.
	preparedSource, err := files.PrepareBounded(ctx, bytes.NewReader(data), filestore.JSON, MaxSourceBytes)
	if err != nil {
		return zero, err
	}
	prepared := make([]preparedChunk, 0, len(plan.chunks))
	for _, chunk := range plan.chunks {
		body, err := files.PrepareBounded(ctx, bytes.NewReader(chunk.body), filestore.Bytes, chatsession.JournalChunkBytes)
		if err != nil {
			return zero, err
		}
		prepared = append(prepared, preparedChunk{row: chunk.row, body: body})
	}
	encoded, err := json.Marshal(plan.pointer)
	if err != nil {
		return zero, err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := fenceOfflineTx(tx, owner); err != nil {
			return err
		}
		queue := models.ChatQueue{ID: scope(session).ID(), Surface: "solo", SessionKey: session}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&queue).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&queue, "id = ?", queue.ID).Error; err != nil {
			return err
		}
		if queue.Surface != "solo" || queue.SessionKey != session || queue.NextEventSequence != first ||
			queue.Generation != before.Generation || queue.Revision != before.Revision {
			return chatsession.ErrJournalPrepareConflict
		}
		var current sourceMessage
		if err := tx.Table("solo_messages").Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "session_id", "role", "turn_id", "events", "event_log", "created_at").Where("id = ?", id).Take(&current).Error; err != nil {
			return err
		}
		if current.ID != source.ID || current.SessionID != source.SessionID || current.Role != source.Role ||
			current.TurnID != source.TurnID || !current.CreatedAt.Equal(source.CreatedAt) ||
			!reflect.DeepEqual(current.Events, source.Events) || !reflect.DeepEqual(current.EventLog, source.EventLog) {
			return ErrInvalidHistory
		}
		bound, err := filestore.FromDB(tx)
		if err != nil {
			return err
		}
		sourceRef, err := bound.PreparedRef(preparedSource)
		if err != nil {
			return err
		}
		for _, chunk := range prepared {
			row := chunk.row
			row.Payload, err = bound.PreparedRef(chunk.body)
			if err != nil {
				return err
			}
			if err := tx.Table("chat_event_chunks").Create(&row).Error; err != nil {
				return err
			}
		}
		if err := tx.Table("solo_messages").Where("id = ?", id).Updates(map[string]any{"events": sourceRef, "event_log": string(encoded)}).Error; err != nil {
			return err
		}
		return tx.Model(&models.ChatQueue{}).Where("id = ?", queue.ID).
			Updates(map[string]any{"next_event_sequence": plan.pointer.Through + 1, "revision": gorm.Expr("revision + 1")}).Error
	})
	if err != nil {
		return zero, err
	}
	return plan, verifyPlan(ctx, db, files, source, plan)
}

// loadOfflineSource is the only inline legacy body reader. importMessage calls
// it only after validating the held maintenance owner and stopped runtimes.
// Runtime Load/serializer paths deliberately cannot decode these old cells.
func loadOfflineSource(ctx context.Context, files *filestore.Store, raw string) ([]byte, error) {
	if filestore.IsRef(raw) {
		return files.LoadBounded(ctx, raw, filestore.JSON, MaxSourceBytes)
	}
	if strings.HasPrefix(raw, filestore.Prefix) || int64(len(raw)) > MaxSourceBytes {
		return nil, ErrInvalidHistory
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte(raw), nil
}

func verifyPlan(ctx context.Context, db *gorm.DB, files *filestore.Store, source sourceMessage, plan importPlan) error {
	_, pointer, chunks, err := captureImport(ctx, db, source.ID, source.SessionID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(pointer, plan.pointer) || len(chunks) != len(plan.chunks) {
		return ErrInvalidHistory
	}
	for i, chunk := range chunks {
		if chunk.ID != plan.chunks[i].row.ID {
			return ErrInvalidHistory
		}
		body, err := files.LoadBounded(ctx, chunk.Payload, filestore.Bytes, chatsession.JournalChunkBytes)
		if err != nil {
			return err
		}
		if !bytes.Equal(body, plan.chunks[i].body) {
			return ErrInvalidHistory
		}
		logical := models.ChatEventChunk{ID: chunk.ID, QueueID: chunk.QueueID, FirstSequence: chunk.FirstSequence,
			LastSequence: chunk.LastSequence, TurnID: chunk.TurnID, ExecutionEpoch: chunk.ExecutionEpoch,
			Count: chunk.Count, AcpCount: chunk.AcpCount, Bytes: chunk.Bytes, Payload: body}
		if _, err := chatsession.DecodeJournalChunk(logical); err != nil {
			return err
		}
	}
	return nil
}
