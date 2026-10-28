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
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n\x00") {
		return p, errors.New("OpenCode official authorization: selected API key is missing or invalid")
	}
	p.Model = strings.TrimSpace(p.Model)
	if p.Model == "auto" {
		p.Model = ""
	}
	if p.Model != "" && (!strings.HasPrefix(p.Model, "opencode/") || strings.TrimSpace(strings.TrimPrefix(p.Model, "opencode/")) == "") {
		return p, errors.New("OpenCode official authorization requires an opencode model")
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
	config := map[string]any{"$schema": "https://opencode.ai/config.json", "enabled_providers": []string{"opencode"},
		"provider": map[string]any{"opencode": map[string]any{"options": map[string]any{"apiKey": "{env:OPENCODE_API_KEY}"}}}}
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
