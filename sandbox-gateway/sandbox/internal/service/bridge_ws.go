package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"backend/internal/logging"
	"backend/internal/provider"

	"github.com/gorilla/websocket"
)

func (b *Bridge) Broadcast(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		log.Printf("bridge %s: Broadcast 序列化失败: %v", b.AgentLogPrefix(), err)
		return
	}
	payload = b.stampUsageEvent(payload)

	// 自动将 op:event 的 data 字段记录到事件日志，供刷新后重放
	b.tryRecordBroadcast(payload)

	deadline := time.Now().Add(10 * time.Second)

	b.mu.Lock()
	list := make([]*wsClient, 0, len(b.clients))
	for _, wc := range b.clients {
		list = append(list, wc)
	}
	b.mu.Unlock()

	for _, wc := range list {
		if err := wc.writeMessage(websocket.TextMessage, payload, deadline); err != nil {
			log.Printf("bridge %s: 向客户端广播失败（将移除连接）: %v", b.AgentLogPrefix(), err)
			logging.WarnErr(wc.conn.Close(), "ws close after broadcast failure", map[string]any{
				"agent": b.AgentLogPrefix(),
			})
			b.unregisterClientLocked(wc.conn)
		}
	}
}

// Persist the operation identity and original measurement time in the event
// log as well as the live envelope, so restart replay can account exactly once.
func (b *Bridge) stampUsageEvent(payload []byte) []byte {
	if !bytes.Contains(payload, []byte(`"prompt_done"`)) {
		return payload
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(payload, &envelope) != nil || string(envelope["op"]) != `"event"` {
		return payload
	}
	var event map[string]json.RawMessage
	if json.Unmarshal(envelope["data"], &event) != nil || string(event["type"]) != `"prompt_done"` {
		return payload
	}
	if len(envelope["opId"]) > 0 {
		event["opId"] = envelope["opId"]
	}
	if len(event["reportedAt"]) == 0 {
		event["reportedAt"], _ = json.Marshal(time.Now().UTC())
	}
	b.turnMu.Lock()
	var carried map[string]provider.TokenUsage
	if b.activeTurn != nil && string(envelope["opId"]) == fmt.Sprintf("%q", b.activeTurn.opID) {
		carried = b.activeTurn.carriedUsage
		if b.activeTurn.continued.Load() && (b.activeTurn.carriedPartial || len(carried) == 0 || len(event["usage"]) == 0 || string(event["usage"]) == "null") {
			event["usagePartial"] = json.RawMessage("true")
		}
	}
	if len(carried) > 0 {
		usage := map[string]provider.TokenUsage{}
		_ = json.Unmarshal(event["usage"], &usage)
		if usage == nil {
			usage = map[string]provider.TokenUsage{}
		}
		for model, u := range carried {
			v := usage[model]
			v.InputTokens += u.InputTokens
			v.OutputTokens += u.OutputTokens
			v.CacheReadTokens += u.CacheReadTokens
			v.CacheWriteTokens += u.CacheWriteTokens
			usage[model] = v
		}
		event["usage"], _ = json.Marshal(usage)
	}
	b.turnMu.Unlock()
	envelope["data"], _ = json.Marshal(event)
	stamped, err := json.Marshal(envelope)
	if err != nil {
		return payload
	}
	return stamped
}

// unregisterClientLocked 从 clients 移除 c（持 b.mu）。
func (b *Bridge) unregisterClientLocked(c *websocket.Conn) {
	b.mu.Lock()
	delete(b.clients, c)
	b.mu.Unlock()
}

// RegisterClient 登记客户端；会话已删除时直接关闭连接并返回 false。
func (b *Bridge) RegisterClient(c *websocket.Conn) bool {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		logging.WarnErr(c.Close(), "ws close on deleted chat", nil)
		return false
	}
	b.clients[c] = &wsClient{conn: c}
	b.mu.Unlock()
	return true
}

func (b *Bridge) UnregisterClient(c *websocket.Conn) {
	b.unregisterClientLocked(c)
}

// WriteTextWS 向单个客户端发送文本帧（与 Broadcast 共用该连接的 wsClient 写锁）。
func (b *Bridge) WriteTextWS(c *websocket.Conn, payload []byte) error {
	b.mu.Lock()
	wc := b.clients[c]
	b.mu.Unlock()
	if wc == nil {
		return fmt.Errorf("websocket not registered")
	}
	return wc.writeMessage(websocket.TextMessage, payload, time.Now().Add(10*time.Second))
}

// WriteJSONWS 向单个客户端发送 JSON（与 Broadcast 共用该连接的 wsClient 写锁）。
func (b *Bridge) WriteJSONWS(c *websocket.Conn, v any) error {
	b.mu.Lock()
	wc := b.clients[c]
	b.mu.Unlock()
	if wc == nil {
		return fmt.Errorf("websocket not registered")
	}
	return wc.writeJSON(v, time.Now().Add(10*time.Second))
}
