package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/handoff"
	"github.com/cocofhu/grasp/internal/models"
)

func TestReactHandoffEntriesKeepStructuredParts(t *testing.T) {
	entries := reactHandoffEntries([]models.ReactMessage{
		{Role: "agent", Text: "用亮色吗", Questions: []models.ReactQuestion{{Prompt: "配色", Options: []models.ReactOption{{Label: "亮色"}}}},
			Forms: []models.ReactForm{{Title: "环境", Fields: []models.ReactFormField{{Label: "DB_URL", Value: "set"}, {Label: "TOKEN"}}}},
			Parts: []models.AcpPart{{Kind: "tool", Title: "read", Summary: "main.go"}, {Kind: "tool"}}},
		{Role: "human", Text: "先看截图", Images: []models.PromptImage{{Name: "shot.png"}, {}},
			Annotations: []models.ReactAnnotation{{Label: "$.title", Note: "改短"}, {Note: "only note"}, {}}},
		{Role: "agent", Text: "(澄清回复失败:boom)"},
		{Role: "agent", Text: "done", Tools: []models.ReactTool{{Title: "edit"}, {}}},
	})
	if len(entries) != 3 {
		t.Fatalf("entries = %+v", entries)
	}
	got := handoff.Render(entries, handoff.Options{TokenCap: 4000})
	for _, want := range []string{
		"[#1 Agent] 用亮色吗\n  问题: 配色\n    - 亮色\n  表单: 环境\n    - DB_URL: set\n    - TOKEN\n  工具: read main.go\n",
		"[#2 用户] 先看截图\n  附件: shot.png（需要时请让用户重新上传）\n  附件: 未命名附件（需要时请让用户重新上传）\n  批注: $.title — 改短\n  批注: only note\n",
		"[#3 Agent] done\n  工具: edit\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("handoff missing %q in %q", want, got)
		}
	}
}

func TestRenderHandoffIsBoundedAndKeepsLatestDecision(t *testing.T) {
	var history []models.ReactMessage
	history = append(history, models.ReactMessage{Role: "human", Text: "原始需求：做一个看板"})
	for i := 0; i < 300; i++ {
		history = append(history, models.ReactMessage{Role: "agent", Text: strings.Repeat("甲", 80)})
	}
	history = append(history, models.ReactMessage{Role: "human", Text: "最新决定用亮色"})
	out := renderHandoff(history)
	for _, want := range []string{handoff.Notice, handoffProductsNote, "原始需求：做一个看板", "最新决定用亮色", "为控制长度，省略了"} {
		if !strings.Contains(out, want) {
			t.Errorf("handoff missing %q", want)
		}
	}
	if handoff.Tokens(out) > handoff.DefaultTokenCap {
		t.Fatalf("tokens %d over cap", handoff.Tokens(out))
	}
}

func TestHandoffToolNamesAreBounded(t *testing.T) {
	var parts []models.AcpPart
	var tools []models.ReactTool
	for i := 0; i < 30; i++ {
		parts = append(parts, models.AcpPart{Kind: "tool", Title: "t"})
		tools = append(tools, models.ReactTool{Title: "t"})
	}
	if n := len(handoffToolNames(models.ReactMessage{Parts: parts})); n != maxHandoffTools {
		t.Fatalf("parts kept %d", n)
	}
	if n := len(handoffToolNames(models.ReactMessage{Tools: tools})); n != maxHandoffTools {
		t.Fatalf("tools kept %d", n)
	}
}
