package services

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func setCredentialKey(t *testing.T) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	t.Setenv(crypto.SecretsKeyEnv, base64.StdEncoding.EncodeToString(b))
}

func TestWorkspaceCredentialsCRUDAndResolution(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("Credential workspace", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	created, err := s.Create(CredentialInput{Type: "ai", Provider: "cursor", Name: "Cursor", Value: "secret-value"})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Configured || created.Masked == "secret-value" || created.Masked == "" {
		t.Fatalf("unsafe view: %+v", created)
	}
	var raw models.Credential
	if err := db.First(&raw, "id = ?", created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if raw.ValueEnc == "secret-value" || raw.ValueEnc == "" {
		t.Fatalf("plaintext/empty ciphertext: %q", raw.ValueEnc)
	}
	rows, err := s.List()
	if err != nil || len(rows) < 9 {
		t.Fatalf("list defaults: n=%d err=%v", len(rows), err)
	}
	resolved := s.ResolveEnv()
	if resolved["GRASP_CURSOR_API_KEY"] != "secret-value" {
		t.Fatalf("resolved=%v", resolved)
	}
	updated, err := s.Update(created.ID, CredentialInput{Value: "rotated"})
	if err != nil || !updated.Configured {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if s.ResolveEnv()["GRASP_CURSOR_API_KEY"] != "rotated" {
		t.Fatal("rotation not resolved")
	}
	if err := s.Clear(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ResolveEnv()["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatal("cleared credential still resolved")
	}
	if _, err := s.Update(created.ID, CredentialInput{Value: "again"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ResolveEnv()["GRASP_CURSOR_API_KEY"]; ok {
		t.Fatal("revoked credential still resolved")
	}
}

func TestWorkspaceCredentialOpenCodeMetadataResolvesRuntimeSettings(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("OpenCode metadata", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	created, err := s.Create(CredentialInput{
		Type:     "ai",
		Provider: "opencode",
		Name:     "Model API Key",
		EnvKey:   runtime.EnvGraspOpenCodeAPIKey,
		Value:    "sk-model",
		Metadata: map[string]any{
			"provider": "openrouter",
			"baseUrl":  "https://gateway.example/v1",
			"model":    "openrouter/anthropic/claude-sonnet-4-5",
			"vision":   true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Metadata["vision"] != true {
		t.Fatalf("vision metadata was not retained: %#v", created.Metadata)
	}
	if _, ok := s.ResolveEnv()[runtime.EnvGraspOpenCodeAPIKey]; ok {
		t.Fatal("model-vendor key must not be merged into the shared env")
	}
	env := s.ResolveOpenCodeCredential(created.ID)
	want := map[string]string{
		runtime.EnvGraspOpenCodeAPIKey: "sk-model",
		runtime.EnvOpenCodeProvider:    "openrouter",
		runtime.EnvOpenCodeBaseURL:     "https://gateway.example/v1",
		runtime.EnvACPBridgeModel:      "openrouter/anthropic/claude-sonnet-4-5",
		runtime.EnvOpenCodeModelVision: "1",
	}
	for key, value := range want {
		if env[key] != value {
			t.Fatalf("resolved %s=%q, want %q (env=%v)", key, env[key], value, env)
		}
	}
	if s.ResolveOpenCodeCredential("") != nil {
		t.Fatal("empty selection must not fall back")
	}
	if s.ResolveOpenCodeCredential("cred-missing") != nil {
		t.Fatal("missing selection must not fall back")
	}
	if _, legacy := env["GRASP_OPENCODE_MODEL"]; legacy {
		t.Fatalf("legacy model env key must not be emitted: %v", env)
	}
}

// Unfilled slots store no ciphertext; reading them must not report a key mismatch.
func TestWorkspaceCredentialResolveEnvSkipsEmptySlotsQuietly(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("Empty slots", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	if rows, err := s.List(); err != nil || len(rows) == 0 {
		t.Fatalf("list defaults: n=%d err=%v", len(rows), err)
	}
	logged := captureLog(t)

	if env := s.ResolveEnv(); len(env) != 0 {
		t.Fatalf("empty slots resolved: %v", env)
	}
	if out := logged(); strings.Contains(out, "undecryptable") {
		t.Fatalf("empty slots logged as undecryptable:\n%s", out)
	}
}

func TestWorkspaceCredentialRejectsReservedAndInvalidKeys(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("Reserved keys", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	for _, in := range []CredentialInput{
		{Type: "custom", Name: "artifact", EnvKey: "GRASP_ARTIFACT_TOKEN", Value: "v"},
		{Type: "custom", Name: "pm", EnvKey: "GRASP_PM_TOKEN", Value: "v"},
		{Type: "custom", Name: "root", EnvKey: "CONFIG_ROOT", Value: "v"},
		{Type: "custom", Name: "bad", EnvKey: "vars.repo_url", Value: "v"},
		{Type: "custom", Name: "bad-chars", EnvKey: "A-B", Value: "v"},
	} {
		if _, err := s.Create(in); !errors.Is(err, ErrCredentialEnvKey) {
			t.Fatalf("%s: want ErrCredentialEnvKey, got %v", in.Name, err)
		}
	}
	for _, typ := range []string{"channel", "external_mcp", "workflow"} {
		if _, err := s.Create(CredentialInput{Type: typ, Name: typ, EnvKey: "X", Value: "v"}); !errors.Is(err, ErrCredentialType) {
			t.Fatalf("%s: adapter type must not be creatable, got %v", typ, err)
		}
	}
	created, err := s.Create(CredentialInput{Type: "custom", Name: "ok", EnvKey: "MY_TOKEN", Value: "v"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(created.ID, CredentialInput{EnvKey: "GRASP_MEMORY_TOKEN"}); !errors.Is(err, ErrCredentialEnvKey) {
		t.Fatalf("update to reserved key: got %v", err)
	}
}

func TestWorkspaceCredentialEnvKeysIgnoreEmptySlots(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("Empty slots", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	if keys := s.CredentialEnvKeys(); len(keys) != 0 {
		t.Fatalf("empty default slots must not block Run env: %v", keys)
	}
	if _, err := s.SetByEnvKey(CredentialInput{Type: "git", Provider: "github", Name: "GitHub", EnvKey: "GITHUB_TOKEN", Value: "ghp"}); err != nil {
		t.Fatal(err)
	}
	keys := s.CredentialEnvKeys()
	if _, ok := keys["GITHUB_TOKEN"]; !ok || len(keys) != 1 {
		t.Fatalf("configured key should be protected: %v", keys)
	}
}

func TestOverlayWorkspaceCredentialEnvKeepsPlatformKeys(t *testing.T) {
	env := map[string]string{"GRASP_PM_TOKEN": "platform", "GITHUB_TOKEN": "agent"}
	overlayCredentialEnv(env, map[string]string{"GRASP_PM_TOKEN": "user", "GITHUB_TOKEN": "ui"})
	if env["GRASP_PM_TOKEN"] != "platform" || env["GITHUB_TOKEN"] != "ui" {
		t.Fatalf("env=%v", env)
	}
}

func TestOpenCodeSelectionInjectsOnlyTheChosenKey(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("Two model keys", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	meta := func(model string) map[string]any {
		return map[string]any{"provider": "openai", "model": model, "baseUrl": "", "vision": false}
	}
	first, err := s.Create(CredentialInput{
		Type: "ai", Provider: "opencode", Name: "Primary", Value: "sk-first", Metadata: meta("openai/gpt-a"),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Create(CredentialInput{
		Type: "ai", Provider: "opencode", Name: "Secondary", Value: "sk-second", Metadata: meta("openai/gpt-b"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CredentialInput{Type: "ai", Provider: "opencode", Name: "No model", Value: "sk"}); !errors.Is(err, ErrCredentialModel) {
		t.Fatalf("missing model: %v", err)
	}
	if _, err := s.Create(CredentialInput{
		Type: "ai", Provider: "opencode", Name: "Custom", Value: "sk",
		Metadata: map[string]any{"provider": "custom", "model": "custom/m"},
	}); !errors.Is(err, ErrCredentialBaseURL) {
		t.Fatalf("missing base url: %v", err)
	}
	if got := s.ResolveOpenCodeCredential(first.ID)[runtime.EnvGraspOpenCodeAPIKey]; got != "sk-first" {
		t.Fatalf("first=%q", got)
	}
	if got := s.ResolveOpenCodeCredential(second.ID)[runtime.EnvGraspOpenCodeAPIKey]; got != "sk-second" {
		t.Fatalf("second=%q", got)
	}
	env := map[string]string{runtime.EnvGraspOpenCodeAPIKey: "stale", runtime.EnvACPBridgeModel: "old/model"}
	runtime.ApplyOpenCodeSelection(env, s.ResolveOpenCodeCredential(second.ID))
	if env[runtime.EnvGraspOpenCodeAPIKey] != "sk-second" || env[runtime.EnvACPBridgeModel] != "openai/gpt-b" {
		t.Fatalf("applied=%v", env)
	}
	if env[runtime.EnvOpenCodeModelVision] != "" {
		t.Fatalf("vision should be cleared, env=%v", env)
	}
	stale := map[string]string{runtime.EnvGraspOpenCodeAPIKey: "keep"}
	runtime.ApplyOpenCodeSelection(stale, nil)
	if stale[runtime.EnvGraspOpenCodeAPIKey] != "keep" {
		t.Fatal("nil selection changed env")
	}
	if err := s.Clear(first.ID); err != nil {
		t.Fatal(err)
	}
	if s.ResolveOpenCodeCredential(first.ID) != nil {
		t.Fatal("cleared credential still resolved")
	}
	if got := s.ResolveOpenCodeCredential(second.ID)[runtime.EnvGraspOpenCodeAPIKey]; got != "sk-second" {
		t.Fatalf("other key changed: %q", got)
	}
	rows, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == first.ID {
			t.Fatal("extra model-vendor row still listed after clear")
		}
	}

	agents := NewAgentService(t.TempDir())
	if err := agents.Save(Agent{Name: "one", AcpBackend: AcpBackendOpenCode, OpenCodeCredentialID: second.ID}); err != nil {
		t.Fatal(err)
	}
	if err := agents.Save(Agent{Name: "two", AcpBackend: AcpBackendOpenCode, OpenCodeCredentialID: "cred-other"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(second.ID); err != nil {
		t.Fatal(err)
	}
	agents.ClearOpenCodeCredentialSelection(second.ID)
	one, _ := agents.Get("one")
	two, _ := agents.Get("two")
	if one.OpenCodeCredentialID != "" {
		t.Fatalf("selected agent still points at cleared credential: %q", one.OpenCodeCredentialID)
	}
	if two.OpenCodeCredentialID != "cred-other" {
		t.Fatalf("other selection changed: %q", two.OpenCodeCredentialID)
	}
}

func TestCodexLoginFileSlotMaskRejectAndWriteBack(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("Codex login", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	rows, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var slot CredentialView
	found := false
	for _, row := range rows {
		if row.EnvKey == envauth.EnvCodexAuthFile {
			slot = row
			found = true
		}
	}
	if !found || slot.Configured || slot.Provider != "codex" || slot.Name != "Codex" {
		t.Fatalf("codex slot=%+v found=%v", slot, found)
	}
	login := `{"auth_mode":"chatgpt","tokens":{"access_token":"at","refresh_token":"rt"}}`
	saved, err := s.Update(slot.ID, CredentialInput{Value: login})
	if err != nil {
		t.Fatal(err)
	}
	rawView, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawView), "access_token") || strings.Contains(string(rawView), login) || saved.Masked != "••••••••" {
		t.Fatalf("view leaked login file: %s", rawView)
	}
	if s.ResolveEnv()[envauth.EnvCodexAuthFile] != login {
		t.Fatal("resolve lost the login file")
	}
	var before models.Credential
	if err := db.First(&before, "id = ?", slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"sk-live", `{"auth_mode":"apikey","OPENAI_API_KEY":"sk-live"}`, "  \n"} {
		if _, err := s.Update(slot.ID, CredentialInput{Value: bad}); !errors.Is(err, runtime.ErrCodexLoginFileAPIKey) && !errors.Is(err, runtime.ErrCodexLoginFileEmpty) {
			t.Fatalf("reject %q: %v", bad, err)
		}
	}
	if _, err := s.Create(CredentialInput{Type: "ai", Provider: "codex", Name: "extra", Value: "sk-live"}); !errors.Is(err, runtime.ErrCodexLoginFileAPIKey) {
		t.Fatalf("create api key: %v", err)
	}
	var afterReject models.Credential
	if err := db.First(&afterReject, "id = ?", slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterReject.ValueEnc != before.ValueEnc || !afterReject.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("rejected paste overwrote the login file")
	}
	if err := s.WriteBackCodexLoginCredential(slot.ID, login); err != nil {
		t.Fatal(err)
	}
	var same models.Credential
	if err := db.First(&same, "id = ?", slot.ID).Error; err != nil {
		t.Fatal(err)
	}
	if same.ValueEnc != before.ValueEnc || !same.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("unchanged writeback touched the credential")
	}
	refreshed := `{"auth_mode":"chatgpt","tokens":{"access_token":"new","refresh_token":"rt2"}}`
	if err := s.WriteBackCodexLoginCredential(slot.ID, refreshed); err != nil {
		t.Fatal(err)
	}
	later := `{"auth_mode":"chatgpt","tokens":{"access_token":"later","refresh_token":"rt3"}}`
	if err := s.WriteBackCodexLoginCredential(slot.ID, later); err != nil {
		t.Fatal(err)
	}
	if got := s.ResolveEnv()[envauth.EnvCodexAuthFile]; got != later {
		t.Fatalf("last writeback=%q", got)
	}
	if err := s.WriteBackCodexLoginCredential(slot.ID, "sk-nope"); !errors.Is(err, runtime.ErrCodexLoginFileAPIKey) {
		t.Fatalf("invalid writeback: %v", err)
	}
	if got := s.ResolveEnv()[envauth.EnvCodexAuthFile]; got != later {
		t.Fatalf("invalid writeback overwrote %q", got)
	}
	listed, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(listed)
	if strings.Contains(string(blob), "later") || strings.Contains(string(blob), "refresh_token") {
		t.Fatalf("list leaked login file: %s", blob)
	}
}

func TestBuiltinCredentialShortNames(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	if _, err := NewWorkspaceService(db).CreateFixture("Short names", "", nil); err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	if aiCredentialName(AcpBackendCursor) != builtinNameCursor ||
		aiCredentialName(AcpBackendClaudeCode) != builtinNameClaudeCode ||
		aiCredentialName(AcpBackendCodex) != builtinNameCodex ||
		aiCredentialName("cursor") != "Cursor" {
		t.Fatalf("generated names cursor=%q claude=%q codex=%q", aiCredentialName("cursor"), aiCredentialName(AcpBackendClaudeCode), aiCredentialName(AcpBackendCodex))
	}

	rows, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		name   string
		envKey string
	}{
		"cred-builtin-cursor":     {builtinNameCursor, envauth.EnvCursorAPIKey},
		"cred-builtin-claude":     {builtinNameClaudeCode, envauth.EnvClaudeAPIKey},
		"cred-builtin-codebuddy":  {builtinNameCodeBuddy, envauth.EnvCodeBuddyAPIKey},
		"cred-builtin-trae":       {builtinNameTrae, envauth.EnvTraeAPIKey},
		"cred-builtin-opencode":   {builtinNameOpenCode, envauth.EnvOpenCodeAPIKey},
		"cred-builtin-codex":      {builtinNameCodex, envauth.EnvCodexAuthFile},
		"cred-builtin-github":     {builtinNameGitHub, envauth.EnvGitHubToken},
		"cred-builtin-gitlab":     {builtinNameGitLab, envauth.EnvGitLabToken},
		"cred-builtin-gitlab-url": {builtinNameGitLabURL, "GITLAB_URL"},
		"cred-builtin-ssh-key":    {builtinNameSSHKey, envauth.EnvGitSSHPrivateKey},
		"cred-builtin-ssh-hosts":  {builtinNameSSHHosts, envauth.EnvGitSSHKnownHosts},
	}
	got := map[string]CredentialView{}
	for _, row := range rows {
		if _, ok := want[row.ID]; ok {
			got[row.ID] = row
		}
	}
	if len(got) != len(want) {
		t.Fatalf("builtin slots=%d want=%d", len(got), len(want))
	}
	for id, spec := range want {
		row := got[id]
		if row.Name != spec.name || row.EnvKey != spec.envKey {
			t.Fatalf("%s name=%q env=%q want name=%q env=%q", id, row.Name, row.EnvKey, spec.name, spec.envKey)
		}
	}
}

func TestCredentialAliasIsUniqueWithinKind(t *testing.T) {
	setCredentialKey(t)
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("Alias workspace", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := NewCredentialService(db)
	if _, err := s.List(); err != nil {
		t.Fatal(err)
	}
	cursorID := "cred-builtin-cursor"
	if _, err := s.Update(cursorID, CredentialInput{Value: "builtin-key"}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Create(CredentialInput{Type: "ai", Provider: "cursor", Name: "cursor", EnvKey: envauth.EnvCursorAPIKey, Value: "dup"})
	var taken *CredentialAliasTakenError
	if !errors.As(err, &taken) || taken.Alias != "Cursor" {
		t.Fatalf("duplicate alias: %v", err)
	}
	work, err := s.Create(CredentialInput{Type: "ai", Provider: "cursor", Name: "工作号", EnvKey: envauth.EnvCursorAPIKey, Value: "work-key"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CredentialInput{Type: "git", Provider: "github", Name: "工作号", EnvKey: envauth.EnvGitHubToken, Value: "gh"}); err != nil {
		t.Fatalf("same alias on another kind: %v", err)
	}
	if _, ok := s.ResolveEnv()[envauth.EnvCursorAPIKey]; ok {
		t.Fatal("two cursor secrets were merged")
	}
	key, value, ok := s.ResolveSelectedValue(work.ID)
	if !ok || key != envauth.EnvCursorAPIKey || value != "work-key" {
		t.Fatalf("selected=%s %s %v", key, value, ok)
	}
	env := map[string]string{envauth.EnvCursorAPIKey: "builtin-key"}
	runtime.ApplyAgentCredentialChoice(env, nil, runtime.AgentCredentialChoice{
		Backend:        "cursor",
		AiCredentialID: work.ID,
	}, s.ResolveSelectedValue)
	if env[envauth.EnvCursorAPIKey] != "work-key" {
		t.Fatalf("env=%v", env)
	}
	runtime.ApplyAgentCredentialChoice(env, nil, runtime.AgentCredentialChoice{
		Backend:        "cursor",
		AiCredentialID: "gone",
	}, s.ResolveSelectedValue)
	if _, ok := env[envauth.EnvCursorAPIKey]; ok {
		t.Fatal("cleared selection fell back to the other cursor key")
	}

	if err := s.Clear(work.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ResolveEnv()[envauth.EnvCursorAPIKey]; ok {
		t.Fatal("clearing the extra cursor restored the builtin secret")
	}
	stale := map[string]string{envauth.EnvCursorAPIKey: "builtin-key"}
	runtime.ApplyAgentCredentialChoice(stale, s.ResolveEnv(), runtime.AgentCredentialChoice{
		Backend: "cursor",
	}, s.ResolveSelectedValue)
	if _, ok := stale[envauth.EnvCursorAPIKey]; ok {
		t.Fatal("empty selection kept the builtin key after the extra row was cleared")
	}
}

func TestCredentialAliasLookupFailureIsNotAvailable(t *testing.T) {
	db := newTestDB(t)
	s := NewCredentialService(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	alias, taken, err := s.credentialAliasTaken(envauth.EnvCursorAPIKey, "Cursor", "")
	if err == nil || taken || alias != "" {
		t.Fatalf("alias=%q taken=%v err=%v", alias, taken, err)
	}
}

func TestWorkspaceCredentialRequiresMasterKey(t *testing.T) {
	db := newTestDB(t)
	_, err := NewWorkspaceService(db).CreateFixture("No key", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(crypto.SecretsKeyEnv, "")
	if _, err := NewCredentialService(db).Create(CredentialInput{Type: "custom", Name: "x", EnvKey: "X", Value: "v"}); !errors.Is(err, crypto.ErrNoSecretsKey) {
		t.Fatalf("expected missing key, got %v", err)
	}
}
