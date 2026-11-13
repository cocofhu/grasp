package browser

import (
	"sync"
	"time"
)

// Overlay.setInspectMode(searchForNode) often emits inspectModeCanceled for the
// previous mode — sometimes more than once. If any of those reach the UI, the
// toggle looks off while Overlay is still on — the next click re-enters inspect
// and cancel never sticks. Swallow every cancel for the skip window, not just
// the first one.
const inspectCancelSkip = 300 * time.Millisecond

// inspectCancelFilter decides whether Overlay.inspectModeCanceled should notify
// the UI. It swallows the side-effect cancel from enabling inspect.
type inspectCancelFilter struct {
	mu     sync.Mutex
	wanted bool
	skip   bool
	gen    int
	timer  *time.Timer
}

func (f *inspectCancelFilter) set(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wanted = on
	f.gen++
	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}
	if !on {
		f.skip = false
		return
	}
	f.skip = true
	gen := f.gen
	f.timer = time.AfterFunc(inspectCancelSkip, func() {
		f.expireSkip(gen)
	})
}

func (f *inspectCancelFilter) expireSkip(gen int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gen == gen {
		f.skip = false
	}
}

func (f *inspectCancelFilter) onCanceled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.skip {
		return false
	}
	if !f.wanted {
		return false
	}
	f.wanted = false
	return true
}

func (f *inspectCancelFilter) stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.timer != nil {
		f.timer.Stop()
		f.timer = nil
	}
}
