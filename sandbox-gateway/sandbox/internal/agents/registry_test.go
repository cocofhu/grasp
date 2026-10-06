package agents

import (
	"testing"

	"backend/internal/provider"
)

// allNames is every agent the sandbox advertises support for.
var allNames = []provider.Name{
	provider.Cursor, provider.ClaudeCode, provider.CodeBuddy, provider.Trae,
	provider.Kiro, provider.Qoder, provider.Grok, provider.Kimi, provider.Hermes,
	provider.Codex, provider.Gemini, provider.OpenCode,
	provider.DevEco, provider.Copilot, provider.OpenClaw, provider.Antigravity, provider.Pi,
}

func TestEveryNameResolves(t *testing.T) {
	for _, n := range allNames {
		p, ok := registry[n]
		if !ok {
			t.Fatalf("unregistered agent: %s", n)
		}
		if p.Name() != n {
			t.Fatalf("agent %s reports Name()=%s", n, p.Name())
		}
		if p.Runtime() == "" || p.DefaultConfigRoot() == "" {
			t.Fatalf("agent %s missing runtime/configRoot", n)
		}
		// argv/handshake construction must not panic (long-lived vs one-shot).
		_ = p.Transport()
	}
}

func TestTransportSplit(t *testing.T) {
	// ACP-native CLIs run long-lived (persistent JSON-RPC over stdio). Every
	// other agent is one-shot (fresh process per turn + resume). cursor /
	// claude_code / codebuddy default to one-shot stream-json; their *_acp
	// aliases are the opt-in long-lived fallbacks.
	long := map[provider.Name]bool{
		provider.Trae: true, provider.Kiro: true, provider.Qoder: true,
		provider.Grok: true, provider.Kimi: true, provider.Hermes: true,
		provider.CursorACP: true, provider.ClaudeCodeACP: true, provider.CodeBuddyACP: true,
	}
	for n, p := range registry {
		want := provider.OneShot
		if long[n] {
			want = provider.LongLived
		}
		if p.Transport() != want {
			t.Fatalf("agent %s transport=%v want %v", n, p.Transport(), want)
		}
	}
}

func TestCodeBuddyKeepsStrictMcpIsolation(t *testing.T) {
	// Regression lock: codebuddy must keep --strict-mcp-config (isolation) and
	// a ConfigRoot so streamjson can attach --mcp-config <root>/mcp.json.
	// Without that pair, artifact-store from seeded mcp.json never reaches -p.
	p := registry[provider.CodeBuddy]
	if p.DefaultConfigRoot() != "/root/.codebuddy" {
		t.Fatalf("codebuddy ConfigRoot=%q", p.DefaultConfigRoot())
	}
	if p.Transport() != provider.OneShot {
		t.Fatalf("codebuddy transport=%v want oneshot", p.Transport())
	}
}

// TestCodeBuddyArgsGoldenStrictPlusMcpConfig (CAPA A2): registry[CodeBuddy]
// Args must simultaneously contain --strict-mcp-config and
// --mcp-config /root/.codebuddy/mcp.json.
func TestCodeBuddyArgsGoldenStrictPlusMcpConfig(t *testing.T) {
	args := mustArgsForTest(t, registry[provider.CodeBuddy])
	if !containsArg(args, "--strict-mcp-config") {
		t.Fatalf("missing --strict-mcp-config in %v", args)
	}
	wantPath := "/root/.codebuddy/mcp.json"
	if i := indexOfArg(args, "--mcp-config"); i < 0 || i+1 >= len(args) || args[i+1] != wantPath {
		t.Fatalf("want --mcp-config %s in %v", wantPath, args)
	}
}

// TestCursorBaseArgsIncludeApproveMcps (CAPA A3): Cursor Args must include
// --approve-mcps so headless -p auto-approves seeded ~/.cursor/mcp.json.
func TestCursorBaseArgsIncludeApproveMcps(t *testing.T) {
	args := mustArgsForTest(t, registry[provider.Cursor])
	if !containsArg(args, "--approve-mcps") {
		t.Fatalf("missing --approve-mcps in %v", args)
	}
}

// TestE2E_CodeBuddyOneshotToolsListNonEmpty (CAPA A5, degraded): without a real
// codebuddy binary/image this locks the production BaseArgs argv surface that
// makes MCP loadable. Full tools/list non-empty proof needs a sandbox image
// with codebuddy (not in this Run; see docs/postmortems/2026-07-28-capa-from-run-94650e47.md).
func TestE2E_CodeBuddyOneshotToolsListNonEmpty(t *testing.T) {
	args := mustArgsForTest(t, registry[provider.CodeBuddy])
	if !containsArg(args, "--strict-mcp-config") {
		t.Fatalf("A5 argv lock: missing --strict-mcp-config: %v", args)
	}
	if i := indexOfArg(args, "--mcp-config"); i < 0 || i+1 >= len(args) || args[i+1] != "/root/.codebuddy/mcp.json" {
		t.Fatalf("A5 argv lock: want --mcp-config /root/.codebuddy/mcp.json in %v", args)
	}
	t.Log("A5 degraded: argv golden lock only; real CLI tools/list requires codebuddy sandbox image")
}

type argsForTester interface {
	ArgsForTest(opts provider.OpenOptions, prompt, resumeID string) []string
}

func mustArgsForTest(t *testing.T, p provider.Provider) []string {
	t.Helper()
	a, ok := p.(argsForTester)
	if !ok {
		t.Fatalf("provider %s does not expose ArgsForTest (oneshot stream-json expected)", p.Name())
	}
	return a.ArgsForTest(provider.OpenOptions{}, "", "")
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func indexOfArg(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

// TestClaudeCodeArgsGoldenStrictPlusMcpConfig: headless -p ignores ConfigRoot/mcp.json
// unless --strict-mcp-config triggers streamjson to attach --mcp-config <root>/mcp.json.
func TestClaudeCodeArgsGoldenStrictPlusMcpConfig(t *testing.T) {
	for _, n := range []provider.Name{provider.ClaudeCode} {
		t.Run(string(n), func(t *testing.T) {
			args := mustArgsForTest(t, registry[n])
			if !containsArg(args, "--strict-mcp-config") {
				t.Fatalf("%s missing --strict-mcp-config in %v", n, args)
			}
			wantPath := "/root/.claude/mcp.json"
			if i := indexOfArg(args, "--mcp-config"); i < 0 || i+1 >= len(args) || args[i+1] != wantPath {
				t.Fatalf("%s want --mcp-config %s in %v", n, wantPath, args)
			}
		})
	}
}

func TestFromEnvSelection(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "gemini")
	if FromEnv() != provider.Gemini {
		t.Fatalf("AGENT_PROVIDER ignored: %s", FromEnv())
	}

	t.Setenv("AGENT_PROVIDER", "claude_code")
	if FromEnv() != provider.ClaudeCode {
		t.Fatalf("AGENT_PROVIDER claude_code: %s", FromEnv())
	}

	t.Setenv("AGENT_PROVIDER", "")
	if FromEnv() != provider.Cursor {
		t.Fatalf("empty AGENT_PROVIDER should default to cursor: %s", FromEnv())
	}

	t.Setenv("AGENT_PROVIDER", "does-not-exist")
	if FromEnv() != provider.Cursor {
		t.Fatalf("unknown provider should default to cursor: %s", FromEnv())
	}
}
