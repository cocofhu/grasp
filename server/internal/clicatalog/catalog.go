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
	// Efforts lists the reasoning levels the CLI accepts for this model
	// (Codex's model_reasoning_effort); empty means the CLI exposes none.
	Efforts []string `json:"efforts,omitempty"`
}

var (
	visionReasoning = []string{TagVision, TagReasoning}
	reasoningOnly   = []string{TagReasoning}
	visionFast      = []string{TagVision, TagFast}
	fastOnly        = []string{TagFast}
	reasoningFast   = []string{TagReasoning, TagFast}

	codexEfforts     = []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	codexLunaEfforts = []string{"low", "medium", "high", "xhigh", "max"}
)

// Cursor bakes the reasoning level into the id; each entry is the model's
// default variant, as listed by `cursor-agent models`.
var catalog = map[string][]Model{
	"cursor": {
		{ID: "claude-opus-5-5-medium", Name: "Claude Opus 5.5", Vendor: "anthropic", Tags: visionReasoning, Context: 1000, Recommended: true},
		{ID: "gpt-5.6-sol-medium", Name: "GPT-5.6 Sol", Vendor: "openai", Tags: visionReasoning, Context: 1000, Recommended: true},
		{ID: "claude-sonnet-5-high", Name: "Claude Sonnet 5", Vendor: "anthropic", Tags: visionReasoning, Context: 1000, Recommended: true},
		{ID: "claude-fable-5-1-high", Name: "Claude Fable 5.1", Vendor: "anthropic", Tags: visionReasoning, Context: 1000},
		{ID: "gpt-5.6-terra-medium", Name: "GPT-5.6 Terra", Vendor: "openai", Tags: visionReasoning, Context: 1000},
		{ID: "gpt-5.6-luna-medium", Name: "GPT-5.6 Luna", Vendor: "openai", Tags: visionReasoning, Context: 1000},
		{ID: "gpt-5.3-codex", Name: "Codex 5.3", Vendor: "openai", Tags: reasoningOnly},
		{ID: "gemini-4-argon-medium", Name: "Gemini 4 Argon", Vendor: "google", Tags: visionReasoning},
		{ID: "grok-4.7-medium", Name: "Grok 4.7", Vendor: "xai", Tags: reasoningOnly},
		{ID: "composer-2.5", Name: "Composer 2.5", Vendor: "cursor", Tags: reasoningFast},
		{ID: "kimi-k3-max", Name: "Kimi K3", Vendor: "moonshot", Tags: reasoningOnly},
	},
	"claude_code": {
		{ID: "sonnet", Name: "Claude Sonnet (latest)", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "opus", Name: "Claude Opus (latest)", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "haiku", Name: "Claude Haiku (latest)", Vendor: "anthropic", Tags: visionFast, Context: 200},
		{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Vendor: "anthropic", Tags: visionReasoning, Context: 200},
		{ID: "claude-opus-4-1", Name: "Claude Opus 4.1", Vendor: "anthropic", Tags: visionReasoning, Context: 200},
	},
	// The models Codex lists in its picker (models_cache.json, visibility "list").
	"codex": {
		{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", Vendor: "openai", Tags: visionReasoning, Context: 500, Recommended: true, Efforts: codexEfforts},
		{ID: "gpt-6-astra", Name: "GPT-6 Astra", Vendor: "openai", Tags: visionReasoning, Context: 272, Recommended: true, Efforts: codexEfforts},
		{ID: "gpt-6-sol", Name: "GPT-6 Sol", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexEfforts},
		{ID: "gpt-6-luna", Name: "GPT-6 Luna", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexLunaEfforts},
		{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexEfforts},
		{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexEfforts},
		{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexLunaEfforts},
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
