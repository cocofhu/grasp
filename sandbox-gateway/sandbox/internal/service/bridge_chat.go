// 有界 FIFO + 单 worker 串行处理（promptQueue + pumpPromptQueue）。
package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	"backend/internal/acp"
	"backend/internal/correl"
	"backend/internal/provider"
)

func (b *Bridge) clearPromptQueue() {
	b.queueMu.Lock()
	b.promptQueue = nil
	b.queueMu.Unlock()
}

// eventEnvelope 构造 op:event 帧；opId 挂在信封上（与 provider 无关），让客户端把帧归属到具体回合。
func eventEnvelope(data any, opID string) map[string]any {
	m := map[string]any{"op": "event", "data": data}
	if opID != "" {
		m["opId"] = opID
	}
	return m
}

// activeOpID 返回当前正在执行的回合 opId；空闲时为空。
func (b *Bridge) activeOpID() string {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	if b.activeTurn == nil {
		return ""
	}
	return b.activeTurn.opID
}

func (b *Bridge) BroadcastQueueState() {
	m := b.queueSnapshot()
	m["op"] = "queue_state"
	b.Broadcast(m)
}

// pumpPromptQueue 在「当前无 activeTurn」时从 FIFO 取一条并启动执行。
// 须在启动 goroutine **之前**同步占用 activeTurn，否则连续 Chat 会在首条 runPrompt 尚未设 activeTurn 时再次入泵，导致多条 session/prompt 并发（顺序错乱）；须保证每会话单 worker 串行消费。
func (b *Bridge) pumpPromptQueue() {
	b.turnMu.Lock()
	if b.activeTurn != nil {
		b.turnMu.Unlock()
		b.BroadcastQueueState()
		return
	}

	b.queueMu.Lock()
	if len(b.promptQueue) == 0 {
		b.queueMu.Unlock()
		b.turnMu.Unlock()
		b.BroadcastQueueState()
		return
	}
	item := b.promptQueue[0]
	b.promptQueue = b.promptQueue[1:]
	b.queueMu.Unlock()

	b.mu.Lock()
	p := b.sess
	ctx := b.agentCtx
	b.mu.Unlock()
	if p == nil {
		b.queueMu.Lock()
		b.promptQueue = append([]queuedPrompt{item}, b.promptQueue...)
		b.queueMu.Unlock()
		b.turnMu.Unlock()
		b.BroadcastQueueState()
		return
	}

	oid := item.OpID
	if oid == "" {
		oid = correl.ID()
	}
	turnCtx, cancelCause := context.WithCancelCause(ctx)
	cancelTurn := func() { cancelCause(context.Canceled) }
	th := &promptTurn{cancel: cancelTurn, cancelCause: cancelCause, opID: oid, userText: item.Text, imageCount: len(item.Images)}
	th.lastActivity.Store(time.Now().UnixNano())
	b.activeTurn = th
	b.turnMu.Unlock()

	log.Printf("prompt %s oid=%s: 开始 session/prompt textLen=%d", b.AgentLogPrefix(), oid, len(item.Text))

	b.BroadcastQueueState()
	// 每轮 prompt 边界：避免上一轮未收到 prompt_done 时前端仍把新正文流式接到同一助手块（见 chat_view appendStreamAgent）
	promptBeginData := map[string]any{
		"type":       "prompt_begin",
		"sessionId":  p.SessionID(),
		"opId":       oid,
		"text":       item.Text,
		"promptText": item.Text,
	}
	// 图片 → data URL 供前端重放气泡预览；非图片只记文件名（附件已统一落盘，不嵌入 prompt）。
	if len(item.Images) > 0 {
		urls := make([]string, 0, len(item.Images))
		fileNames := make([]string, 0, len(item.Images))
		for _, img := range item.Images {
			if img.Data == "" {
				continue
			}
			mime := img.MimeType
			if mime == "" {
				mime = "application/octet-stream"
			}
			if strings.HasPrefix(strings.ToLower(mime), "image/") {
				urls = append(urls, "data:"+mime+";base64,"+img.Data)
				continue
			}
			name := strings.TrimSpace(img.Name)
			if name == "" {
				name = "attachment" + provider.ExtForMIME(mime)
			}
			fileNames = append(fileNames, name)
		}
		if len(urls) > 0 {
			promptBeginData["imageURLs"] = urls
		}
		if len(fileNames) > 0 {
			promptBeginData["fileNames"] = fileNames
		}
	}
	b.Broadcast(eventEnvelope(promptBeginData, oid))

	go b.executePrompt(p, turnCtx, item, th, cancelTurn)
}

func (b *Bridge) executePrompt(p provider.Session, turnCtx context.Context, item queuedPrompt, th *promptTurn, cancelTurn context.CancelFunc) {
	oid := th.opID
	defer func() {
		b.turnMu.Lock()
		if b.activeTurn == th {
			b.activeTurn = nil
		}
		b.turnMu.Unlock()
		cancelTurn()
		b.recordUserTurnDone(item)
		b.pumpPromptQueue()
	}()

	// 统一附件策略：图片/文件一律落盘到 /tmp，prompt 里只引用绝对路径。
	// 这样 cursor / gemini / ACP 等都走同一套「读本地文件」能力，不再依赖各 CLI 的原生 image block。
	text := item.Text
	images := item.Images
	if len(images) > 0 {
		dir, paths, merr := provider.MaterializeAttachments(images)
		if merr != nil {
			log.Printf("prompt %s oid=%s: 附件落盘失败: %v", b.AgentLogPrefix(), oid, merr)
			b.Broadcast(map[string]any{"op": "error", "message": "附件保存失败: " + merr.Error(), "opId": oid})
			b.Broadcast(eventEnvelope(map[string]any{
				"type":       "prompt_done",
				"sessionId":  p.SessionID(),
				"stopReason": "failed",
				"opId":       oid,
			}, oid))
			return
		}
		defer os.RemoveAll(dir)
		log.Printf("prompt %s oid=%s: 已将 %d 个附件落到 %s", b.AgentLogPrefix(), oid, len(paths), dir)
		text = provider.AppendAttachmentRefs(text, paths)
		images = nil
	}

	idle, max := b.turnLimits(item)
	stopWatch := b.watchTurn(p, th, idle, max)
	res, err := p.Prompt(turnCtx, text, images)
	stopWatch()
	stopReason := res.StopReason
	if th.timedOut.Load() {
		log.Printf("prompt %s oid=%s: 超时终止 stopReason=%q err=%v", b.AgentLogPrefix(), oid, stopReason, err)
		if finishedBeforeExit(stopReason, err) {
			return
		}
		if stopReason == "" {
			// Transport returned without emitting prompt_done (e.g. ACP Call):
			// explain first — clients stop reading at prompt_done.
			b.Broadcast(eventEnvelope(map[string]any{"op": "raw", "type": "error_text", "text": timeoutCauseText(turnCtx), "opId": oid}, oid))
			b.Broadcast(eventEnvelope(map[string]any{
				"type":       "prompt_done",
				"sessionId":  p.SessionID(),
				"stopReason": provider.StopReasonTimeout,
				"opId":       oid,
			}, oid))
		}
		return
	}
	if err == nil {
		if sr := strings.ToLower(strings.TrimSpace(stopReason)); sr == "refusal" {
			log.Printf("prompt %s oid=%s: 轮次结束 stopReason=refusal（多为鉴权失败：检查 CODEBUDDY_API_KEY / 区域 ACP_CODEBUDDY_REGION，或执行 codebuddy login）", b.AgentLogPrefix(), oid)
			b.Broadcast(map[string]any{
				"op": "error",
				"message": "Agent 拒绝本轮请求（refusal）。CodeBuddy 多为 API Key 无效或区域不匹配：" +
					"请确认 CODEBUDDY_API_KEY（iOA 站：https://tencent.sso.copilot.tencent.com/profile/keys），" +
					"并设置 ACP_CODEBUDDY_REGION=ioa；也可 unset CODEBUDDY_API_KEY 后执行 codebuddy login。",
			})
		}
		return
	}
	log.Printf("prompt %s oid=%s: 结束 err=%v", b.AgentLogPrefix(), oid, err)
	if errors.Is(err, context.Canceled) {
		// Session already emitted prompt_done (e.g. oneshot); only synthesize
		// for transports that return cancel without a stop reason (ACP Call).
		if stopReason == "" && th.fromUserStop.Load() {
			b.Broadcast(eventEnvelope(map[string]any{
				"type":       "prompt_done",
				"sessionId":  p.SessionID(),
				"stopReason": "cancelled",
				"opId":       oid,
			}, oid))
		}
		return
	}
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "connection closed") || strings.Contains(low, "broken pipe") ||
		strings.Contains(low, "write |1:") || errors.Is(err, syscall.EPIPE) {
		// Pipe death usually means the agent process exited; surface to the UI
		// so the client is not left waiting on an open turn.
		b.Broadcast(map[string]any{"op": "error", "message": acp.UserFacingAny(err), "opId": oid})
		return
	}
	b.Broadcast(map[string]any{"op": "error", "message": acp.UserFacingAny(err), "opId": oid})
}

func (b *Bridge) CancelPrompt() {
	b.mu.Lock()
	p := b.sess
	b.mu.Unlock()

	var stopTurn context.CancelFunc
	b.turnMu.Lock()
	if t := b.activeTurn; t != nil {
		t.fromUserStop.Store(true)
		stopTurn = t.cancel
	}
	b.turnMu.Unlock()

	// 先通知 Agent（session/cancel + $/cancel_request），再取消本地 wait，避免只断客户端、子进程仍在跑
	if p != nil {
		if err := p.Cancel(); err != nil {
			log.Printf("ws→acp %s: Stop 通知 Agent 失败: %v", b.AgentLogPrefix(), err)
		}
	}
	if stopTurn != nil {
		stopTurn()
	}
	log.Printf("ws→acp %s: CancelPrompt 已执行", b.AgentLogPrefix())
	// 取消当前轮次并丢弃尚未开始处理的排队消息
	b.clearPromptQueue()
	b.BroadcastQueueState()
}

// Cancel ack statuses carried by op:cancel_ack.
const (
	CancelAckCancelling = "cancelling" // 正在执行的回合已发出取消，终态以该 opId 的 prompt_done 为准
	CancelAckRemoved    = "removed"    // 尚未开始的排队项已移除，不会再有 prompt_done
	CancelAckUnknown    = "unknown"    // 既不在执行也不在排队（多为已结束）
)

// CancelPromptOp 只取消 opID 对应的回合：正在执行的就取消它（不清空后续队列），排队中的就只移除这一条。
// opID 为空时等同 CancelPrompt（取消当前回合并清空队列）。每次都会广播 op:cancel_ack 作为回执。
func (b *Bridge) CancelPromptOp(opID string) string {
	opID = strings.TrimSpace(opID)
	if opID == "" {
		b.CancelPrompt()
		return CancelAckCancelling
	}
	status := CancelAckUnknown
	var stopTurn context.CancelFunc
	b.turnMu.Lock()
	if t := b.activeTurn; t != nil && t.opID == opID {
		t.fromUserStop.Store(true)
		stopTurn = t.cancel
		status = CancelAckCancelling
	}
	b.turnMu.Unlock()

	if stopTurn != nil {
		b.mu.Lock()
		p := b.sess
		b.mu.Unlock()
		if p != nil {
			if err := p.Cancel(); err != nil {
				log.Printf("ws→acp %s oid=%s: Stop 通知 Agent 失败: %v", b.AgentLogPrefix(), opID, err)
			}
		}
		stopTurn()
	} else if b.removeQueuedPrompt(opID) {
		status = CancelAckRemoved
	}
	log.Printf("ws→acp %s oid=%s: CancelPromptOp status=%s", b.AgentLogPrefix(), opID, status)
	b.Broadcast(map[string]any{"op": "cancel_ack", "opId": opID, "status": status})
	b.BroadcastQueueState()
	return status
}

func (b *Bridge) removeQueuedPrompt(opID string) bool {
	b.queueMu.Lock()
	defer b.queueMu.Unlock()
	for i, q := range b.promptQueue {
		if q.OpID == opID {
			b.promptQueue = append(b.promptQueue[:i:i], b.promptQueue[i+1:]...)
			return true
		}
	}
	return false
}

// ChatWithOpID 与 WebSocket 单次 chat 帧共用 oid（与 InMessage.ID 一致）；action 默认 chat。
// 无 oid 时自动生成（非 WS 入口时仍可有可搜日志键）。
func (b *Bridge) ChatWithOpID(text, opID, action string, images []PromptImage) error {
	return b.ChatWithDeadline(text, opID, action, images, 0)
}

// ChatWithDeadline 同 ChatWithOpID；maxDuration>0 时覆盖本回合的总时长上限。
func (b *Bridge) ChatWithDeadline(text, opID, action string, images []PromptImage, maxDuration time.Duration) error {
	t := strings.TrimSpace(text)
	if t == "" && len(images) == 0 {
		return errors.New("empty message")
	}
	if opID == "" {
		opID = correl.ID()
	}
	action = strings.TrimSpace(action)
	if action == "" {
		action = "chat"
	}
	b.mu.Lock()
	ok := b.sess != nil
	b.mu.Unlock()
	if !ok {
		return errors.New("not connected; send connect first")
	}

	b.enqueueMu.Lock()
	defer b.enqueueMu.Unlock()

	b.queueMu.Lock()
	if len(b.promptQueue) >= MaxPromptQueueItems {
		b.queueMu.Unlock()
		return fmt.Errorf("消息队列已满（最多 %d 条），请等待当前回复结束后再发", MaxPromptQueueItems)
	}
	b.promptQueue = append(b.promptQueue, queuedPrompt{Text: t, OpID: opID, Action: action, Images: images, MaxDuration: maxDuration})
	b.queueMu.Unlock()
	b.BroadcastQueueState()
	b.pumpPromptQueue()
	return nil
}
