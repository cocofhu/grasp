package sandbox

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// writeDeepSeekInstructions adapts the completed overlay into dsh's global
// instructions. dsh loads AGENTS.md and skills directly, but does not load the
// Cursor-style rules directory. Only explicitly always-on rules can become
// global instructions; glob/description rules must retain their narrower scope.
func writeDeepSeekInstructions(dir string) error {
	path := filepath.Join(dir, "AGENTS.md")
	agent, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read DeepSeek Harness instructions: %w", err)
	}
	parts := []string{}
	if body := strings.TrimSpace(string(agent)); body != "" {
		parts = append(parts, body)
	}
	rules := filepath.Join(dir, "rules")
	if err := filepath.WalkDir(rules, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (filepath.Ext(path) != ".md" && filepath.Ext(path) != ".mdc") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body, always, err := alwaysApplyRule(data)
		if err != nil {
			return fmt.Errorf("decode rule %s: %w", entry.Name(), err)
		}
		if always && strings.TrimSpace(body) != "" {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			parts = append(parts, "# Rule: "+filepath.ToSlash(rel)+"\n\n"+strings.TrimSpace(body))
		}
		return nil
	}); err != nil {
		return fmt.Errorf("build DeepSeek Harness instructions: %w", err)
	}
	return os.WriteFile(path, []byte(strings.Join(parts, "\n\n")+"\n"), 0o644)
}

func alwaysApplyRule(data []byte) (body string, always bool, err error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", false, nil
	}
	frontmatter, body, found := strings.Cut(text[len("---\n"):], "\n---\n")
	if !found {
		return "", false, nil
	}
	var meta struct {
		AlwaysApply bool `yaml:"alwaysApply"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return "", false, err
	}
	return body, meta.AlwaysApply, nil
}
