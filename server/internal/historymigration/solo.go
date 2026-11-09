// Package historymigration imports stored history under an offline maintenance
// fence. It creates neither execution receipts nor executors. Imported records
// retain unknown turn/generation provenance and the old source bytes.
package historymigration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
)

const (
	Protocol     = "grasp-solo-events-import/v1"
	OriginFormat = "solo-events-array/v1"
	// v3.2 silently retained the first 6000 frames. Hitting that cap cannot
	// distinguish a complete process from a truncated prefix.
	LegacyEventCap = 6000
	// Explicit failure bounds; neither an oversized input nor one oversized
	// frame is successfully imported as a partial history.
	MaxSourceBytes     = 256 << 20
	MaxSourceEvents    = 100_000
	maxJournalSequence = 1<<53 - 1
)

var ErrInvalidHistory = errors.New("legacy conversation history is invalid or inconsistent")

// Report covers the entire verified import set, including imports committed
// before an interrupted invocation. Its digest is stable across resume.
type Report struct {
	Protocol          string `json:"protocol"`
	Messages          int64  `json:"messages"`
	Events            int64  `json:"events"`
	PossiblyTruncated int64  `json:"possiblyTruncated"`
	EmptyMessages     int64  `json:"emptyMessages"`
	SHA256            string `json:"sha256"`
}

type sourceMessage struct {
	ID, SessionID, Role, TurnID string
	Events, EventLog            *string
	CreatedAt                   time.Time
	OrderTime                   string `gorm:"column:history_order_time"`
}

type chunkPlan struct {
	row  chatsession.JournalChunkRecord
	body []byte
}

type importPlan struct {
	pointer models.ChatEventLog
	chunks  []chunkPlan
}

func scope(session string) chatsession.QueueScope {
	return chatsession.QueueScope{Surface: "solo", Key: session}
}

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func decodeEvents(data []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(data) > MaxSourceBytes || len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, ErrInvalidHistory
	}
	var events []json.RawMessage
	if err := json.Unmarshal(data, &events); err != nil || len(events) > MaxSourceEvents {
		return nil, ErrInvalidHistory
	}
	return events, nil
}

func normalize(message sourceMessage, data []byte, first uint64) (importPlan, error) {
	var plan importPlan
	if message.ID == "" || message.SessionID == "" || message.Role != "assistant" || first > maxJournalSequence {
		return plan, ErrInvalidHistory
	}
	events, err := decodeEvents(data)
	if err != nil || uint64(len(events)) > maxJournalSequence-first {
		return plan, ErrInvalidHistory
	}
	origin := &models.ChatLegacyEventOrigin{Format: OriginFormat, MessageID: message.ID,
		SHA256: digest(data), Count: len(events), PossiblyTruncated: len(events) >= LegacyEventCap}
	plan.pointer = models.ChatEventLog{Version: chatsession.JournalVersion, From: int64(first),
		Through: int64(first) + int64(len(events)) - 1, Count: len(events), Legacy: origin}
	queueID := scope(message.SessionID).ID()
	for start := 0; start < len(events); {
		entries := make([]chatsession.JournalEntry, 0, chatsession.JournalBatchEvents)
		size := 0
		for end := start; end < len(events) && len(entries) < chatsession.JournalBatchEvents; end++ {
			// These fields identify stored history only. No execution frame,
			// user-turn association, provider result or new time is synthesized.
			entry := chatsession.JournalEntry{Seq: first + uint64(end), Type: chatsession.FrameAcp,
				At: message.CreatedAt, Data: events[end], Payload: map[string]any{
					"historyMessageId": message.ID, "historyEventsSha256": origin.SHA256,
					"historyIndex": end, "historyTimeSource": "message_created_at"}}
			encoded, err := json.Marshal(entry)
			if err != nil || len(encoded) > chatsession.JournalChunkBytes-1024 {
				return importPlan{}, ErrInvalidHistory
			}
			if len(entries) > 0 && size+len(encoded) > chatsession.JournalChunkBytes-1024 {
				break
			}
			entries, size = append(entries, entry), size+len(encoded)+1
		}
		last := entries[len(entries)-1].Seq
		envelope := struct {
			Version int                        `json:"version"`
			QueueID string                     `json:"queueId"`
			First   uint64                     `json:"first"`
			Last    uint64                     `json:"last"`
			Entries []chatsession.JournalEntry `json:"entries"`
		}{chatsession.JournalVersion, queueID, entries[0].Seq, last, entries}
		body, err := json.Marshal(envelope)
		if err != nil || len(body) > chatsession.JournalChunkBytes {
			return importPlan{}, ErrInvalidHistory
		}
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", Protocol, queueID, message.ID, origin.SHA256, entries[0].Seq))).String()
		row := chatsession.JournalChunkRecord{ID: id, QueueID: queueID, FirstSequence: entries[0].Seq,
			LastSequence: last, Count: len(entries), AcpCount: len(entries), Bytes: len(body), CreatedAt: message.CreatedAt}
		logical := models.ChatEventChunk{ID: row.ID, QueueID: row.QueueID, FirstSequence: row.FirstSequence,
			LastSequence: row.LastSequence, Count: row.Count, AcpCount: row.AcpCount, Bytes: row.Bytes, Payload: body}
		if _, err := chatsession.DecodeJournalChunk(logical); err != nil {
			return importPlan{}, err
		}
		plan.chunks = append(plan.chunks, chunkPlan{row: row, body: body})
		start += len(entries)
	}
	return plan, nil
}

func parsePointer(raw *string) (*models.ChatEventLog, error) {
	if raw == nil || *raw == "" || *raw == "null" {
		return nil, nil
	}
	var pointer models.ChatEventLog
	if err := json.Unmarshal([]byte(*raw), &pointer); err != nil {
		return nil, ErrInvalidHistory
	}
	return &pointer, nil
}
