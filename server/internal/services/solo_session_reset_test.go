package services

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloSessionResetReplaysDialogueOnce(t *testing.T) {
	f := newStopFixture(t)
	chat := newRestoreChat(false)
	f.solo.SetChatterForTest(chat)
	answered := func(n int) {
		waitFor(t, "turn answered", func() bool {
			msgs, _ := f.solo.Messages(f.id, "alice")
			ok := 0
			for _, m := range msgs {
				if m.Role == "assistant" && m.Status == "ok" {
					ok++
				}
			}
			return ok == n
		})
	}
	if _, err := f.solo.SendWithReferences(f.id, "alice", "改了哪些东西", nil, nil); err != nil {
		t.Fatal(err)
	}
	answered(1)
	if err := f.db.Model(&models.SoloSession{}).Where("sandbox_id = ?", f.row(t).SandboxID).Update("session_context_pending", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.solo.SendWithReferences(f.id, "alice", "说中文", nil, nil); err != nil {
		t.Fatal(err)
	}
	answered(2)
	if _, err := f.solo.SendWithReferences(f.id, "alice", "继续", nil, nil); err != nil {
		t.Fatal(err)
	}
	answered(3)

	got := chat.seen()
	if len(got) != 3 {
		t.Fatalf("prompts=%q", got)
	}
	if !strings.HasPrefix(got[1], soloSessionResetPreambleHeader) || !strings.Contains(got[1], "改了哪些东西") || !strings.HasSuffix(got[1], "说中文") {
		t.Fatalf("turn after a session reset must carry the dialogue: %q", got[1])
	}
	if strings.Contains(got[1], "停止状态恢复") || strings.Contains(got[1], "未提交的改动已丢失") {
		t.Fatalf("session reset must not claim the sandbox changed: %q", got[1])
	}
	if got[2] != "继续" || f.row(t).SessionContextPending {
		t.Fatalf("context must be replayed only once: prompt=%q pending=%v", got[2], f.row(t).SessionContextPending)
	}
}
