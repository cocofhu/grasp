package runtime

import (
	"context"
	"strings"

	"github.com/cocofhu/grasp/internal/handoff"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

const handoffProductsNote = "已写入的平台产物若存在，请先用 get_clarified_requirement 与 get_plan 读取，不要凭记忆重写。"

// renderHandoff is the dialogue a native session gets when it has not seen
// it: a fresh sandbox, a dropped resume id, or a compacted context.
func renderHandoff(history []models.ReactMessage) string {
	return handoff.Render(reactHandoffEntries(history), handoff.Options{Notes: []string{handoffProductsNote}}).Text
}

// reactHandoffEntries flattens persisted rows into handoff entries. A steer
// the human sent mid-turn becomes a user entry before the reply it shaped.
// Tool calls keep their names only; their output was never persisted.
func reactHandoffEntries(history []models.ReactMessage) []handoff.Entry {
	var out []handoff.Entry
	for _, m := range history {
		if isClarifyFailedOpenText(m.Text) {
			continue
		}
		if m.Role != "human" {
			for _, s := range models.SteerTexts(m.Parts) {
				out = append(out, handoff.Entry{Role: handoff.User, Text: s, Details: []string{"（回合进行中补充）"}})
			}
		}
		e := handoff.Entry{Role: handoff.Agent, Text: m.Text}
		if m.Role == "human" {
			e.Role = handoff.User
		}
		for _, q := range m.Questions {
			e.Details = append(e.Details, "问题: "+strings.TrimSpace(q.Prompt))
			for _, o := range q.Options {
				e.Details = append(e.Details, "  - "+strings.TrimSpace(o.Label))
			}
		}
		for _, f := range m.Forms {
			if title := strings.TrimSpace(f.Title); title != "" {
				e.Details = append(e.Details, "表单: "+title)
			}
			for _, field := range f.Fields {
				line := "  - " + strings.TrimSpace(field.Label)
				if v := strings.TrimSpace(field.Value); v != "" {
					line += ": " + v
				}
				e.Details = append(e.Details, line)
			}
		}
		if m.Role == "human" {
			for _, im := range m.Images {
				name := strings.TrimSpace(im.Name)
				if name == "" {
					name = "未命名附件"
				}
				e.Details = append(e.Details, "附件: "+name+"（需要时请让用户重新上传）")
			}
			for _, a := range m.Annotations {
				label, note := strings.TrimSpace(a.Label), strings.TrimSpace(a.Note)
				switch {
				case label != "" && note != "":
					e.Details = append(e.Details, "批注: "+label+" — "+note)
				case label != "" || note != "":
					e.Details = append(e.Details, "批注: "+label+note)
				}
			}
		}
		if tools := handoffToolNames(m); len(tools) > 0 {
			e.Details = append(e.Details, "工具: "+strings.Join(tools, "；"))
		}
		out = append(out, e)
	}
	return out
}

const maxHandoffTools = 20

func handoffToolNames(m models.ReactMessage) []string {
	var out []string
	for _, p := range m.Parts {
		if p.Kind != "tool" || len(out) >= maxHandoffTools {
			continue
		}
		line := strings.TrimSpace(strings.TrimSpace(p.Title) + " " + strings.TrimSpace(p.Summary))
		if line != "" {
			out = append(out, line)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, t := range m.Tools {
		if title := strings.TrimSpace(t.Title); title != "" && len(out) < maxHandoffTools {
			out = append(out, title)
		}
	}
	return out
}

// handOff sends a prompt carrying the dialogue and tracks its delivery. Until
// the turn completes cleanly the native session may or may not hold the
// history, so the node stays marked and handoffUnconfirmed steers the next
// recovery to a fresh native session instead of replaying it twice.
func (c *acpProvider) handOff(ctx context.Context, acp *sandbox.ACPClient, req NodeReq, prompt string, images []models.PromptImage) (*sandbox.ChatResult, error) {
	key := reactKey(req)
	c.mu.Lock()
	if c.handoffs == nil {
		c.handoffs = map[string]bool{}
	}
	c.handoffs[key] = true
	c.mu.Unlock()
	res, err := c.streamChat(ctx, acp, req, prompt, images)
	c.carryChatUsage(key, res)
	if err == nil && chatFailure(res) == "" {
		c.forgetHandoff(key)
	}
	return res, err
}

func (c *acpProvider) handoffUnconfirmed(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handoffs[key]
}

func (c *acpProvider) forgetHandoff(key string) {
	c.mu.Lock()
	delete(c.handoffs, key)
	c.mu.Unlock()
}
