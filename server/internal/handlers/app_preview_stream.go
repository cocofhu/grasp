package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

type previewMainStreamKey struct {
	runID, nodeID, owner string
	sandboxID            uint
	iteration            int
}

type previewMainStream struct {
	cancel      context.CancelFunc
	subscribers map[chan previewMainUpdate]struct{}
	latest      previewMainUpdate
	finished    bool
}

type previewMainUpdate struct {
	snapshot *previewMainSnapshot
	err      error
}

// Snapshots are immutable. Live updates reuse the encoded historical prefix;
// only a newly connected viewer receives that prefix over the wire.
type previewMainSnapshot struct {
	history *previewMainHistory
	rows    []json.RawMessage
	queue   json.RawMessage
}

func newPreviewMainSnapshot(history *previewMainHistory) (*previewMainSnapshot, error) {
	snapshot := &previewMainSnapshot{history: history, rows: make([]json.RawMessage, len(history.Rows))}
	for i, row := range history.Rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, err
		}
		snapshot.rows[i] = encoded
	}
	var err error
	snapshot.queue, err = json.Marshal(history.Queue)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func (s *previewMainSnapshot) withLive(live *models.SoloMessage, busy bool) (*previewMainSnapshot, error) {
	if live == nil {
		if s.history.Busy == busy {
			return s, nil
		}
		history := *s.history
		history.Busy = busy
		if !busy {
			history.ActiveTurnID = ""
		}
		return &previewMainSnapshot{history: &history, rows: s.rows, queue: s.queue}, nil
	}
	index, insert := -1, false
	for i, row := range s.history.Rows {
		if row.ID == live.ID {
			index, insert = i, false
			break
		}
		if row.ID == strings.TrimPrefix(live.ID, "live:") {
			index, insert = i+1, true
		}
	}
	if index < 0 {
		// The turn-begin control frame will load the newly persisted user row.
		return s, nil
	}
	if insert {
		for _, next := range s.history.Rows[index:] {
			if next.Role == "assistant" && !next.Streaming {
				// The terminal control frame is about to replace the saved reply.
				return s, nil
			}
		}
	}
	row := previewSoloRow(*live)
	encoded, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	if !insert && bytes.Equal(s.rows[index], encoded) && s.history.Busy == busy {
		return s, nil
	}
	history := *s.history
	history.Busy = busy
	if !busy {
		history.ActiveTurnID = ""
	}
	history.Rows = append([]sharedPreviewRow(nil), history.Rows...)
	rows := append([]json.RawMessage(nil), s.rows...)
	if insert {
		history.Rows = append(history.Rows, sharedPreviewRow{})
		copy(history.Rows[index+1:], history.Rows[index:])
		rows = append(rows, nil)
		copy(rows[index+1:], rows[index:])
	}
	history.Rows[index], rows[index] = row, encoded
	return &previewMainSnapshot{history: &history, rows: rows, queue: s.queue}, nil
}

func (s *previewMainSnapshot) frameAfter(previous *previewMainSnapshot) gin.H {
	if previous == nil {
		return gin.H{"type": "snapshot", "history": s.history}
	}
	start := 0
	for start < len(s.rows) && start < len(previous.rows) && bytes.Equal(s.rows[start], previous.rows[start]) {
		start++
	}
	frame := gin.H{"type": "tail", "start": start, "rows": s.rows[start:], "busy": s.history.Busy, "model": s.history.Model,
		"activeTurnId": s.history.ActiveTurnID}
	if !bytes.Equal(s.queue, previous.queue) {
		frame["queue"] = s.history.Queue
	}
	return frame
}

// One source subscription and one coalescing loop serve every viewer of a
// conversation, including different ports/share links. The last viewer owns
// cleanup; no transcript or subscription survives after it disconnects.
func (h *Handlers) subscribePreviewMain(link *models.GateShareLink, sb models.Sandbox) (<-chan previewMainUpdate, func()) {
	key := previewMainStreamKey{link.RunID, link.NodeID, link.CreatedBy, sb.ID, link.Iteration}
	h.previewMainMu.Lock()
	if h.previewMainStreams == nil {
		h.previewMainStreams = make(map[previewMainStreamKey]*previewMainStream)
	}
	stream := h.previewMainStreams[key]
	if stream == nil {
		ctx, cancel := context.WithCancel(context.Background())
		stream = &previewMainStream{cancel: cancel, subscribers: make(map[chan previewMainUpdate]struct{})}
		h.previewMainStreams[key] = stream
		go h.runPreviewMainStream(ctx, key, stream, *link, sb)
	}
	updates := make(chan previewMainUpdate, 1)
	stream.subscribers[updates] = struct{}{}
	if stream.latest.snapshot != nil || stream.latest.err != nil {
		updates <- stream.latest
	}
	h.previewMainMu.Unlock()
	var once sync.Once
	return updates, func() {
		once.Do(func() {
			h.previewMainMu.Lock()
			delete(stream.subscribers, updates)
			if !stream.finished {
				close(updates)
			}
			last := len(stream.subscribers) == 0
			if last && h.previewMainStreams[key] == stream {
				delete(h.previewMainStreams, key)
			}
			h.previewMainMu.Unlock()
			if last {
				stream.cancel()
			}
		})
	}
}

func (h *Handlers) publishPreviewMain(stream *previewMainStream, update previewMainUpdate) {
	h.previewMainMu.Lock()
	defer h.previewMainMu.Unlock()
	stream.latest = update
	for subscriber := range stream.subscribers {
		// A slow viewer needs only the newest snapshot. Its tail is calculated
		// against the last snapshot it actually sent, so skipped updates are safe.
		select {
		case <-subscriber:
		default:
		}
		subscriber <- update
	}
}

func (h *Handlers) runPreviewMainStream(ctx context.Context, key previewMainStreamKey, stream *previewMainStream, link models.GateShareLink, sb models.Sandbox) {
	defer func() {
		h.previewMainMu.Lock()
		stream.finished = true
		for subscriber := range stream.subscribers {
			close(subscriber)
		}
		if h.previewMainStreams[key] == stream {
			delete(h.previewMainStreams, key)
		}
		h.previewMainMu.Unlock()
		stream.cancel()
	}()
	var soloEvents <-chan chatsession.Event
	var workflowEvents <-chan []byte
	var unsubscribe func()
	if sb.Purpose == services.SandboxPurposeSolo {
		var err error
		soloEvents, unsubscribe, err = h.Solo.Subscribe(sb.ThreadID, link.CreatedBy, -1)
		if err != nil {
			h.publishPreviewMain(stream, previewMainUpdate{err: err})
			return
		}
	} else {
		workflowEvents, unsubscribe = h.Eng.Broker().Subscribe(link.RunID)
	}
	defer unsubscribe()
	load := func() (*previewMainSnapshot, error) {
		history, err := h.appPreviewMainHistory(&link, sb)
		if err != nil {
			return nil, err
		}
		return newPreviewMainSnapshot(history)
	}
	current, err := load()
	if err != nil {
		h.publishPreviewMain(stream, previewMainUpdate{err: err})
		return
	}
	h.publishPreviewMain(stream, previewMainUpdate{snapshot: current})
	tick := time.NewTicker(125 * time.Millisecond)
	defer tick.Stop()
	dirty, reload := false, false
	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-soloEvents:
			if !open {
				h.publishPreviewMain(stream, previewMainUpdate{err: errPreviewMainServiceUnavailable})
				return
			}
			dirty = true
			reload = reload || event.Type == chatsession.FrameSession
		case raw, open := <-workflowEvents:
			if !open {
				h.publishPreviewMain(stream, previewMainUpdate{err: errPreviewMainServiceUnavailable})
				return
			}
			if isDialogueFrameFor(raw, link.NodeID, "") {
				dirty, reload = true, true
			}
		case <-tick.C:
			if !dirty {
				continue
			}
			var next *previewMainSnapshot
			if reload {
				next, err = load()
			} else {
				live, busy := h.Solo.PreviewLiveReply(sb.ThreadID)
				next, err = current.withLive(live, busy)
			}
			if err != nil {
				h.publishPreviewMain(stream, previewMainUpdate{err: err})
				return
			}
			if next != current {
				h.publishPreviewMain(stream, previewMainUpdate{snapshot: next})
				current = next
			}
			dirty, reload = false, false
		}
	}
}
