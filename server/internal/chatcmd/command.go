// Package chatcmd defines the platform's Agent chat commands.
package chatcmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const (
	New     = "new"
	Compact = "compact"
	Skill   = "skill"
)

type Command struct {
	Name   string
	Skill  string `json:",omitempty"`
	Prompt string `json:",omitempty"`
}

func (c Command) ChangesContext() bool {
	return c.Name == New || c.Name == Compact
}

func cutWord(text string) (string, string) {
	text = strings.TrimSpace(text)
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		return text[:i], strings.TrimSpace(text[i:])
	}
	return text, ""
}

func Parse(text string) Command {
	if !strings.HasPrefix(strings.TrimSpace(text), "/") {
		return Command{}
	}
	name, args := cutWord(text)
	switch name {
	case "/new":
		if args == "" || args == "chat" {
			return Command{Name: New}
		}
	case "/compact":
		if args == "" {
			return Command{Name: Compact}
		}
	case "/skill":
		skill, prompt := cutWord(args)
		return Command{Name: Skill, Skill: skill, Prompt: prompt}
	}
	return Command{}
}

var (
	ErrAttachments   = errors.New("请单独发送 /new 或 /compact，移除附件、引用和标注后再试")
	ErrSkillRequired = errors.New("请输入 /skill 和 skill 名称，可在名称后补充本轮任务")
	ErrSharedContext = errors.New("请在独立对话或工作区会话中重设上下文")
)

func (c Command) Validate(hasAttachments bool) error {
	if c.ChangesContext() && hasAttachments {
		return ErrAttachments
	}
	if c.Name == Skill {
		if c.Skill == "" {
			return ErrSkillRequired
		}
		if !ValidSkillName(c.Skill) {
			return UnknownSkill(c.Skill)
		}
	}
	return nil
}

func UnknownSkill(name string) error {
	return fmt.Errorf("当前 Agent 没有名为 %q 的 skill", name)
}

func ValidSkillName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\") &&
		strings.IndexFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func SkillNames(paths []string) []string {
	var names []string
	for _, file := range paths {
		rel, ok := strings.CutPrefix(file, "skills/")
		if !ok {
			continue
		}
		name, ok := strings.CutSuffix(rel, "/SKILL.md")
		if ok && ValidSkillName(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func InstalledSkill(name string, configHomes ...string) bool {
	if !ValidSkillName(name) {
		return false
	}
	for _, home := range configHomes {
		if home != "" {
			if info, err := os.Stat(filepath.Join(home, "skills", name, "SKILL.md")); err == nil && info.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}

func WithSkill(configRoot, name, prompt string) string {
	root := path.Join(configRoot, "skills", name)
	instruction := fmt.Sprintf("本轮请使用 skill %q。先阅读 %q，按其中的指引处理本轮任务。skill 中的相对路径以 %q 为基准。",
		name, path.Join(root, "SKILL.md"), root)
	return strings.TrimSpace(instruction + "\n\n" + prompt)
}

func Scheduled(name string) string {
	if name == New {
		return "已开启新对话。下一条消息将从新的对话上下文开始。"
	}
	return "已安排压缩上下文。下一条消息将带上关键要求和近期对话。"
}
