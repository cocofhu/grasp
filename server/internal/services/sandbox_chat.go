package services

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
)

const sandboxChatQueueCapacity = 64

type sandboxChatter interface {
	Chat(ctx context.Context, id uint, text string, images []models.PromptImage, onEvent func(json.RawMessage)) error
	Cancel(id uint)
}

// SandboxChatItem is one queued Studio / console chat turn.
type SandboxChatItem struct {
	ID      string
	Content string
	Images  []models.PromptImage
}

// SandboxChats runs Agent Studio and sandbox-console chat turns: one
// chatsession FIFO per sandbox that outlives the WebSocket, so a refresh or a
// dropped connection neither loses the queue nor stops the running turn.
type SandboxChats struct {
	chat sandboxChatter

	mu    sync.Mutex
	chats map[uint]*sandboxChat
}

type sandboxChat struct {
	sess   *chatsession.Session[*SandboxChatItem]
	stream *chatsession.Stream
	// refs counts callers and subscribers; guarded by SandboxChats.mu.
	refs int
}

// NewSandboxChats builds the registry over the sandbox service.
func NewSandboxChats(sbx *SandboxService) *SandboxChats {
	c := &SandboxChats{chats: map[uint]*sandboxChat{}}
	if sbx != nil {
		c.chat = sbx
	}
	return c
}

// SetChatterForTest replaces the sandbox chat backend.
func (c *SandboxChats) SetChatterForTest(ch sandboxChatter) {
	c.chat = ch
}

func (c *SandboxChats) acquire(id uint) *sandboxChat {
	c.mu.Lock()
	defer c.mu.Unlock()
	sc := c.chats[id]
	if sc == nil {
		sc = c.newChat(id)
		c.chats[id] = sc
	}
	sc.refs++
	return sc
}

func (c *SandboxChats) release(id uint, sc *sandboxChat) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sc.refs--
	c.dropIfIdleLocked(id, sc)
}

func (c *SandboxChats) dropIfIdleLocked(id uint, sc *sandboxChat) {
	if sc.refs <= 0 && sc.sess.Idle() && c.chats[id] == sc {
		delete(c.chats, id)
	}
}

func (c *SandboxChats) newChat(id uint) *sandboxChat {
	sc := &sandboxChat{stream: chatsession.NewStream(0)}
	sc.sess = chatsession.New(chatsession.Config[*SandboxChatItem]{
		Capacity: sandboxChatQueueCapacity,
		View: func(it *SandboxChatItem) chatsession.ItemView {
			return chatsession.ItemView{ID: it.ID, Text: it.Content, Images: it.Images}
		},
		Execute: func(ctx context.Context, it *SandboxChatItem) (bool, error) {
			return false, c.chat.Chat(ctx, id, it.Content, it.Images, sc.stream.Acp)
		},
		Publish: func(event string, payload map[string]any) {
			keep := chatsession.KeepNone
			switch event {
			case chatsession.EventTurnBegin:
				keep = chatsession.KeepBegin
			case chatsession.EventTurnDone, chatsession.EventError:
				keep = chatsession.KeepEnd
			}
			sc.stream.Session(event, payload, keep)
		},
		OnIdle: func() {
			c.mu.Lock()
			c.dropIfIdleLocked(id, sc)
			c.mu.Unlock()
		},
		CancelTurn: func() { c.chat.Cancel(id) },
	})
	return sc
}

// Enqueue queues a turn for the sandbox and returns the pending count.
func (c *SandboxChats) Enqueue(id uint, item SandboxChatItem) (int, error) {
	if c.chat == nil {
		return 0, errors.New("sandbox chat unavailable")
	}
	if item.Content == "" && len(item.Images) == 0 {
		return 0, errors.New("empty message")
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	sc := c.acquire(id)
	defer c.release(id, sc)
	return sc.sess.Enqueue(&item, nil)
}

// Cancel stops the running turn and drops the queue.
func (c *SandboxChats) Cancel(id uint) {
	c.mu.Lock()
	sc := c.chats[id]
	c.mu.Unlock()
	if sc != nil {
		sc.sess.Cancel(true)
		return
	}
	if c.chat != nil {
		c.chat.Cancel(id)
	}
}

// Active reports a running or queued turn.
func (c *SandboxChats) Active(id uint) bool {
	c.mu.Lock()
	sc := c.chats[id]
	c.mu.Unlock()
	return sc != nil && !sc.sess.Ready()
}

// Subscribe streams the sandbox chat: a queue_state snapshot, the active
// turn's frames after afterSeq, then live frames until unsubscribe.
func (c *SandboxChats) Subscribe(id uint, afterSeq int) (<-chan chatsession.Event, func()) {
	sc := c.acquire(id)
	ch, unsubStream := sc.stream.Subscribe(func() (map[string]any, bool) {
		sn := sc.sess.Snapshot()
		return sn.QueueState(), sn.Busy
	}, afterSeq)
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			unsubStream()
			c.release(id, sc)
		})
	}
}
