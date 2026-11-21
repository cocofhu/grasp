package service

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/provider"

	"github.com/gorilla/websocket"
)

// steerSess is a blocking session that takes mid-turn input.
type steerSess struct {
	blockingSess
	unsupported bool
	err         error
	// beforeReply runs inside Steer, like events the agent streams before
	// it answers the steer request.
	beforeReply func()
	mu          sync.Mutex
	steered     []string
}

func (s *steerSess) Capabilities() provider.Capabilities {
	return provider.Capabilities{Steer: !s.unsupported}
}

func (s *steerSess) Steer(text string) error {
	if s.beforeReply != nil {
		s.beforeReply()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.steered = append(s.steered, text)
	return nil
}

type steerAck struct {
	Op       string `json:"op"`
	SteerID  string `json:"steerId"`
	TurnOpID string `json:"turnOpId"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

func readSteerAck(t *testing.T, c *websocket.Conn) steerAck {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var ack steerAck
		if json.Unmarshal(raw, &ack) == nil && ack.Op == "steer_ack" {
			return ack
		}
	}
}

func startBlockingTurn(t *testing.T, b *Bridge, opID string) {
	t.Helper()
	if err := b.ChatWithLimits("first", opID, "chat", nil, 0, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.CancelPromptOp(opID); waitIdle(t, b) })
}

func TestSteerTurnDeliversIntoRunningTurn(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sess := &steerSess{blockingSess: blockingSess{stubSess: stubSess{id: "s1"}}}
	b := newTestBridge(sess)
	c := dialBridge(t, b)
	startBlockingTurn(t, b, "op-A")
	readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "prompt_begin" })

	images := []PromptImage{{Data: "aGk=", MimeType: "image/png"}, {Data: "aGk=", MimeType: "text/plain", Name: "notes.txt"}}
	if got := b.SteerTurn("op-A", "steer-1", "  also check tests  ", images); got != SteerDelivered {
		t.Fatalf("status = %q", got)
	}
	ev := readUntil(t, c, func(f wsFrame) bool { return f.Op == "event" && dataType(f) == "user_steer" })
	if ev.OpID != "op-A" {
		t.Fatalf("user_steer belongs to the steered turn, got opId=%q", ev.OpID)
	}
	var data struct {
		SteerID   string   `json:"steerId"`
		Text      string   `json:"text"`
		ImageURLs []string `json:"imageURLs"`
		FileNames []string `json:"fileNames"`
	}
	_ = json.Unmarshal(ev.Data, &data)
	if data.SteerID != "steer-1" || data.Text != "also check tests" || len(data.ImageURLs) != 1 || len(data.FileNames) != 1 {
		t.Fatalf("user_steer = %+v", data)
	}
	if ack := readSteerAck(t, c); ack.SteerID != "steer-1" || ack.TurnOpID != "op-A" || ack.Status != SteerDelivered {
		t.Fatalf("ack = %+v", ack)
	}
	sess.mu.Lock()
	steered := append([]string(nil), sess.steered...)
	sess.mu.Unlock()
	if len(steered) != 1 || !strings.HasPrefix(steered[0], "also check tests") || !strings.Contains(steered[0], "notes.txt") {
		t.Fatalf("agent input = %q", steered)
	}
}

func TestSteerTurnStatuses(t *testing.T) {
	base := func() *steerSess { return &steerSess{blockingSess: blockingSess{stubSess: stubSess{id: "s1"}}} }
	for _, tc := range []struct {
		name    string
		sess    provider.Session
		running bool
		opID    string
		text    string
		images  []PromptImage
		status  string
		message string
	}{
		{name: "empty", sess: base(), running: true, opID: "op-A", status: SteerFailed, message: "empty message"},
		{name: "no session", status: SteerNoTurn, opID: "op-A", text: "x"},
		{name: "not a steerer", sess: &blockingSess{stubSess: stubSess{id: "s1"}}, running: true, opID: "op-A", text: "x", status: SteerUnsupported},
		{name: "steering off", sess: &steerSess{blockingSess: blockingSess{stubSess: stubSess{id: "s1"}}, unsupported: true}, running: true, opID: "op-A", text: "x", status: SteerUnsupported},
		{name: "idle", sess: base(), opID: "op-A", text: "x", status: SteerNoTurn},
		{name: "other turn", sess: base(), running: true, opID: "op-B", text: "x", status: SteerNoTurn},
		{name: "turn ended in the agent", sess: &steerSess{blockingSess: blockingSess{stubSess: stubSess{id: "s1"}}, err: provider.ErrNoActiveTurn}, running: true, opID: "op-A", text: "x", status: SteerNoTurn},
		{name: "write failed", sess: &steerSess{blockingSess: blockingSess{stubSess: stubSess{id: "s1"}}, err: errors.New("broken pipe")}, running: true, opID: "op-A", text: "x", status: SteerFailed, message: "broken pipe"},
		{name: "bad attachment", sess: base(), running: true, opID: "op-A", images: []PromptImage{{Data: "%%%", MimeType: "image/png"}}, status: SteerFailed, message: "附件保存失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBridge()
			if tc.sess != nil {
				b = newTestBridge(tc.sess)
			}
			c := dialBridge(t, b)
			if tc.running {
				startBlockingTurn(t, b, "op-A")
			}
			if got := b.SteerTurn(tc.opID, "sid-1", tc.text, tc.images); got != tc.status {
				t.Fatalf("status = %q, want %q", got, tc.status)
			}
			ack := readSteerAck(t, c)
			if ack.Status != tc.status || ack.SteerID != "sid-1" || ack.TurnOpID != tc.opID || !strings.Contains(ack.Message, tc.message) {
				t.Fatalf("ack = %+v", ack)
			}
		})
	}
}

func TestSteerReplyBehindTurnEventsIsDelivered(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	sess := &steerSess{blockingSess: blockingSess{stubSess: stubSess{id: "s1"}}}
	b := newTestBridge(sess)
	sess.beforeReply = func() { b.touchActiveTurn(json.RawMessage(`{"type":"agent_message_chunk"}`)) }
	startBlockingTurn(t, b, "op-A")
	done := make(chan string, 1)
	go func() { done <- b.SteerTurn("op-A", "steer-1", "also check tests", nil) }()
	select {
	case got := <-done:
		if got != SteerDelivered {
			t.Fatalf("status = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a turn event streamed before the steer reply blocked delivery")
	}
}
