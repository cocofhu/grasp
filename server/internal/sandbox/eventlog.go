package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gorilla/websocket"
)

// FetchEventLog reads the full agent event history straight from a live
// sandbox's cursor-acp bridge — the bridge records every op:event payload it
// ever broadcast and serves them via the {op:connect} handshake (eventLog +
// totalTurns + hasMoreTurns) and GET /api/events?before=&limit= for older
// turns. We connect as a passive observer (autoPermission=true), aggregate the
// raw frames into a ChatResult and return it, so the platform never has to
// re-persist the log: the sandbox is the single source of truth while it lives.
//
// Best-effort: callers treat a nil/empty result as "no live log available" and
// fall back to the persisted final snapshot.
//
// NOTE: Full-session aggregation concatenates every turn's message/thought.
// Streaming bubble seeds and timeline ingest must use FetchEventLogLastTurn /
// AggregateLastTurnFrames instead — otherwise a hard refresh stitches the
// previous turn into the live bubble.
func FetchEventLog(ctx context.Context, host string, port int) (*ChatResult, string, error) {
	all, sessionID, err := FetchEventLogRaw(ctx, host, port)
	if err != nil {
		return nil, "", err
	}
	result := &ChatResult{}
	for _, frame := range all {
		dispatchFrame(frame, result)
	}
	return result, sessionID, nil
}

// FetchEventLogLastTurn is like FetchEventLog but only folds frames after the
// last prompt_begin. Used for nodeEvents / timeline streaming seeds so the
// live bubble never receives cross-turn narration. The sandbox still keeps the
// full eventLog for console replay via FetchEventLog / FetchEventLogRaw.
func FetchEventLogLastTurn(ctx context.Context, host string, port int) (*ChatResult, string, error) {
	all, sessionID, err := FetchEventLogRaw(ctx, host, port)
	if err != nil {
		return nil, "", err
	}
	result := &ChatResult{}
	for _, frame := range FramesAfterLastPromptBegin(all) {
		dispatchFrame(frame, result)
	}
	return result, sessionID, nil
}

// FetchEventLogRaw is like FetchEventLog but returns the raw event frames
// (full {op:"event",...} / bare {type,update} JSON) instead of an aggregated
// ChatResult. Callers that need per-turn structure — e.g. rebuilding a Q→A→Q→A
// transcript with the original user prompts (prompt_begin frames carry
// promptText + imageURLs, which the aggregate drops) — use this.
func FetchEventLogRaw(ctx context.Context, host string, port int) ([]json.RawMessage, string, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("ws://%s:%d/ws", host, port)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{"op": "connect", "autoPermission": true}); err != nil {
		return nil, "", fmt.Errorf("ws connect: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))

	var (
		sessionID  string
		initial    []json.RawMessage
		totalTurns int
		hasMore    bool
	)
	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			return nil, "", fmt.Errorf("ws read connected: %w", readErr)
		}
		var probe struct {
			Op           string            `json:"op"`
			SessionID    string            `json:"sessionId"`
			EventLog     []json.RawMessage `json:"eventLog"`
			TotalTurns   int               `json:"totalTurns"`
			HasMoreTurns bool              `json:"hasMoreTurns"`
		}
		if json.Unmarshal(raw, &probe) != nil || probe.Op != "connected" {
			continue
		}
		sessionID = probe.SessionID
		initial = probe.EventLog
		totalTurns = probe.TotalTurns
		hasMore = probe.HasMoreTurns
		break
	}

	// initial covers the most recent turns; walk backwards for older history.
	all := initial
	cursor := totalTurns - 10
	for hasMore && cursor > 0 {
		batch, more, fetchErr := fetchEventsBefore(ctx, host, port, cursor, 50)
		if fetchErr != nil {
			break // partial history is still useful
		}
		all = append(batch, all...)
		hasMore = more
		cursor -= 50
	}
	return all, sessionID, nil
}

// fetchEventsBefore pages older history via GET /api/events?before=&limit=.
func fetchEventsBefore(ctx context.Context, host string, port, before, limit int) ([]json.RawMessage, bool, error) {
	url := fmt.Sprintf("http://%s:%d/api/events?before=%d&limit=%d", host, port, before, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("acp events GET: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, false, fmt.Errorf("acp events %d: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Events  []json.RawMessage `json:"events"`
		HasMore bool              `json:"hasMore"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, false, fmt.Errorf("acp events decode: %w", err)
	}
	return payload.Events, payload.HasMore, nil
}

// EventLogPageResult is one page of raw event frames with cursor metadata.
type EventLogPageResult struct {
	Events     []json.RawMessage
	NextCursor string
	HasMore    bool
}

// FetchEventLogPage returns a page of raw event frames from a live sandbox.
// Without cursor it returns the most recent limit turns; with cursor (turn index
// as string) it fetches older history via GET /api/events?before=&limit=.
func FetchEventLogPage(ctx context.Context, host string, port int, cursor string, limit int) (*EventLogPageResult, error) {
	if host == "" {
		host = "127.0.0.1"
	}
	if limit <= 0 {
		limit = 20
	}

	if cursor != "" {
		before, err := strconv.Atoi(cursor)
		if err != nil || before <= 0 {
			return &EventLogPageResult{}, nil
		}
		events, hasMore, ferr := fetchEventsBefore(ctx, host, port, before, limit)
		if ferr != nil {
			return nil, ferr
		}
		next := ""
		if hasMore && len(events) > 0 {
			next = strconv.Itoa(before - len(events))
			if n, err := strconv.Atoi(next); err != nil || n <= 0 {
				next = strconv.Itoa(before - limit)
			}
		}
		return &EventLogPageResult{Events: events, NextCursor: next, HasMore: hasMore}, nil
	}

	url := fmt.Sprintf("ws://%s:%d/ws", host, port)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{"op": "connect", "autoPermission": true}); err != nil {
		return nil, fmt.Errorf("ws connect: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))

	var (
		initial    []json.RawMessage
		totalTurns int
		hasMore    bool
	)
	for {
		_, raw, readErr := conn.ReadMessage()
		if readErr != nil {
			return nil, fmt.Errorf("ws read connected: %w", readErr)
		}
		var probe struct {
			Op           string            `json:"op"`
			EventLog     []json.RawMessage `json:"eventLog"`
			TotalTurns   int               `json:"totalTurns"`
			HasMoreTurns bool              `json:"hasMoreTurns"`
		}
		if json.Unmarshal(raw, &probe) != nil || probe.Op != "connected" {
			continue
		}
		initial = probe.EventLog
		totalTurns = probe.TotalTurns
		hasMore = probe.HasMoreTurns
		break
	}

	events := initial
	nextCursor := ""
	if totalTurns > len(initial) {
		hasMore = true
		nextCursor = strconv.Itoa(totalTurns - len(initial))
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	if hasMore && nextCursor == "" && totalTurns > limit {
		nextCursor = strconv.Itoa(totalTurns - limit)
	}
	return &EventLogPageResult{Events: events, NextCursor: nextCursor, HasMore: hasMore}, nil
}

// AggregateFrames folds raw event frames into AcpEvents.
func AggregateFrames(frames []json.RawMessage) []models.AcpEvent {
	result := &ChatResult{}
	for _, frame := range frames {
		dispatchFrame(frame, result)
	}
	return result.AcpEvents()
}

// AggregateLastTurnFrames folds only the last prompt_begin turn into AcpEvents.
// Streaming seeds must use this (or FetchEventLogLastTurn) so message/thought
// rails stay current-turn-only.
func AggregateLastTurnFrames(frames []json.RawMessage) []models.AcpEvent {
	return AggregateFrames(FramesAfterLastPromptBegin(frames))
}

// FramesAfterLastPromptBegin returns frames from the last prompt_begin onward
// (inclusive). When no prompt_begin is present, frames are returned unchanged
// (single-turn / legacy logs).
func FramesAfterLastPromptBegin(frames []json.RawMessage) []json.RawMessage {
	last := -1
	for i, raw := range frames {
		if frameIsPromptBegin(raw) {
			last = i
		}
	}
	if last < 0 {
		return frames
	}
	return frames[last:]
}

func frameIsPromptBegin(raw json.RawMessage) bool {
	data := raw
	var env struct {
		Op   string          `json:"op"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Op == "event" && len(env.Data) > 0 {
		data = env.Data
	}
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	return probe.Type == "prompt_begin"
}

// dispatchFrame folds one persisted event frame into the aggregate. Frames may
// arrive either as a full {op:"event", data:{...}} envelope or as the bare
// event payload {type, update}; both are handled.
func dispatchFrame(raw json.RawMessage, result *ChatResult) {
	data := raw
	var env struct {
		Op   string          `json:"op"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Op == "event" && len(env.Data) > 0 {
		data = env.Data
	}
	var ev struct {
		Type   string          `json:"type"`
		Update json.RawMessage `json:"update"`
		Usage  json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(data, &ev) != nil {
		return
	}
	if ev.Type == "turn_segment" {
		if result != nil {
			result.sealSegment()
		}
		return
	}
	if ev.Type == "prompt_done" {
		if result != nil {
			// Event-log replay has no session bridge context; weak keys → unknown.
			if u, byModel := parsePromptDoneUsage(ev.Usage, ""); u != nil {
				result.Usage = models.AddTokenUsage(result.Usage, u)
				result.UsageByModel = models.AddTokenUsageByModel(result.UsageByModel, byModel)
			}
		}
		return
	}
	if ev.Type == "session_update" && len(ev.Update) > 0 {
		kind, flat := normalizeSessionUpdate(ev.Update)
		dispatchSessionUpdate(kind, flat, result)
	}
}
