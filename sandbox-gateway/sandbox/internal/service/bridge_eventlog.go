package service

import (
	"encoding/json"
	"sync"
)

// eventLog 线程安全的事件日志追加缓冲。
// 记录所有广播给前端的 op:event 载荷，刷新页面后一次性下发供前端重放；不做条数截断，保留完整会话上下文。
type eventLog struct {
	mu      sync.Mutex
	entries []json.RawMessage
}

// append 追加一条事件（已序列化的 JSON）。
func (l *eventLog) append(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	l.mu.Lock()
	l.entries = append(l.entries, raw)
	l.mu.Unlock()
}

// snapshot 返回当前所有事件的副本（按时间顺序）。
func (l *eventLog) snapshot() []json.RawMessage {
	l.mu.Lock()
	cp := make([]json.RawMessage, len(l.entries))
	copy(cp, l.entries)
	l.mu.Unlock()
	return cp
}

// clear 清空日志（Agent 重启 / 新会话时调用）。
func (l *eventLog) clear() {
	l.mu.Lock()
	l.entries = nil
	l.mu.Unlock()
}

// EventLogRecentTurns 返回最近 n 轮对话的事件，以及总轮次数。
// 一轮以 prompt_begin 为界。返回 (events, totalTurns, hasMore)。
func (b *Bridge) EventLogRecentTurns(n int) ([]json.RawMessage, int, bool) {
	all := b.evLog.snapshot()
	if n <= 0 {
		n = 10
	}
	turns := splitEventsByTurn(all)
	total := len(turns)
	if total <= n {
		return all, total, false
	}
	start := total - n
	var result []json.RawMessage
	for _, t := range turns[start:] {
		result = append(result, t...)
	}
	return result, total, true
}

// EventLogTurnsBefore 返回第 beforeTurn 轮之前的 n 轮事件。
// beforeTurn 从 0 开始计数（即第 0 轮是最早的轮次）。
func (b *Bridge) EventLogTurnsBefore(beforeTurn, n int) ([]json.RawMessage, bool) {
	all := b.evLog.snapshot()
	if n <= 0 {
		n = 10
	}
	turns := splitEventsByTurn(all)
	if beforeTurn <= 0 || beforeTurn > len(turns) {
		return nil, false
	}
	end := beforeTurn
	start := end - n
	if start < 0 {
		start = 0
	}
	var result []json.RawMessage
	for _, t := range turns[start:end] {
		result = append(result, t...)
	}
	hasMore := start > 0
	return result, hasMore
}

// splitEventsByTurn 按 prompt_begin 切分事件为轮次。
func splitEventsByTurn(events []json.RawMessage) [][]json.RawMessage {
	if len(events) == 0 {
		return nil
	}
	var turns [][]json.RawMessage
	var current []json.RawMessage
	for _, ev := range events {
		if isPromptBegin(ev) {
			if len(current) > 0 {
				turns = append(turns, current)
			}
			current = []json.RawMessage{ev}
		} else {
			current = append(current, ev)
		}
	}
	if len(current) > 0 {
		turns = append(turns, current)
	}
	return turns
}

func isPromptBegin(raw json.RawMessage) bool {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return false
	}
	return probe.Type == "prompt_begin"
}

// tryRecordBroadcast 从已序列化的广播 payload 中提取 op:event 的 data 字段，写入事件日志。
// 非 op:event 的消息（如 queue_state、error）不记录。
func (b *Bridge) tryRecordBroadcast(payload []byte) {
	// 快速路径：payload 不含 "event" 则跳过
	if len(payload) == 0 {
		return
	}
	var probe struct {
		Op   string          `json:"op"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return
	}
	if probe.Op != "event" || len(probe.Data) == 0 {
		return
	}
	// data 即前端 handleEvent 收到的原始 JSON
	b.evLog.append(probe.Data)
	b.notifyEventSubscribers(probe.Data)
}

// SubscribeEvents 订阅会话事件广播。callback 不应阻塞；返回值用于取消订阅。
func (b *Bridge) SubscribeEvents(callback func(json.RawMessage)) func() {
	if callback == nil {
		return func() {}
	}
	b.eventSubMu.Lock()
	b.eventSubNextID++
	id := b.eventSubNextID
	b.eventSubs[id] = callback
	b.eventSubMu.Unlock()
	return func() {
		b.eventSubMu.Lock()
		delete(b.eventSubs, id)
		b.eventSubMu.Unlock()
	}
}

func (b *Bridge) notifyEventSubscribers(data json.RawMessage) {
	if len(data) == 0 {
		return
	}
	b.eventSubMu.Lock()
	subs := make([]func(json.RawMessage), 0, len(b.eventSubs))
	for _, cb := range b.eventSubs {
		subs = append(subs, cb)
	}
	b.eventSubMu.Unlock()
	for _, cb := range subs {
		cb(data)
	}
}
