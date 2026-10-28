package acpx

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"backend/internal/backend/claude"
	"backend/internal/provider"
	"backend/internal/provider/modelconnection"
)

func TestAgentArgvUsesCatalogIDAndLiteralProgram(t *testing.T) {
	const foreign = "request-body-not-in-catalog"
	catalog := modelconnection.Connection{
		ConnectionID:    "connection-team-a",
		ProviderID:      "gateway",
		Protocol:        modelconnection.Responses,
		BaseURL:         "https://gateway.example/v1",
		Auth:            modelconnection.Auth{Scheme: "bearer"},
		Models:          []modelconnection.Model{{ID: "alpha"}, {ID: "beta"}},
		SelectedModelID: "alpha",
	}

	t.Run("codex args hit", func(t *testing.T) {
		prepared := prepareConnection(t, provider.Codex, catalog, "beta")
		argv := agentArgv([]string{"kimi", "acp"}, prepared, nil)
		if argv[0] != "kimi" {
			t.Fatalf("program = %q argv=%q", argv[0], argv)
		}
		if !hasArgPair(argv, "-c", "model="+strconv.Quote("beta")) {
			t.Fatalf("catalog model missing: %q", argv)
		}
		assertArgvOmits(t, argv, foreign)
	})

	t.Run("codex args miss", func(t *testing.T) {
		prepared := prepareConnection(t, provider.Codex, catalog, foreign)
		argv := agentArgv([]string{"kimi", "acp"}, prepared, nil)
		if argv[0] != "kimi" {
			t.Fatalf("program = %q argv=%q", argv[0], argv)
		}
		if !hasArgPair(argv, "-c", "model="+strconv.Quote("alpha")) {
			t.Fatalf("primary model missing: %q", argv)
		}
		assertArgvOmits(t, argv, foreign)
	})

	t.Run("claude program", func(t *testing.T) {
		claudeCatalog := catalog
		claudeCatalog.Protocol = modelconnection.Messages
		claudeCatalog.Auth.Scheme = "x-api-key"
		hit := prepareConnection(t, provider.ClaudeCodeACP, claudeCatalog, "beta")
		argv := agentArgv((claude.Backend{}).Argv(hit.Model), hit, nil)
		if argv[0] != "npx" || !containsArg(argv, "beta") {
			t.Fatalf("hit argv = %q", argv)
		}
		assertArgvOmits(t, argv, foreign)

		miss := prepareConnection(t, provider.ClaudeCodeACP, claudeCatalog, foreign)
		argv = agentArgv((claude.Backend{}).Argv(miss.Model), miss, nil)
		if argv[0] != "npx" || !containsArg(argv, "alpha") {
			t.Fatalf("miss argv = %q", argv)
		}
		assertArgvOmits(t, argv, foreign)
		for _, item := range miss.Env {
			if strings.Contains(item, foreign) {
				t.Fatalf("request leaked into env %q", item)
			}
		}
	})
}

func prepareConnection(t *testing.T, name provider.Name, c modelconnection.Connection, requested string) modelconnection.Prepared {
	t.Helper()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	env := []string{
		modelconnection.EnvConfig + "=" + string(data),
		modelconnection.EnvAPIKey + "=selected-secret-12345",
		"PATH=/usr/bin:/bin",
	}
	prepared, err := modelconnection.Prepare(name, env, requested, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(prepared.Cleanup)
	return prepared
}

func hasArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func assertArgvOmits(t *testing.T, argv []string, forbidden string) {
	t.Helper()
	for _, arg := range argv {
		if strings.Contains(arg, forbidden) {
			t.Fatalf("argv contains %q: %q", forbidden, argv)
		}
	}
}
