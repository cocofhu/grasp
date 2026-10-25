package modelconnection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/provider"
)

func TestClaudeVersionFailsClosed(t *testing.T) {
	for text, want := range map[string]bool{"2.1.256 (Claude Code)": false, "2.1.257 (Claude Code)": true, "2.1.257-beta": false, "2.2.0": true, "3.0.0": true, "1.9.999": false, "unknown": false} {
		if supportedClaudeVersion(text) != want {
			t.Errorf("version %q = %v, want %v", text, !want, want)
		}
	}
	dir := t.TempDir()
	for _, test := range []struct {
		name, output string
		valid        bool
	}{{"old", "2.1.256", false}, {"new", "2.1.257 (Claude Code)", true}, {"invalid", "version unknown", false}} {
		binary := filepath.Join(dir, test.name)
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\n' '"+test.output+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			err := CheckVersion(context.Background(), provider.ClaudeCode, binary, os.Environ())
			if (err == nil) != test.valid {
				t.Fatalf("version gate %s: %v", test.name, err)
			}
		}
	}
	if err := CheckVersion(context.Background(), provider.ClaudeCode, filepath.Join(dir, "missing"), nil); err == nil {
		t.Fatal("missing CLI accepted")
	}
	if err := CheckVersion(context.Background(), provider.CodeBuddy, "missing", nil); err != nil {
		t.Fatal("Claude gate changed another tool")
	}
}

func TestConnectionCatalogKeepsRawModelIDs(t *testing.T) {
	c := connection(Chat)
	models, selected, err := Catalog(provider.OpenCode, environ(t, c))
	if err != nil || !selected || len(models) != 2 || models[0].ID != c.SelectedModelID || !models[0].IsDefault {
		t.Fatalf("wrong catalog: %+v %v", models, err)
	}
	if _, selected, err := Catalog(provider.OpenCode, nil); selected || err != nil {
		t.Fatal("platform catalog changed")
	}
	if _, selected, err := Catalog(provider.OpenCode, []string{EnvConfig + "={bad"}); !selected || err == nil {
		t.Fatal("invalid connection catalog must not fall back to platform models")
	}
}

func TestConnectionRejectsRoutingOverridesAndProjectModelCollisions(t *testing.T) {
	c := connection(Chat)
	p := prepare(t, provider.CodeBuddy, c)
	for _, args := range [][]string{{"--model", "other"}, {"--settings={}"}, {"-c", "model_provider=other"}, {"-c=model=other"}} {
		if err := p.ValidateOptions(provider.CodeBuddy, provider.OpenOptions{CustomArgs: args}); err == nil {
			t.Fatalf("routing override accepted: %v", args)
		}
	}
	if err := p.ValidateOptions(provider.CodeBuddy, provider.OpenOptions{CustomArgs: []string{"-c", "model_reasoning_effort=high"}}); err != nil {
		t.Fatal("unrelated setting rejected")
	}
	root := t.TempDir()
	dir := filepath.Join(root, ".codebuddy")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "models.json")
	content := `{"models":[{"id":"vendor/model/with/slashes","url":"https://old.example/v1/chat/completions","apiKey":"do-not-copy"}]}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateOptions(provider.CodeBuddy, provider.OpenOptions{Cwd: root}); err == nil || strings.Contains(err.Error(), "do-not-copy") {
		t.Fatal("project model collision must fail without revealing secret")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatal("project config was modified")
	}
	if err := (Prepared{}).ValidateOptions(provider.CodeBuddy, provider.OpenOptions{Cwd: root, CustomArgs: []string{"--model", "native"}}); err != nil {
		t.Fatal("platform options changed")
	}
}
