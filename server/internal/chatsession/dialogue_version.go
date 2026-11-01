package chatsession

import (
	"sync"
	"sync/atomic"
	"time"
)

// DialogueVersion orders dialogue-state snapshots. Epoch (Unix milliseconds,
// so it stays an exact JavaScript number) identifies the
// process (it grows across restarts, even when the wall clock steps back) and
// Seq grows within it, so (Epoch, Seq) compared lexicographically is monotonic
// for the lifetime of the deployment. Session generations restart at zero on
// boot; dialogue versions never do.
type DialogueVersion struct {
	Epoch int64  `json:"epoch"`
	Seq   uint64 `json:"seq"`
}

// Less reports v < o.
func (v DialogueVersion) Less(o DialogueVersion) bool {
	if v.Epoch != o.Epoch {
		return v.Epoch < o.Epoch
	}
	return v.Seq < o.Seq
}

// EpochStore persists the last epoch handed out so a restart can exceed it.
type EpochStore interface {
	LoadEpoch() (int64, bool)
	SaveEpoch(int64) error
}

var (
	epochMu     sync.Mutex
	epochValue  int64
	dialogueSeq atomic.Uint64
	// nowMilli is replaced in tests to simulate a clock stepping back.
	nowMilli = func() int64 { return time.Now().UnixMilli() }
)

// NextEpoch picks this process's epoch: the wall clock, unless the store
// already holds a value at or above it (clock stepped back, or two boots in
// the same millisecond), in which case it is the stored value plus one. The
// choice is saved before it is used.
func NextEpoch(store EpochStore) (int64, error) {
	next := nowMilli()
	if store != nil {
		if last, ok := store.LoadEpoch(); ok && next <= last {
			next = last + 1
		}
		if err := store.SaveEpoch(next); err != nil {
			return 0, err
		}
	}
	return next, nil
}

// InitDialogueEpoch fixes the process epoch from store. Call once at startup,
// before any dialogue state is published. A nil store uses the wall clock.
func InitDialogueEpoch(store EpochStore) error {
	e, err := NextEpoch(store)
	if err != nil {
		return err
	}
	epochMu.Lock()
	epochValue = e
	epochMu.Unlock()
	return nil
}

// DialogueEpoch is the process epoch, lazily taken from the wall clock when
// InitDialogueEpoch was never called (unit tests, tools).
func DialogueEpoch() int64 {
	epochMu.Lock()
	defer epochMu.Unlock()
	if epochValue == 0 {
		epochValue = nowMilli()
	}
	return epochValue
}

// NextDialogueVersion returns a version strictly greater than every version
// returned before it in this process.
func NextDialogueVersion() DialogueVersion {
	return DialogueVersion{Epoch: DialogueEpoch(), Seq: dialogueSeq.Add(1)}
}
