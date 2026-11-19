package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"backend/internal/agents"
	"backend/internal/provider"
)

// UserChatCommand is selected only by the console or an explicit platform
// command frame. Ordinary host prompts use the opaque chat action.
const UserChatCommand = "user_chat_command"

func commandWord(text string) (string, string) {
	text = strings.TrimSpace(text)
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		return text[:i], strings.TrimSpace(text[i:])
	}
	return text, ""
}

func (b *Bridge) executeChatCommand(ctx context.Context, session provider.Session, item queuedPrompt, opID string) (string, bool) {
	name, args := commandWord(item.Text)
	switch name {
	case "/skill":
		skill, task := commandWord(args)
		prompt, err := consoleSkillPrompt(skill, task)
		if err != nil {
			b.finishChatCommand(session.SessionID(), opID, "", err)
			return "", true
		}
		return prompt, false
	case "/new":
		if args != "" && args != "chat" {
			return item.Text, false
		}
	case "/compact":
		if args != "" {
			return item.Text, false
		}
	default:
		return item.Text, false
	}

	if len(item.Images) != 0 {
		b.finishChatCommand(session.SessionID(), opID, "", errors.New("请单独发送 /new 或 /compact"))
		return "", true
	}
	if err := ctx.Err(); err != nil {
		b.finishChatCommand(session.SessionID(), opID, "", err)
		return "", true
	}
	preamble := ""
	status := "已开启新对话。"
	if name == "/compact" {
		preamble = b.compactTranscript(opID)
		status = "已压缩上下文。下一条消息将带上近期对话摘录。"
	}
	next, err := b.restartChatContext(preamble, false)
	if err != nil {
		b.finishChatCommand(session.SessionID(), opID, "", err)
		b.broadcastAgentExitDebounced(err.Error())
		return "", true
	}
	b.Broadcast(eventEnvelope(map[string]any{"type": "context_command", "command": name, "opId": opID}, opID))
	b.finishChatCommand(next.SessionID(), opID, status, nil)
	b.Broadcast(b.ConnectedPayload(next))
	return "", true
}

func (b *Bridge) finishChatCommand(sessionID, opID, status string, err error) {
	stop := "end_turn"
	if err != nil {
		stop = "failed"
		b.Broadcast(eventEnvelope(map[string]any{"type": "error_text", "text": err.Error(), "opId": opID}, opID))
	} else {
		b.Broadcast(eventEnvelope(map[string]any{"type": "session_update", "sessionId": sessionID, "opId": opID,
			"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": status}},
		}, opID))
	}
	b.Broadcast(eventEnvelope(map[string]any{"type": "prompt_done", "sessionId": sessionID, "stopReason": stop, "opId": opID}, opID))
}

func (b *Bridge) prependCompactContext(text string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.compactPreamble == "" {
		return text
	}
	return "此前对话（节选）：\n" + b.compactPreamble + "\n\n本轮用户消息：\n" + text
}

func consoleSkillPrompt(name, task string) (string, error) {
	if name == "" {
		return "", errors.New("请输入 /skill 和 skill 名称")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.ContainsFunc(name, unicode.IsControl) {
		return "", fmt.Errorf("无效的 skill 名称 %q", name)
	}
	root := os.Getenv("CONFIG_ROOT")
	if root == "" {
		root = agents.Current().DefaultConfigRoot()
	}
	entry, ok := consoleSkillEntry(root, name)
	if !ok {
		return "", fmt.Errorf("当前 Agent 没有名为 %q 的 skill", name)
	}
	instruction := fmt.Sprintf("本轮请使用 skill %q，先阅读 %q。skill 中的相对路径以 %q 为基准。", name, entry, filepath.Dir(entry))
	return strings.TrimSpace(instruction + "\n\n" + task), nil
}

func consoleSkillEntry(home, name string) (string, bool) {
	config, err := os.OpenRoot(home)
	if err != nil {
		return "", false
	}
	defer func() { _ = config.Close() }()
	skills, err := config.OpenRoot("skills")
	if err != nil {
		return "", false
	}
	defer func() { _ = skills.Close() }()
	entries, err := fs.ReadDir(skills.FS(), ".")
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.Name() != name {
			continue
		}
		relative := filepath.Join(entry.Name(), "SKILL.md")
		info, err := skills.Stat(relative)
		if err != nil || !info.Mode().IsRegular() {
			return "", false
		}
		return filepath.Join(home, "skills", relative), true
	}
	return "", false
}

func (b *Bridge) compactTranscript(currentOp string) string {
	events, _, _ := b.EventLogRecentTurns(9) // eight preceding turns plus the command
	var excerpts []string
	for _, events := range splitEventsByTurn(events) {
		var text strings.Builder
		control := false
		for _, raw := range events {
			var event struct {
				Type, OpID, Text, Command string
				Update                    struct {
					SessionUpdate string
					Content       struct{ Text string }
				}
			}
			if json.Unmarshal(raw, &event) != nil {
				continue
			}
			switch event.Type {
			case "prompt_begin":
				control = event.OpID == currentOp
				text.WriteString("用户：" + excerpt(event.Text, 750) + "\nAgent：")
			case "context_command":
				control = true
				if event.Command == "/new" {
					excerpts = nil
				}
			case "context_boundary":
				control = true
				excerpts = nil
			case "session_update":
				if event.Update.SessionUpdate == "agent_message_chunk" && text.Len() < 6400 {
					text.WriteString(excerpt(event.Update.Content.Text, 850))
				}
			}
		}
		if !control && text.Len() > 0 {
			excerpts = append(excerpts, excerpt(text.String(), 1600))
		}
	}
	return strings.Join(excerpts, "\n\n")
}

func excerpt(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
