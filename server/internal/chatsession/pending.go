package chatsession

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/cocofhu/grasp/internal/models"
)

// PendingTurn is one stored unfinished turn of a session.
type PendingTurn struct {
	ID      string
	Started bool
	Payload []byte
	// EnqueuedAt is when the turn was first stored; rewrites keep it.
	EnqueuedAt time.Time
}

// PendingStore keeps each session's unfinished turns in the database so a
// restarted server can restore them. A nil store, or one without a database,
// stores nothing.
type PendingStore struct {
	db *gorm.DB
}

// NewPendingStore wraps db.
func NewPendingStore(db *gorm.DB) *PendingStore {
	return &PendingStore{db: db}
}

func (p *PendingStore) enabled() bool { return p != nil && p.db != nil }

// Save replaces the stored turns of one session with turns, in order.
func (p *PendingStore) Save(surface, key string, turns []PendingTurn) error {
	if !p.enabled() {
		return nil
	}
	return p.db.Transaction(func(tx *gorm.DB) error {
		var existing []models.ChatPendingTurn
		if err := tx.Select("id", "created_at").Where("surface = ? AND session_key = ?", surface, key).
			Find(&existing).Error; err != nil {
			return err
		}
		enqueued := make(map[string]time.Time, len(existing))
		for _, row := range existing {
			enqueued[row.ID] = row.CreatedAt
		}
		if err := tx.Where("surface = ? AND session_key = ?", surface, key).
			Delete(&models.ChatPendingTurn{}).Error; err != nil {
			return err
		}
		if len(turns) == 0 {
			return nil
		}
		now := time.Now()
		rows := make([]models.ChatPendingTurn, 0, len(turns))
		for i, t := range turns {
			at, ok := enqueued[t.ID]
			if !ok {
				at = now
			}
			rows = append(rows, models.ChatPendingTurn{ID: t.ID, Surface: surface, SessionKey: key,
				Position: i, Started: t.Started, Payload: t.Payload, CreatedAt: at})
		}
		return tx.Create(&rows).Error
	})
}

// Clear drops the stored turns of one session.
func (p *PendingStore) Clear(surface, key string) error {
	return p.Save(surface, key, nil)
}

// Load returns every stored session of surface, each in FIFO order, and the
// session keys ordered by their oldest turn.
func (p *PendingStore) Load(surface string) (map[string][]PendingTurn, []string, error) {
	if !p.enabled() {
		return nil, nil, nil
	}
	var rows []models.ChatPendingTurn
	if err := p.db.Where("surface = ?", surface).Order("session_key asc, position asc").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	out := map[string][]PendingTurn{}
	first := map[string]time.Time{}
	for _, row := range rows {
		out[row.SessionKey] = append(out[row.SessionKey], PendingTurn{ID: row.ID, Started: row.Started,
			Payload: row.Payload, EnqueuedAt: row.CreatedAt})
		if at, ok := first[row.SessionKey]; !ok || row.CreatedAt.Before(at) {
			first[row.SessionKey] = row.CreatedAt
		}
	}
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if !first[keys[i]].Equal(first[keys[j]]) {
			return first[keys[i]].Before(first[keys[j]])
		}
		return keys[i] < keys[j]
	})
	return out, keys, nil
}

// Persister adapts a store to Config.Persist for one session. Items are
// stored as their JSON encoding; a failed write is logged and the session
// keeps running from memory.
func Persister[T any](p *PendingStore, surface, key string, id func(T) string) func(active T, hasActive bool, pending []T) {
	if !p.enabled() {
		return nil
	}
	return func(active T, hasActive bool, pending []T) {
		turns := make([]PendingTurn, 0, len(pending)+1)
		add := func(item T, started bool) {
			payload, err := json.Marshal(item)
			if err != nil {
				log.Warn().Err(err).Str("surface", surface).Str("session", key).Msg("chat pending turn encode failed")
				return
			}
			turns = append(turns, PendingTurn{ID: id(item), Started: started, Payload: payload})
		}
		if hasActive {
			add(active, true)
		}
		for _, item := range pending {
			add(item, false)
		}
		if err := p.Save(surface, key, turns); err != nil {
			log.Warn().Err(err).Str("surface", surface).Str("session", key).Msg("chat pending turns save failed")
		}
	}
}
