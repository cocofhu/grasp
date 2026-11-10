package historymigration

import (
	"context"
	"fmt"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CheckSoloEventsFormat verifies the surviving imported sources and exact SQL
// journal ranges without reading objects or repairing history. The completion
// receipt describes an earlier set: normal message creation/deletion must not
// require today's aggregate hash to equal the original upgrade hash.
func CheckSoloEventsFormat(ctx context.Context, db *gorm.DB) (Report, error) {
	report := Report{Protocol: Protocol}
	db = db.Session(&gorm.Session{NewDB: true, Context: ctx}).Model(nil)
	after := ""
	for {
		var rows []sourceMessage
		query := db.WithContext(ctx).Table("solo_messages").Select("id", "session_id", "role", "turn_id", "events", "event_log", "created_at").Order("id").Limit(256)
		if after != "" {
			query = query.Where("id > ?", after)
		}
		if err := query.Find(&rows).Error; err != nil {
			return report, err
		}
		if len(rows) == 0 {
			return report, nil
		}
		for _, row := range rows {
			if row.ID == "" {
				return report, ErrInvalidHistory
			}
			pointer, err := parsePointer(row.EventLog)
			if err != nil {
				return report, err
			}
			hasSource := row.Events != nil && *row.Events != "" && *row.Events != "null"
			if pointer == nil {
				if hasSource {
					return report, fmt.Errorf("message %s: %w: source has not been imported", row.ID, ErrInvalidHistory)
				}
				continue
			}
			if row.Role != "assistant" || pointer.Version != chatsession.JournalVersion {
				return report, ErrInvalidHistory
			}
			if pointer.Legacy == nil {
				if row.TurnID == "" || pointer.Count < 1 || pointer.From < 0 || pointer.Through < pointer.From || pointer.Through > maxJournalSequence {
					return report, ErrInvalidHistory
				}
				continue // Live journals retain their own generation-aware reader.
			}
			if err := validateOrigin(row, pointer); err != nil {
				return report, err
			}
			var parents int64
			if err := db.WithContext(ctx).Table("solo_sessions").Where("id = ?", row.SessionID).Count(&parents).Error; err != nil || parents != 1 {
				return report, ErrInvalidHistory
			}
			var queue models.ChatQueue
			if err := db.WithContext(ctx).Select("id", "surface", "session_key").Take(&queue, "id = ?", scope(row.SessionID).ID()).Error; err != nil || queue.Surface != "solo" || queue.SessionKey != row.SessionID {
				return report, ErrInvalidHistory
			}
			_, _, chunks, err := captureImport(ctx, db, row.ID, row.SessionID)
			if err != nil {
				return report, err
			}
			for _, chunk := range chunks {
				encoding, _, err := filestore.Parse(chunk.Payload)
				expectedID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", Protocol, scope(row.SessionID).ID(), row.ID, pointer.Legacy.SHA256, chunk.FirstSequence))).String()
				if err != nil || encoding != filestore.Bytes || chunk.ID != expectedID || !chunk.CreatedAt.Equal(row.CreatedAt) || chunk.Bytes < 1 || chunk.Bytes > chatsession.JournalChunkBytes {
					return report, ErrInvalidHistory
				}
			}
			report.Messages++
			report.Events += int64(pointer.Count)
			if pointer.Count == 0 {
				report.EmptyMessages++
			}
			if pointer.Legacy.PossiblyTruncated {
				report.PossiblyTruncated++
			}
		}
		after = rows[len(rows)-1].ID
	}
}
