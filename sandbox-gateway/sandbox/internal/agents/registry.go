// Package agents is the single source of truth for the active agent provider.
//
// It resolves the provider selected by AGENT_PROVIDER and exposes the stable
// facade used by the handler/service
// layers (FromEnv / Current / Get / ConfigRoot / RuntimeLabel). Concrete
// providers live under internal/provider/* and are transport-specific
// (long-lived ACP, one-shot stream-json / NDJSON / plain-text, ...). Adding a
// new agent means registering one entry here — the WSP wire protocol is
// unaffected.
package agents

import (
	"log"
	"os"
	"strings"

	"backend/internal/backend"
	"backend/internal/backend/common"
	"backend/internal/provider"
	"backend/internal/provider/acpx"
	"backend/internal/provider/antigravity"
	"backend/internal/provider/codex"
	"backend/internal/provider/copilot"
	"backend/internal/provider/opencodejson"
	"backend/internal/provider/openclaw"
	"backend/internal/provider/pi"
	"backend/internal/provider/streamjson"
)

// Name re-exports provider.Name for callers.
type Name = provider.Name

// registry maps each agent name to its provider (one singleton per agent).
//
// Default transport per agent tracks each CLI's primary headless contract:
//   - stream-json (one-shot, spawn per turn + --resume): cursor / claude_code /
//     codebuddy / gemini. Fresh subprocess each turn; multi-turn continuity via
//     a resume pointer. These are the primary transports.
//   - run --format json (one-shot NDJSON): opencode / deveco.
//   - json event lines (one-shot): copilot / pi / openclaw / antigravity.
//   - JSONL exec (one-shot): codex.
//   - ACP (long-lived, JSON-RPC over stdio): kimi / hermes / kiro / qoder /
//     grok / trae — these CLIs are ACP-native, so a persistent process is the
//     right fit.
//
// The *_acp entries are opt-in fallbacks that force the long-lived stdio path
// for the stream-json CLIs (reusing the proven common.Backend logic). They are
// not built as dedicated images by default.
var registry = map[Name]provider.Provider{
	// --- stream-json family (one-shot; primary) ----------------------------
	// cursor-agent: `-p --output-format stream-json --yolo --workspace <cwd>`,
	// prompt fed raw on stdin (keeps user text off every command line).
	// --approve-mcps: headless -p must auto-approve seeded ~/.cursor/mcp.json
	// or artifact-store tools stay connected-but-not-injected.
	provider.Cursor: streamjson.New(streamjson.Config{
		AgentName:     provider.Cursor,
		Bin:           "cursor-agent",
		Runtime:       "cursor-agent-stream-json",
		ConfigRoot:    "/root/.cursor",
		PromptMode:    streamjson.PromptStdinRaw,
		BaseArgs:      []string{"--yolo", "--approve-mcps"},
		WorkspaceFlag: "--workspace",
		ResumeFlag:    "--resume",
		ModelFlag:     "--model",
		AuthEnvFn:     genericAuthEnv("CURSOR_API_KEY", "ACP_CURSOR_API_KEY"),
	}),
	// claude: `-p --output-format stream-json --input-format stream-json`,
	// prompt fed as a stream-json user envelope on stdin. Autonomous headless
	// run => bypass permissions; the interactive question tool is disabled.
	// --strict-mcp-config + streamjson auto --mcp-config: headless -p does not
	// load ConfigRoot/mcp.json by default (verified claude 2.1.241); without this
	// pair artifact-store from SANDBOX_INJECT never reaches the tool list.
	provider.ClaudeCode: streamjson.New(streamjson.Config{
		AgentName:  provider.ClaudeCode,
		Bin:        "claude",
		Runtime:    "claude-stream-json",
		ConfigRoot: "/root/.claude",
		PromptMode: streamjson.PromptStdinJSON,
		BaseArgs:   []string{"--verbose", "--strict-mcp-config", "--permission-mode", "bypassPermissions", "--disallowedTools", "AskUserQuestion"},
		ResumeFlag: "--resume",
		ModelFlag:  "--model",
		AuthEnvFn:  streamjson.ClaudeAuthEnv,
	}),
	// codebuddy: Claude-compatible stream-json fork. --strict-mcp-config
	// isolates MCP to --mcp-config only (ignores user/project mcp.json).
	// streamjson.Args auto-adds --mcp-config <ConfigRoot>/mcp.json so the
	// Approving-seeded artifact-store is not wiped to "zero MCP servers".
	provider.CodeBuddy: streamjson.New(streamjson.Config{
		AgentName:  provider.CodeBuddy,
		Bin:        "codebuddy",
		Runtime:    "codebuddy-stream-json",
		ConfigRoot: "/root/.codebuddy",
		PromptMode: streamjson.PromptStdinJSON,
		BaseArgs:   []string{"--verbose", "--strict-mcp-config", "--permission-mode", "bypassPermissions", "--disallowedTools", "AskUserQuestion"},
		ResumeFlag: "--resume",
		ModelFlag:  "--model",
		AuthEnvFn:  genericAuthEnv("CODEBUDDY_API_KEY", "ACP_CODEBUDDY_API_KEY"),
	}),
	// gemini: `-p <prompt> --output-format stream-json`, prompt in argv, `-r`
	// resume. Tracks the Gemini-family stream-json event schema.
	provider.Gemini: streamjson.New(streamjson.Config{
		AgentName:  provider.Gemini,
		Bin:        "gemini",
		Runtime:    "gemini-stream-json",
		ConfigRoot: "/root/.gemini",
		PromptMode: streamjson.PromptArg,
		ResumeFlag: "-r",
		ModelFlag:  "-m",
		AuthEnvFn:  genericAuthEnv("GEMINI_API_KEY", "ACP_GEMINI_API_KEY", "GOOGLE_API_KEY"),
	}),

	// --- run --format json family (one-shot NDJSON) ------------------------
	// opencode/deveco: `run --format json --dangerously-skip-permissions
	// <prompt>`, resume via `--session <id>`; deveco speaks the same protocol.
	provider.OpenCode: opencodejson.New(opencodejson.Config{
		AgentName:     provider.OpenCode,
		Bin:           "opencode",
		Runtime:       "opencode-json",
		ConfigRoot:    "/root/.config/opencode",
		BaseArgs:      []string{"run", "--format", "json", "--dangerously-skip-permissions"},
		WorkspaceFlag: "--dir",
		ResumeFlag:    "--session",
		ModelFlag:     "--model",
		AuthEnvFn:     genericAuthEnv("OPENCODE_API_KEY", "ACP_OPENCODE_API_KEY"),
	}),
	provider.DevEco: opencodejson.New(opencodejson.Config{
		AgentName:     provider.DevEco,
		Bin:           "deveco",
		Runtime:       "deveco-json",
		ConfigRoot:    "/root/.deveco",
		BaseArgs:      []string{"run", "--format", "json", "--dangerously-skip-permissions"},
		WorkspaceFlag: "--dir",
		ResumeFlag:    "--session",
		ModelFlag:     "--model",
		AuthEnvFn:     genericAuthEnv("DEVECO_API_KEY", "ACP_DEVECO_API_KEY"),
	}),

	// --- other one-shot CLIs (dedicated codecs) ----------------------------
	provider.Codex: codex.New(),
	// copilot: `-p <prompt> --output-format json --allow-all --no-ask-user`,
	// resume via `--resume <id>`. JSONL envelope { type: "dotted.name", data }
	// with a synthetic "result" line; parsed by the dedicated copilot codec.
	provider.Copilot: copilot.New(copilot.Config{
		AgentName:  provider.Copilot,
		Bin:        "copilot",
		Runtime:    "copilot-json",
		ConfigRoot: "/root/.copilot",
		AuthEnvFn:  genericAuthEnv("GITHUB_TOKEN", "ACP_COPILOT_API_KEY", "COPILOT_API_KEY"),
	}),
	// pi: `pi -p --mode json --session <path>`. Continuity via a session log
	// file (the resume pointer); text arrives under assistantMessageEvent.delta.
	provider.Pi: pi.New(pi.Config{
		AgentName:  provider.Pi,
		Bin:        "pi",
		Runtime:    "pi-json",
		ConfigRoot: "/root/.pi",
		AuthEnvFn:  genericAuthEnv("ANTHROPIC_API_KEY", "ACP_PI_API_KEY", "PI_API_KEY"),
	}),
	// antigravity: `agy -p <prompt> --dangerously-skip-permissions`. Plain-text
	// stdout; the conversation id (resume pointer) is recovered from a
	// --log-file the engine allocates.
	provider.Antigravity: antigravity.New(antigravity.Config{
		AgentName:  provider.Antigravity,
		Bin:        "agy",
		Runtime:    "antigravity-text",
		ConfigRoot: "/root/.antigravity",
		AuthEnvFn:  genericAuthEnv("GEMINI_API_KEY", "ANTIGRAVITY_API_KEY", "GOOGLE_API_KEY"),
	}),
	// openclaw: `openclaw agent --local --json --session-id <id> --message
	// <prompt>`. Emits a single pretty-printed JSON result document (payloads +
	// meta); parsed whole-buffer by the dedicated openclaw codec.
	provider.OpenClaw: openclaw.New(openclaw.Config{
		AgentName:  provider.OpenClaw,
		Bin:        "openclaw",
		Runtime:    "openclaw-json",
		ConfigRoot: "/root/.openclaw",
		AuthEnvFn:  genericAuthEnv("OPENCLAW_API_KEY", "ACP_OPENCLAW_API_KEY"),
	}),

	// --- ACP family (long-lived, JSON-RPC over stdio) ----------------------
	provider.Trae:   acpx.FromBackend(backend.Get(backend.Trae)),
	provider.Kimi:   acpSpec(provider.Kimi, "kimi-acp", "/root/.kimi", []string{"kimi", "acp"}, "MOONSHOT_API_KEY"),
	provider.Hermes: acpSpec(provider.Hermes, "hermes-acp", "/root/.hermes", []string{"hermes", "acp"}, "HERMES_API_KEY"),
	provider.Kiro:   acpSpec(provider.Kiro, "kiro-acp", "/root/.kiro", []string{"kiro-cli", "acp", "--trust-all-tools"}, "KIRO_API_KEY"),
	provider.Qoder:  acpSpec(provider.Qoder, "qoder-acp", "/root/.qoder", []string{"qodercli", "--yolo", "--acp"}, "QODER_PERSONAL_ACCESS_TOKEN"),
	provider.Grok:   acpSpec(provider.Grok, "grok-acp", "/root/.grok", []string{"grok", "--no-auto-update", "agent", "--always-approve", "stdio"}, "XAI_API_KEY"),

	// --- opt-in ACP fallbacks for the stream-json CLIs ---------------------
	provider.CursorACP:     acpx.FromBackend(backend.Get(backend.Cursor)),
	provider.ClaudeCodeACP: acpx.FromBackend(backend.Get(backend.ClaudeCode)),
	provider.CodeBuddyACP:  acpx.FromBackend(backend.Get(backend.CodeBuddy)),
}

// acpSpec builds a long-lived ACP provider for a CLI without a bespoke backend
// package. Model selection for ACP CLIs is negotiated in-session (session/new /
// session/set_model), so the model is not appended to argv here.
func acpSpec(name provider.Name, runtime, configRoot string, argv []string, apiKeyVar string) provider.Provider {
	return acpx.New(acpx.Spec{
		AgentName:  name,
		Runtime:    runtime,
		ConfigRoot: configRoot,
		ArgvFn: func(string) []string {
			return append([]string(nil), argv...)
		},
		AuthEnvFn: genericAuthEnv(apiKeyVar, "ACP_"+strings.ToUpper(string(name))+"_API_KEY"),
	})
}

// genericAuthEnv normalizes the first non-empty alias into nativeVar.
func genericAuthEnv(nativeVar string, aliases ...string) func(env []string) []string {
	keys := append([]string{nativeVar}, aliases...)
	return func(env []string) []string {
		return common.SetIfEmpty(env, nativeVar, common.FirstNonEmptyEnv(keys...))
	}
}

// FromEnv resolves the active provider name from AGENT_PROVIDER, defaulting to
// cursor. Unknown values are logged and ignored (cursor is used).
func FromEnv() Name {
	if v := strings.TrimSpace(os.Getenv("AGENT_PROVIDER")); v != "" {
		if _, ok := registry[Name(v)]; ok {
			return Name(v)
		}
		log.Printf("agents: 未知 AGENT_PROVIDER=%q，回退 cursor", v)
	}
	return provider.Cursor
}

// Get returns the provider for name (falls back to cursor).
func Get(n Name) provider.Provider {
	if p, ok := registry[n]; ok {
		return p
	}
	log.Printf("agents: 未知 provider=%q，回退 cursor", n)
	return registry[provider.Cursor]
}

// Current returns the provider selected by the environment.
func Current() provider.Provider { return Get(FromEnv()) }

// ConfigRoot returns the config tree root for capabilities discovery
// (CONFIG_ROOT overrides the active provider's default).
func ConfigRoot() string {
	if r := strings.TrimSpace(os.Getenv("CONFIG_ROOT")); r != "" {
		return r
	}
	return Current().DefaultConfigRoot()
}

// RuntimeLabel is capabilities.agent.runtime for the given provider.
func RuntimeLabel(n Name) string { return Get(n).Runtime() }
