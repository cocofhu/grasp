package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ReadCodexLocalAuth reads only an explicitly opted-in auth file, never the
// user's entire CODEX_HOME, config, history or sessions. Errors omit secrets.
func ReadCodexLocalAuth() ([]byte, error) {
	p := strings.TrimSpace(os.Getenv("GRASP_CODEX_AUTH_FILE"))
	if p == "" {
		return nil, fmt.Errorf("Codex local login is not configured: set GRASP_CODEX_AUTH_FILE")
	}
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
		return nil, fmt.Errorf("Codex local auth file is missing or invalid; run codex login on the server host")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("cannot read Codex local auth file")
	}
	var auth struct {
		Key    string `json:"OPENAI_API_KEY"`
		Tokens struct {
			Access string `json:"access_token"`
		} `json:"tokens"`
	}
	if json.Unmarshal(b, &auth) != nil || (auth.Key == "" && auth.Tokens.Access == "") {
		return nil, fmt.Errorf("Codex login has no usable credentials; run codex login on the server host")
	}
	return b, nil
}

func writeCodexConfigHome(dir string, servers []MCPServerSpec) error {
	if strings.TrimSpace(os.Getenv("GRASP_CODEX_AUTH_FILE")) != "" {
		b, err := ReadCodexLocalAuth()
		if err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(dir, "auth.json"), b, 0o600); err != nil {
			return err
		}
	}
	doc := map[string]any{}
	p := filepath.Join(dir, "config.toml")
	if b, err := os.ReadFile(p); err == nil {
		if err := toml.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("invalid Codex config.toml")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	mcp, _ := doc["mcp_servers"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	for _, s := range servers {
		if s.Name == "" {
			continue
		}
		e := map[string]any{}
		if s.URL != "" {
			e["url"] = s.URL
			if len(s.Headers) > 0 {
				e["http_headers"] = s.Headers
			}
		} else if s.Command != "" {
			e["command"] = s.Command
			e["args"] = s.Args
			if len(s.Env) > 0 {
				e["env"] = s.Env
			}
		} else {
			continue
		}
		mcp[s.Name] = e
	}
	doc["mcp_servers"] = mcp
	b, err := toml.Marshal(doc)
	if err != nil {
		return err
	}
	if err = os.WriteFile(p, b, 0o600); err != nil {
		return err
	}
	// Codex reads AGENTS.md rather than Cursor's rules/ directory.
	var rules strings.Builder
	if b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")); err == nil {
		rules.Write(b)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "rules", "*.md"))
	if err != nil {
		return err
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rules.WriteString("\n\n")
		rules.Write(b)
	}
	return os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(rules.String()), 0o600)
}
