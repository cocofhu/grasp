package services

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// deliveringChat delivers a file during the turn, as send_file does, and
// records the prompt it was given.
type deliveringChat struct {
	db        *gorm.DB
	sessionID string
	mu        sync.Mutex
	prompts   []string
}

func (c *deliveringChat) ChatWithTimeout(_ context.Context, _ uint, text string, _ []models.PromptImage, _ time.Duration, onEvent func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	c.mu.Lock()
	c.prompts = append(c.prompts, text)
	n := len(c.prompts)
	c.mu.Unlock()
	var msg models.SoloMessage
	if err := c.db.Where("session_id = ? AND role = ?", c.sessionID, "user").Order("created_at desc, id desc").First(&msg).Error; err != nil {
		return nil, nil, err
	}
	if n == 1 {
		c.db.Create(&models.ArtifactDelivery{
			ID: "d-" + msg.ID, SessionID: c.sessionID, TurnID: msg.ID,
			ArtifactAttachment: models.ArtifactAttachment{ArtifactID: "a1", Revision: 1, Name: "report.md", Kind: "markdown", SizeBytes: 3},
		})
	}
	return &models.TokenUsage{}, nil, nil
}

func (c *deliveringChat) Cancel(uint) {}

func (c *deliveringChat) prompt(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.prompts) {
		return ""
	}
	return c.prompts[i]
}

func soloReply(t *testing.T, db *gorm.DB, turnID string) (models.SoloMessage, bool) {
	t.Helper()
	var reply models.SoloMessage
	err := db.Where("id = ?", "reply:"+turnID).First(&reply).Error
	return reply, err == nil
}

// TestSoloTurnReplyKeepsDeliveredFiles locks that a file delivered while the
// Agent ran is attached to the saved reply, even with no reply text.
func TestSoloTurnReplyKeepsDeliveredFiles(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	v, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	chat := &deliveringChat{db: db, sessionID: v.ID}
	solo.SetChatterForTest(chat)
	if _, err := solo.Send(v.ID, "alice", "send me the report", nil); err != nil {
		t.Fatal(err)
	}
	var user models.SoloMessage
	waitFor(t, "reply saved", func() bool {
		if db.Where("session_id = ? AND role = ?", v.ID, "user").First(&user).Error != nil {
			return false
		}
		_, ok := soloReply(t, db, user.ID)
		return ok
	})
	reply, _ := soloReply(t, db, user.ID)
	if reply.Status != "ok" || len(reply.Files) != 1 || reply.Files[0].Name != "report.md" || reply.Files[0].ArtifactID != "a1" {
		t.Fatalf("reply = status %q files %+v", reply.Status, reply.Files)
	}
}

// TestSoloTurnRestartPreambleThenCleared locks the recreated-sandbox preamble:
// the turn after a rebuild carries the restart header and the dialogue, and a
// successful turn clears needs_preamble so the next turn is sent as typed.
func TestSoloTurnRestartPreambleThenCleared(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 3)
	v, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sandbox ready", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	earlier := time.Now().Add(-time.Minute)
	for _, m := range []models.SoloMessage{
		{ID: "m-old-user", SessionID: v.ID, Role: "user", Content: "build the login page", Status: "ok", CreatedAt: earlier},
		{ID: "reply:m-old-user", TurnID: "m-old-user", SessionID: v.ID, Role: "assistant", Content: "done", Status: "ok", CreatedAt: earlier.Add(time.Second)},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&models.SoloSession{}).Where("id = ?", v.ID).Update("needs_preamble", true).Error; err != nil {
		t.Fatal(err)
	}
	chat := &deliveringChat{db: db, sessionID: v.ID}
	solo.SetChatterForTest(chat)
	if _, err := solo.Send(v.ID, "alice", "now add logout", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "preamble cleared", func() bool {
		row, err := solo.Require(v.ID, "alice")
		return err == nil && !row.NeedsPreamble && chat.prompt(0) != ""
	})
	first := chat.prompt(0)
	if !strings.HasPrefix(first, soloRestartPreambleHeader) || !strings.Contains(first, "build the login page") || !strings.HasSuffix(first, "用户本轮消息：\nnow add logout") {
		t.Fatalf("first prompt = %q", first)
	}
	waitFor(t, "first turn idle", func() bool {
		c := solo.lookup(v.ID)
		return c != nil && c.sess.Ready()
	})
	if _, err := solo.Send(v.ID, "alice", "and a footer", nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "second prompt", func() bool { return chat.prompt(1) != "" })
	if got := chat.prompt(1); got != "and a footer" {
		t.Fatalf("second prompt = %q", got)
	}
}
