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
	// models.json has its own project-over-user merge contract, separate from
	// --setting-sources. Refuse conflicting project definitions without editing
	// the checkout or allowing a project key/URL to shadow a managed connection.
	models := map[string]bool{}
	for _, m := range p.Connection.Models {
		models[m.ID] = true
	}
	for dir := opts.Cwd; dir != ""; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, ".codebuddy", "models.json")
		data, err := os.ReadFile(path)
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
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return nil
}
