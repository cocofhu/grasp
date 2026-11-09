package historymigration

import (
	"context"
	"encoding/hex"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func outsideTransaction(db *gorm.DB) error {
	if db == nil || db.Statement == nil {
		return ErrInvalidHistory
	}
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return chatsession.ErrPayloadInTransaction
	}
	return nil
}

func readSource(ctx context.Context, db *gorm.DB, id, session string) (sourceMessage, error) {
	var source sourceMessage
	err := db.WithContext(ctx).Table("solo_messages").Select("id", "session_id", "role", "turn_id", "events", "event_log", "created_at").
		Where("id = ? AND session_id = ? AND role = ?", id, session, "assistant").Take(&source).Error
	return source, err
}

func validateOrigin(source sourceMessage, pointer *models.ChatEventLog) error {
	if pointer == nil || pointer.Legacy == nil || source.Events == nil || pointer.Version != chatsession.JournalVersion ||
		pointer.Generation != 0 || pointer.From < 0 || pointer.From > maxJournalSequence || pointer.Count < 0 ||
		pointer.Count > MaxSourceEvents || int64(pointer.Count) > maxJournalSequence-pointer.From ||
		pointer.Through != pointer.From+int64(pointer.Count)-1 {
		return ErrInvalidHistory
	}
	origin := pointer.Legacy
	decoded, err := hex.DecodeString(origin.SHA256)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != origin.SHA256 || origin.Format != OriginFormat ||
		origin.MessageID != source.ID || origin.Count != pointer.Count || origin.PossiblyTruncated != (origin.Count >= LegacyEventCap) {
		return ErrInvalidHistory
	}
	encoding, sha, err := filestore.Parse(*source.Events)
	if err != nil || encoding != filestore.JSON || sha != origin.SHA256 {
		return ErrInvalidHistory
	}
	return nil
}

// captureImport does SQL only and closes every cursor before body reads. The
// retained Events reference is a typed source-hash root, not a legacy body read.
func captureImport(ctx context.Context, db *gorm.DB, id, session string) (sourceMessage, models.ChatEventLog, []chatsession.JournalChunkRecord, error) {
	source, err := readSource(ctx, db, id, session)
	if err != nil {
		return source, models.ChatEventLog{}, nil, err
	}
	pointer, err := parsePointer(source.EventLog)
	if err != nil || validateOrigin(source, pointer) != nil {
		return source, models.ChatEventLog{}, nil, ErrInvalidHistory
	}
	queueID := scope(session).ID()
	var queue struct{ NextEventSequence uint64 }
	if err := db.WithContext(ctx).Table("chat_queues").Select("next_event_sequence").Where("id = ?", queueID).Take(&queue).Error; err != nil {
		return source, *pointer, nil, err
	}
	if queue.NextEventSequence > maxJournalSequence || queue.NextEventSequence < uint64(pointer.Through+1) {
		return source, *pointer, nil, ErrInvalidHistory
	}
	var chunks []chatsession.JournalChunkRecord
	if pointer.Count > 0 {
		if err := db.WithContext(ctx).Table("chat_event_chunks").
			Where("queue_id = ? AND last_sequence >= ? AND first_sequence <= ?", queueID, pointer.From, pointer.Through).
			Order("first_sequence").Limit(MaxSourceEvents + 1).Find(&chunks).Error; err != nil {
			return source, *pointer, nil, err
		}
	}
	next := uint64(pointer.From)
	for _, chunk := range chunks {
		if chunk.TurnID != "" || chunk.ExecutionEpoch != 0 || chunk.ControlEvent != "" || chunk.Count < 1 ||
			chunk.Count > chatsession.JournalBatchEvents || chunk.Count != chunk.AcpCount || chunk.FirstSequence != next ||
			chunk.LastSequence < chunk.FirstSequence || chunk.LastSequence > uint64(pointer.Through) ||
			chunk.LastSequence-chunk.FirstSequence+1 != uint64(chunk.Count) {
			return source, *pointer, nil, ErrInvalidHistory
		}
		next = chunk.LastSequence + 1
	}
	if next != uint64(pointer.Through+1) {
		return source, *pointer, nil, ErrInvalidHistory
	}
	return source, *pointer, chunks, nil
}

func validateImportedEntry(source sourceMessage, pointer models.ChatEventLog, entry chatsession.JournalEntry) error {
	index, ok := entry.Payload["historyIndex"].(float64)
	if entry.Type != chatsession.FrameAcp || entry.TurnID != "" || entry.Generation != 0 ||
		entry.Seq < uint64(pointer.From) || entry.Seq > uint64(pointer.Through) ||
		!entry.At.Equal(source.CreatedAt) || !ok || index != float64(entry.Seq-uint64(pointer.From)) ||
		entry.Payload["historyMessageId"] != source.ID || entry.Payload["historyEventsSha256"] != pointer.Legacy.SHA256 ||
		entry.Payload["historyTimeSource"] != "message_created_at" {
		return ErrInvalidHistory
	}
	return nil
}

// ReadSoloMessagePage reads a specifically authorized message's imported
// journal. The caller must perform its current scope/owner read authorization.
// It does not load the original Events body, mint a lease or wake a provider.
func ReadSoloMessagePage(ctx context.Context, db *gorm.DB, id, session string, after int64) (chatsession.JournalPage, bool, error) {
	result := chatsession.JournalPage{Items: []chatsession.JournalEntry{}, Next: after, Head: -1}
	if err := outsideTransaction(db); err != nil {
		return result, false, err
	}
	source, pointer, _, err := captureImport(ctx, db, id, session)
	if err != nil {
		return result, false, err
	}
	if after == -1 {
		after = pointer.From - 1
	}
	if after < pointer.From-1 || after > pointer.Through {
		return result, pointer.Legacy.PossiblyTruncated, chatsession.ErrJournalCursor
	}
	if pointer.Count == 0 {
		result.Next, result.Head = after, pointer.Through
		return result, pointer.Legacy.PossiblyTruncated, nil
	}
	result, err = chatsession.NewDurableStore(db).JournalPage(ctx, scope(session), chatsession.JournalQuery{After: after, Through: &pointer.Through})
	if err != nil {
		return result, pointer.Legacy.PossiblyTruncated, err
	}
	for _, entry := range result.Items {
		if err := validateImportedEntry(source, pointer, entry); err != nil {
			return chatsession.JournalPage{Items: []chatsession.JournalEntry{}, Next: after, Head: -1}, pointer.Legacy.PossiblyTruncated, err
		}
	}
	return result, pointer.Legacy.PossiblyTruncated, nil
}
