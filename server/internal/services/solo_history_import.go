package services

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// importedMessageEvents uses the precise historical range; an empty legacy
// turn ID must never aggregate other imported messages in the same session.
func readImportedMessageEvents(ctx context.Context, db *gorm.DB, sessionID, messageID string, expected *models.ChatEventLog) ([]json.RawMessage, error) {
	if expected == nil || expected.Legacy == nil || expected.Count < 0 || expected.Count > historymigration.MaxSourceEvents {
		return nil, historymigration.ErrInvalidHistory
	}
	if err := messageOutsideTransaction(db); err != nil {
		return nil, err
	}
	// hydrateSoloMessage receives the original scoped query. A fresh statement
	// keeps its connection/plugin but must not carry message WHERE clauses into
	// the journal's queue/chunk tables.
	db = db.Session(&gorm.Session{NewDB: true, Context: ctx}).Model(nil)
	var source struct{ Events, EventLog *string }
	if err := db.WithContext(ctx).Table("solo_messages").Select("events", "event_log").
		Where("id = ? AND session_id = ? AND role = ?", messageID, sessionID, "assistant").Take(&source).Error; err != nil {
		return nil, err
	}
	var actual models.ChatEventLog
	if source.Events == nil || source.EventLog == nil || json.Unmarshal([]byte(*source.EventLog), &actual) != nil || !reflect.DeepEqual(&actual, expected) {
		return nil, historymigration.ErrInvalidHistory
	}
	encoding, sha, err := filestore.Parse(*source.Events)
	if err != nil || encoding != filestore.JSON || sha != expected.Legacy.SHA256 {
		return nil, historymigration.ErrInvalidHistory
	}
	events := []json.RawMessage{}
	after := int64(-1)
	retained := 0
	for {
		page, _, err := historymigration.ReadSoloMessagePage(ctx, db, messageID, sessionID, after)
		if err != nil {
			return nil, err
		}
		if page.Head != expected.Through {
			return nil, chatsession.ErrJournalGap
		}
		for _, entry := range page.Items {
			retained += len(entry.Data)
			if len(events) >= expected.Count || retained > historymigration.MaxSourceBytes ||
				entry.Seq != uint64(expected.From)+uint64(len(events)) || entry.Payload["historyEventsSha256"] != expected.Legacy.SHA256 {
				return nil, historymigration.ErrInvalidHistory
			}
			events = append(events, entry.Data)
		}
		if !page.HasMore {
			if len(events) != expected.Count {
				return nil, chatsession.ErrJournalGap
			}
			return events, nil
		}
		if page.Next <= after {
			return nil, chatsession.ErrJournalGap
		}
		after = page.Next
	}
}
