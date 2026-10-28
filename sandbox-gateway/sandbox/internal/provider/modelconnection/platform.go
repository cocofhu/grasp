package modelconnection

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/backend/common"
)

// OpenCode's native catalog owns each official model's SDK and endpoint. Keep
// those definitions intact while excluding saved/project provider overrides;
// an inline allowlist alone would deep-merge their base URLs and model routes.
func prepareOpenCodePlatform(p Prepared, root string) (Prepared, error) {
	p.Platform = true
	key := common.GetEnvValue(p.Env, "OPENCODE_API_KEY")
	if key == "" {
		key = common.GetEnvValue(p.Env, "GRASP_OPENCODE_API_KEY")
	}
	localAuth := strings.TrimSpace(common.GetEnvValue(p.Env, "GRASP_LOCAL_AUTH")) == "1"
	if !localAuth && (strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00")) {
		return p, errors.New("OpenCode official authorization: selected API key is missing or invalid")
	}
	p.Model = strings.TrimSpace(p.Model)
	if p.Model == "auto" {
		p.Model = ""
	}
	configured, err := configuredOfficialModelIDs(p.Env)
	if err != nil {
		return p, err
	}
	providerID, err := officialProviderID(p.Model, configured)
	if err != nil {
		return p, err
	}
	if configured := strings.TrimSpace(common.GetEnvValue(p.Env, "CONFIG_ROOT")); configured != "" {
		root = configured
	}
	assets, cleanup, err := isolatedConfig(root)
	if err != nil {
		return p, err
	}
	xdg, err := os.MkdirTemp("", "grasp-opencode-")
	if err != nil {
		cleanup()
		return p, errors.New("OpenCode official authorization: cannot isolate configuration")
	}
	p.Cleanup = func() { cleanup(); _ = os.RemoveAll(xdg) }
	configDir := filepath.Join(xdg, "opencode")
	if err := os.Symlink(assets, configDir); err != nil {
		p.Cleanup()
		return p, errors.New("OpenCode official authorization: cannot preserve configuration assets")
	}
	// Native auth and Console accounts share the data store with sessions. Use a
	// stable official-mode namespace, keeping its history across Open calls while
	// excluding credentials and remote configuration from an earlier API account.
	storage := filepath.Join(root, ".grasp-official")
	for _, dir := range []string{filepath.Join(storage, "data"), filepath.Join(storage, "state"), filepath.Join(xdg, "managed")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			p.Cleanup()
			return p, errors.New("OpenCode official authorization: cannot prepare session storage")
		}
	}
	if localAuth && strings.TrimSpace(key) == "" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			hostAuth := filepath.Join(home, ".local", "share", "opencode", "auth.json")
			if _, statErr := os.Stat(hostAuth); statErr == nil {
				dstDir := filepath.Join(storage, "data", "opencode")
				_ = os.MkdirAll(dstDir, 0o700)
				dstAuth := filepath.Join(dstDir, "auth.json")
				_ = os.Remove(dstAuth)
				_ = os.Symlink(hostAuth, dstAuth)
			}
		}
	}
	// One native provider only. The key stays OPENCODE_API_KEY; do not write the
	// provider's base URL or model SDK.
	config := map[string]any{"$schema": "https://opencode.ai/config.json", "enabled_providers": []string{providerID}}
	if strings.TrimSpace(key) != "" {
		config["provider"] = map[string]any{providerID: map[string]any{"options": map[string]any{"apiKey": "{env:OPENCODE_API_KEY}"}}}
	}
	if p.Model != "" {
		config["model"], config["small_model"] = p.Model, p.Model
	}
	// Preserve managed MCP servers and instruction paths, never provider/model
	// definitions, account tokens, plugins, or other credential routing settings.
	preserveOpenCodeManagedConfig(config, root)
	data, err := json.Marshal(config)
	if err != nil {
		p.Cleanup()
		return p, errors.New("OpenCode official authorization: cannot encode configuration")
	}
	p.Env = cleanEnv(p.Env)
	for name, value := range map[string]string{"OPENCODE_API_KEY": key, "XDG_CONFIG_HOME": xdg,
		"XDG_DATA_HOME": filepath.Join(storage, "data"), "XDG_STATE_HOME": filepath.Join(storage, "state"),
		// These native discovery overrides are verified in the gated CLI source.
		// HOME stays intact for Git/SSH and commands run by the agent.
		"OPENCODE_TEST_HOME": xdg, "OPENCODE_TEST_MANAGED_CONFIG_DIR": filepath.Join(xdg, "managed"),
		"OPENCODE_CONFIG_DIR": configDir, "OPENCODE_DISABLE_PROJECT_CONFIG": "true", "OPENCODE_CONFIG_CONTENT": string(data)} {
		p.Env = common.UpsertEnv(p.Env, name, value)
	}
	return p, nil
}

// configuredOfficialModelIDs reads the allow-list already injected for the run.
// An empty primary is skipped so auxiliary OpenCode Go models can still select
// that family. Invalid JSON fails closed instead of enabling the wrong provider.
func configuredOfficialModelIDs(env []string) ([]string, error) {
	raw := strings.TrimSpace(common.GetEnvValue(env, "GRASP_CONFIGURED_MODELS"))
	if raw == "" {
		return nil, nil
	}
	var items []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, errors.New("OpenCode official authorization: configured models are invalid")
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id := strings.TrimSpace(item.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// officialProviderID enables opencode when nothing is selected. Otherwise every
// non-empty id must share one family. opencode-go/ is matched before opencode/.
func officialProviderID(session string, configured []string) (string, error) {
	ids := make([]string, 0, 1+len(configured))
	if session != "" {
		ids = append(ids, session)
	}
	ids = append(ids, configured...)
	if len(ids) == 0 {
		return "opencode", nil
	}
	family := ""
	for _, id := range ids {
		got, ok := officialModelFamily(id)
		if !ok {
			return "", errors.New("OpenCode official authorization requires an opencode or opencode-go model")
		}
		if family == "" {
			family = got
			continue
		}
		if family != got {
			return "", errors.New("OpenCode official authorization requires one model family")
		}
	}
	return family, nil
}

func officialModelFamily(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if hash := strings.LastIndex(id, "#"); hash >= 0 {
		id = strings.TrimSpace(id[:hash])
	}
	prefix := ""
	switch {
	case strings.HasPrefix(id, "opencode-go/"):
		prefix = "opencode-go/"
	case strings.HasPrefix(id, "opencode/"):
		prefix = "opencode/"
	default:
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(id, prefix))
	if rest == "" || strings.Contains(rest, "/") {
		return "", false
	}
	return strings.TrimSuffix(prefix, "/"), true
}
