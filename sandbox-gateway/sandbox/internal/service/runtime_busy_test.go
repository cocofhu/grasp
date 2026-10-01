package service

import (
	"testing"

	"github.com/gorilla/websocket"
)

func TestRuntimeBusy(t *testing.T) {
	m := NewChatManager()
	if busy, reason := m.RuntimeBusy(); busy || reason != "" {
		t.Fatalf("fresh manager busy=%v reason=%q", busy, reason)
	}
	other, err := m.Create("", "")
	if err != nil {
		t.Fatal(err)
	}

	other.mu.Lock()
	other.clients[&websocket.Conn{}] = &wsClient{}
	other.mu.Unlock()
	if busy, reason := m.RuntimeBusy(); !busy || reason != "clients:"+other.ID() {
		t.Fatalf("clients: busy=%v reason=%q", busy, reason)
	}
	other.mu.Lock()
	other.clients = map[*websocket.Conn]*wsClient{}
	other.mu.Unlock()

	other.queueMu.Lock()
	other.promptQueue = append(other.promptQueue, queuedPrompt{})
	other.queueMu.Unlock()
	if busy, reason := m.RuntimeBusy(); !busy || reason != "queue:"+other.ID() {
		t.Fatalf("queue: busy=%v reason=%q", busy, reason)
	}
	other.queueMu.Lock()
	other.promptQueue = nil
	other.queueMu.Unlock()

	def := m.Default()
	def.turnMu.Lock()
	def.activeTurn = &promptTurn{}
	def.turnMu.Unlock()
	if busy, reason := m.RuntimeBusy(); !busy || reason != "turn:"+DefaultChatID {
		t.Fatalf("turn: busy=%v reason=%q", busy, reason)
	}
	def.turnMu.Lock()
	def.activeTurn = nil
	def.turnMu.Unlock()
	if busy, _ := m.RuntimeBusy(); busy {
		t.Fatal("idle again should not be busy")
	}
}
