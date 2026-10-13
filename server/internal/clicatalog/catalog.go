// Package clicatalog lists the models each coding CLI backend can be pinned to.
//
// The CLIs pick a model from ACP_BRIDGE_MODEL, which the sandbox passes on as
// `--model`. None of them can be asked for a catalog without a signed-in
// sandbox, so the pickers read this shipped shortlist instead. It is a
// convenience, never a gate: an id absent from the list still reaches the CLI.
package clicatalog

import "strings"

// Capability tags a model may carry.
const (
	TagVision    = "vision"
	TagReasoning = "reasoning"
	TagFast      = "fast"
)

// Model is one pickable entry. ID is passed to the CLI verbatim.
type Model struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Vendor string   `json:"vendor"`
	Tags   []string `json:"tags,omitempty"`
	// Context is the context window in thousands of tokens; 0 means unknown.
	Context     int  `json:"context,omitempty"`
	Recommended bool `json:"recommended,omitempty"`
}

var (
	visionReasoning = []string{TagVision, TagReasoning}
	reasoningOnly   = []string{TagReasoning}
	visionFast      = []string{TagVision, TagFast}
	fastOnly        = []string{TagFast}
	visionOnly      = []string{TagVision}
)

var catalog = map[string][]Model{
	"cursor": {
		{ID: "claude-4.6-opus-max", Name: "Claude Opus 4.6 Max", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "claude-4.5-sonnet", Name: "Claude Sonnet 4.5", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "gpt-5", Name: "GPT-5", Vendor: "openai", Tags: visionReasoning, Context: 400, Recommended: true},
		{ID: "gpt-5-codex", Name: "GPT-5 Codex", Vendor: "openai", Tags: reasoningOnly, Context: 400},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Vendor: "google", Tags: visionReasoning, Context: 1000},
		{ID: "grok-4.5-high-fast", Name: "Grok 4.5 Fast", Vendor: "xai", Tags: []string{TagReasoning, TagFast}, Context: 256},
	},
	"claude_code": {
		{ID: "sonnet", Name: "Claude Sonnet (latest)", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "opus", Name: "Claude Opus (latest)", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "haiku", Name: "Claude Haiku (latest)", Vendor: "anthropic", Tags: visionFast, Context: 200},
		{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Vendor: "anthropic", Tags: visionReasoning, Context: 200},
		{ID: "claude-opus-4-1", Name: "Claude Opus 4.1", Vendor: "anthropic", Tags: visionReasoning, Context: 200},
	},
	"codex": {
		{ID: "gpt-5-codex", Name: "GPT-5 Codex", Vendor: "openai", Tags: reasoningOnly, Context: 400, Recommended: true},
		{ID: "gpt-5", Name: "GPT-5", Vendor: "openai", Tags: visionReasoning, Context: 400, Recommended: true},
		{ID: "gpt-5-mini", Name: "GPT-5 mini", Vendor: "openai", Tags: []string{TagVision, TagReasoning, TagFast}, Context: 400},
		{ID: "o3", Name: "o3", Vendor: "openai", Tags: visionReasoning, Context: 200},
		{ID: "gpt-4.1", Name: "GPT-4.1", Vendor: "openai", Tags: visionOnly, Context: 1000},
	},
	"codebuddy": {
		{ID: "claude-4.5-sonnet", Name: "Claude Sonnet 4.5", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "gpt-5", Name: "GPT-5", Vendor: "openai", Tags: visionReasoning, Context: 400, Recommended: true},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Vendor: "google", Tags: visionReasoning, Context: 1000},
		{ID: "deepseek-v3.1", Name: "DeepSeek V3.1", Vendor: "deepseek", Tags: reasoningOnly, Context: 128},
		{ID: "hunyuan-turbos", Name: "Hunyuan TurboS", Vendor: "tencent", Tags: fastOnly, Context: 128},
	},
	"trae": {
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "gpt-5", Name: "GPT-5", Vendor: "openai", Tags: visionReasoning, Context: 400, Recommended: true},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Vendor: "google", Tags: visionReasoning, Context: 1000},
		{ID: "kimi-k2", Name: "Kimi K2", Vendor: "moonshot", Tags: fastOnly, Context: 128},
		{ID: "doubao-seed-code", Name: "Doubao Seed Code", Vendor: "bytedance", Tags: visionFast, Context: 256},
	},
}

// Models answers the shortlist for a backend. Unknown backends, and OpenCode,
// whose models come from the models.dev catalog, get an empty list.
func Models(backend string) []Model {
	list := catalog[strings.ToLower(strings.TrimSpace(backend))]
	out := make([]Model, len(list))
	copy(out, list)
	return out
}
