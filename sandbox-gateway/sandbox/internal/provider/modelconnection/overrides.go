package modelconnection

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/provider"
)

// ValidateOptions prevents custom arguments or project model definitions from
// silently selecting a service other than the Agent's resolved connection.
func (p Prepared) ValidateOptions(tool provider.Name, opts provider.OpenOptions) error {
	if p.Connection == nil {
		return nil
	}
	for i, arg := range opts.CustomArgs {
		flag, value, _ := strings.Cut(arg, "=")
		switch flag {
		case "--model", "-m", "--provider", "--model-provider", "--settings", "--setting-sources", "--config", "--api-key", "--base-url", "--profile":
			return errors.New("model connection: custom CLI arguments cannot override model routing")
		case "-c":
			if value == "" && i+1 < len(opts.CustomArgs) {
				value = opts.CustomArgs[i+1]
			}
			key, _, _ := strings.Cut(value, "=")
			if key == "model" || key == "model_provider" || key == "profile" || key == "forced_login_method" || key == "openai_base_url" || strings.HasPrefix(key, "model_providers.") || strings.HasPrefix(key, "profiles.") {
				return errors.New("model connection: custom Codex config cannot override model routing")
			}
		}
	}
	if tool != provider.CodeBuddy && tool != provider.CodeBuddyACP {
		return nil
	}
	workspace, dir, err := codeBuddyWorkspace(opts)
	if err != nil {
		return err
	}
	defer func() { _ = workspace.Close() }()
	// models.json has its own project-over-user merge contract, separate from
	// --setting-sources. Refuse conflicting project definitions without editing
	// the checkout or allowing a project key/URL to shadow a managed connection.
	models := map[string]bool{}
	for _, m := range p.Connection.Models {
		models[m.ID] = true
	}
	for ; ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, ".codebuddy", "models.json")
		data, err := workspace.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("model connection: cannot inspect project CodeBuddy model overrides")
		}
		if err == nil {
			var doc struct {
				Models []struct {
					ID string `json:"id"`
				} `json:"models"`
			}
			if json.Unmarshal(data, &doc) != nil {
				return errors.New("model connection: invalid project CodeBuddy models.json")
			}
			for _, m := range doc.Models {
				if models[m.ID] {
					return errors.New("model connection: project CodeBuddy models.json overrides a selected connection model")
				}
			}
		}
		if dir == "." {
			break
		}
	}
	return nil
}

// Keep inspection inside the same filesystem boundary as the session. Root
// operations also reject symlinks that escape it, including during a rename.
func codeBuddyWorkspace(opts provider.OpenOptions) (*os.Root, string, error) {
	invalid := errors.New("model connection: CodeBuddy working directory must be inside its filesystem root")
	cwd, err := filepath.Abs(opts.Cwd)
	if err != nil {
		return nil, "", invalid
	}
	root := opts.FSRoot
	if root == "" {
		root = cwd
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, "", invalid
	}
	dir, err := filepath.Rel(root, cwd)
	if err != nil || !filepath.IsLocal(dir) {
		return nil, "", invalid
	}
	workspace, err := os.OpenRoot(root)
	if err != nil {
		return nil, "", invalid
	}
	working, err := workspace.OpenRoot(dir)
	if err != nil {
		_ = workspace.Close()
		return nil, "", invalid
	}
	_ = working.Close()
	return workspace, dir, nil
}
