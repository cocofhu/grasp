package models

import "time"

// ChatPendingTurn is one accepted chat turn that has not finished yet: a FIFO
// waiter or the active turn of a Solo or workflow dialogue session. Rows are
// rewritten whenever the FIFO changes so a restarted server can put waiters
// back in order instead of dropping them.
type ChatPendingTurn struct {
	ID         string `gorm:"primaryKey;size:191"`
	Surface    string `gorm:"size:64;index:idx_chat_pending_session,priority:1"`
	SessionKey string `gorm:"size:191;index:idx_chat_pending_session,priority:2"`
	Position   int
	// Started marks the active turn. Its human message may already be in the
	// transcript, so a restart settles it instead of running it again.
	Started   bool
	Payload   []byte
	CreatedAt time.Time
}
