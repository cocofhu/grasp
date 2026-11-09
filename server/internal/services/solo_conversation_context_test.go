package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

type conversationContextChat struct {
	mu         sync.Mutex
	native     string
	prepareErr error
	fail       bool
	prompts    []string
}

func (c *conversationContextChat) prepareSoloConversation(_ context.Context, id uint) (soloConversationIdentity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return soloConversationIdentity{sandboxID: id, native: c.native, workspace: "/workspace", fingerprint: "logical-root"}, c.prepareErr
}

func (c *conversationContextChat) ChatWithTimeout(_ context.Context, _ uint, prompt string, _ []models.PromptImage, _ time.Duration, emit func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, prompt)
	fail := c.fail
	c.mu.Unlock()
	emit(json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"current native response"}}`))
	if fail {
		return nil, nil, errors.New("native turn failed")
	}
	return nil, nil, nil
}

func (*conversationContextChat) Cancel(uint) {}

func (c *conversationContextChat) promptsCopy() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.prompts...)
}

func contextConversationFixture(t *testing.T) (*SoloService, *SandboxService, *gorm.DB, string, *conversationContextChat) {
	t.Helper()
	acp, port := fakeACPServer(t)
	t.Cleanup(acp.Close)
	solo, sbx, _, db := newSoloFixture(t, port, 3)
	chat := &conversationContextChat{native: "backend-A-agent-1"}
	solo.SetChatterForTest(chat)
	solo.SetModelAlignerForTest(&recordAligner{})
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = solo.Close(context.Background(), view.ID, "alice") })
	waitFor(t, "context sandbox running", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	if err := db.Model(&models.SoloSession{}).Where("id = ?", view.ID).Updates(map[string]any{
		"needs_preamble": false, "model_context_pending": false, "conversation_context_pending": false, "resume_context_pending": false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Minute).UTC()
	for i := range 16 {
		row := models.SoloMessage{ID: fmt.Sprintf("prior-%02d", i), SessionID: view.ID, Role: "user", Content: fmt.Sprintf("previous request %02d", i), CreatedAt: at.Add(time.Duration(i) * time.Second)}
		if i%2 == 1 {
			row.Role, row.Content = "assistant", fmt.Sprintf("original assistant result %02d", i)
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return solo, sbx, db, view.ID, chat
}

func sendContextConversationTurn(t *testing.T, solo *SoloService, id, text string) {
	t.Helper()
	if _, err := solo.Send(id, "alice", text, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "context turn settled", func() bool { return solo.acquire(id).sess.Idle() })
}

func TestSoloConversationRestoresNewHandleAndChangedNativeContext(t *testing.T) {
	solo, sbx, db, id, chat := contextConversationFixture(t)
	sendContextConversationTurn(t, solo, id, "new input exactly once")
	first := chat.promptsCopy()[0]
	if !strings.Contains(first, "original assistant result 01") || !strings.Contains(first, "original assistant result 15") || strings.Count(first, "new input exactly once") != 1 {
		t.Fatalf("new handle omitted bounded history or repeated current input: %s", first)
	}
	sendContextConversationTurn(t, solo, id, "same native context")
	if got := chat.promptsCopy()[1]; got != "same native context" {
		t.Fatalf("same native context repeated preamble: %s", got)
	}
	chat.mu.Lock()
	chat.native = "backend-B-agent-1"
	chat.mu.Unlock()
	sendContextConversationTurn(t, solo, id, "changed native context")
	if got := chat.promptsCopy()[2]; !strings.Contains(got, "original assistant result 01") || strings.Count(got, "changed native context") != 1 {
		t.Fatalf("new native context omitted history: %s", got)
	}
	// An idle prior worker has released its durable owner. A fresh service
	// models application restart while keeping the exact native context alive.
	restarted := NewSoloService(db, sbx, solo.agents, solo.projects)
	restarted.SetChatterForTest(chat)
	restarted.SetModelAlignerForTest(&recordAligner{})
	sendContextConversationTurn(t, restarted, id, "new application handle")
	if got := chat.promptsCopy()[3]; !strings.Contains(got, "original assistant result 01") || strings.Count(got, "new application handle") != 1 {
		t.Fatalf("application restart trusted cleared SQL flags: %s", got)
	}
	sendContextConversationTurn(t, restarted, id, "continued application handle")
	if got := chat.promptsCopy()[4]; got != "continued application handle" {
		t.Fatalf("continued context repeated old messages: %s", got)
	}
}

func TestSoloConversationFailedTurnDoesNotRememberNewContext(t *testing.T) {
	solo, _, _, id, chat := contextConversationFixture(t)
	chat.mu.Lock()
	chat.fail = true
	chat.mu.Unlock()
	sendContextConversationTurn(t, solo, id, "failed explicit turn")
	c := solo.acquire(id)
	c.mu.Lock()
	remembered := c.conversation
	c.mu.Unlock()
	if remembered != (soloConversationIdentity{}) {
		t.Fatal("failed turn remembered an uncommitted context baseline")
	}
	chat.mu.Lock()
	chat.fail = false
	chat.mu.Unlock()
	sendContextConversationTurn(t, solo, id, "next explicit turn")
	if got := chat.promptsCopy()[1]; !strings.Contains(got, "original assistant result 01") || strings.Count(got, "next explicit turn") != 1 {
		t.Fatal("next explicit turn lost persisted context after a failure")
	}
}

func TestSoloConversationPublicationFailureDoesNotRememberContext(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	var failReply atomic.Bool
	// Register before any background executor starts; toggle only the atomic
	// switch instead of mutating GORM's shared callback registry during a turn.
	if err := db.Callback().Create().Before("gorm:create").Register("test:conversation_reply_failure", func(tx *gorm.DB) {
		if record, ok := tx.Statement.Dest.(*soloMessageRecord); ok && record.Role == "assistant" && failReply.Load() {
			tx.AddError(errors.New("reply commit failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	chat := &conversationContextChat{native: "native-A"}
	solo.SetChatterForTest(chat)
	solo.SetModelAlignerForTest(&recordAligner{})
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = solo.Close(context.Background(), view.ID, "alice") }()
	failReply.Store(true)
	if _, err := solo.Send(view.ID, "alice", "completed native, failed SQL", nil); err != nil {
		t.Fatal(err)
	}
	c := solo.acquire(view.ID)
	waitFor(t, "failed publisher stopped without replay", func() bool {
		return len(chat.promptsCopy()) == 1 && !c.sess.View().Pumping
	})
	c.mu.Lock()
	identity := c.conversation
	c.mu.Unlock()
	if len(chat.promptsCopy()) != 1 || identity != (soloConversationIdentity{}) {
		t.Fatal("failed fenced publication was treated as a synchronized native context")
	}
}

func TestSoloConversationPrepareFailureDoesNotDispatch(t *testing.T) {
	solo, _, _, id, chat := contextConversationFixture(t)
	chat.mu.Lock()
	chat.prepareErr = errors.New("read-only native identity unavailable")
	chat.mu.Unlock()
	sendContextConversationTurn(t, solo, id, "explicit preparation failure")
	if len(chat.promptsCopy()) != 0 {
		t.Fatal("identity read failure dispatched a prompt")
	}
}

func TestSoloConversationReadOnlyIdentitySurvivesReconnectAndDetectsAgentChange(t *testing.T) {
	solo, sbx, _, id, _ := contextConversationFixture(t)
	row, err := solo.Require(id, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var native atomic.Value
	native.Store("backend-instance-A-agent-1")
	var callsMu sync.Mutex
	var calls []string
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test-cookie", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		for {
			var request struct{ Op string }
			if err := connection.ReadJSON(&request); err != nil {
				return
			}
			callsMu.Lock()
			calls = append(calls, request.Op)
			callsMu.Unlock()
			replyOp := "operation_session"
			if request.Op == "connect" {
				replyOp = "connected"
			}
			if err := connection.WriteJSON(map[string]string{"op": replyOp, "sessionId": "vendor-display-only", "nativeSessionId": native.Load().(string), "cwd": "/workspace", "workspaceFingerprint": "same-logical-root"}); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	_, portText, _ := strings.Cut(server.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portText)
	connect := func() *sandbox.ACPClient {
		t.Helper()
		client := sandbox.NewACPClient("127.0.0.1", port).WithPassword("fixture-only").WithSession("/workspace", nil)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		return client
	}
	firstClient := connect()
	sbx.mu.Lock()
	live := sbx.live[row.SandboxID]
	oldClient := live.acp
	live.acp = firstClient
	sbx.mu.Unlock()
	oldClient.Close()
	first, err := sbx.prepareSoloConversation(context.Background(), row.SandboxID)
	if err != nil {
		t.Fatal(err)
	}
	secondClient := connect()
	sbx.mu.Lock()
	live.acp = secondClient
	sbx.mu.Unlock()
	firstClient.Close()
	reconnected, err := sbx.prepareSoloConversation(context.Background(), row.SandboxID)
	if err != nil || first != reconnected || reconnected.client != nil {
		t.Fatalf("same native context changed on websocket reconnect: %+v %+v %v", first, reconnected, err)
	}
	native.Store("backend-instance-A-agent-2")
	changed, err := sbx.prepareSoloConversation(context.Background(), row.SandboxID)
	if err != nil || changed == reconnected || changed.native != "backend-instance-A-agent-2" {
		t.Fatalf("native Agent generation change was not refreshed: %+v %v", changed, err)
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	for _, op := range calls {
		if op != "connect" && op != "operation_session" {
			t.Fatalf("context inspection mutated native state: %s", op)
		}
	}
}
