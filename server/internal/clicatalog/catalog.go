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
	// Aliases are exact saved model IDs still handled by this CLI adapter.
	Aliases []string `json:"aliases,omitempty"`
	// Context is the context window in thousands of tokens; 0 means unknown.
	Context     int  `json:"context,omitempty"`
	Recommended bool `json:"recommended,omitempty"`
	// Efforts lists the reasoning levels the CLI accepts for this model;
	// empty means the CLI exposes none.
	Efforts []string `json:"efforts,omitempty"`
	// DefaultEffort is the level the tool runs the model at when none is
	// chosen; empty means it does not say.
	DefaultEffort string `json:"defaultEffort,omitempty"`
	// ServiceTiers lists the speed tiers beyond standard the CLI accepts for
	// this model, independent of reasoning; empty means standard only.
	ServiceTiers []string `json:"serviceTiers,omitempty"`
}

var (
	visionReasoning = []string{TagVision, TagReasoning}
	reasoningOnly   = []string{TagReasoning}
	visionFast      = []string{TagVision, TagFast}
	fastOnly        = []string{TagFast}

	codexEfforts     = []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	codexLunaEfforts = []string{"low", "medium", "high", "xhigh", "max"}
	codexFastTiers   = []string{"fast"}
	cursorFastTiers  = []string{"fast"}
	kimiEfforts      = []string{"low", "high", "max"}
)

// Cursor lists the base models of Cursor.models.list() (@cursor/sdk 1.4.2) in
// its order, without the default router. The effort and speed tier are chosen
// separately; DefaultEffort is the level of Cursor's default variant. Auto
// stays the empty choice, not a row.
var catalog = map[string][]Model{
	"cursor": {
		{ID: "grok-4.7", Name: "Grok 4.7", Vendor: "xai", Context: 500, Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "grok-4.6", Name: "Grok 4.6", Vendor: "xai", Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "composer-2.5", Name: "Composer 2.5", Vendor: "cursor", ServiceTiers: cursorFastTiers},
		{ID: "claude-opus-5-5", Name: "Claude Opus 5.5", Vendor: "anthropic", Context: 1000, Recommended: true, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "medium", ServiceTiers: cursorFastTiers},
		{ID: "claude-opus-5", Name: "Claude Opus 5", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "claude-opus-4-8", Name: "Claude Opus 4.8", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "gemini-4-neon", Name: "Gemini 4 Neon", Vendor: "google", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high"},
		{ID: "gemini-4-carbon", Name: "Gemini 4 Carbon", Vendor: "google", Context: 1000, Efforts: []string{"low", "medium", "high"}, DefaultEffort: "medium"},
		{ID: "gemini-4-hydrogen", Name: "Gemini 4 Hydrogen", Vendor: "google", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high"},
		{ID: "gemini-4-argon", Name: "Gemini 4 Argon", Vendor: "google", Context: 1000, Efforts: []string{"low", "medium", "high"}, DefaultEffort: "medium"},
		{ID: "muse-wonder-1.0", Name: "Muse Wonder", Vendor: "muse", Context: 1000, Efforts: []string{"minimal", "low", "medium", "high", "xhigh", "max"}, DefaultEffort: "xhigh"},
		{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", Vendor: "openai", Context: 1000, Recommended: true, Efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}, DefaultEffort: "medium", ServiceTiers: cursorFastTiers},
		{ID: "gpt-5.5", Name: "GPT-5.5", Vendor: "openai", Context: 1000, Efforts: []string{"none", "low", "medium", "high", "xhigh"}, DefaultEffort: "medium", ServiceTiers: cursorFastTiers},
		{ID: "claude-fable-5-2", Name: "Claude Fable 5.2", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "claude-fable-5-1", Name: "Claude Fable 5.1", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
		{ID: "claude-fable-5", Name: "Claude Fable 5", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
		{ID: "grok-4.5", Name: "Grok 4.5", Vendor: "xai", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Vendor: "google", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "high"},
		{ID: "muse-spark-1.3", Name: "Muse Spark 1.3", Vendor: "muse", Context: 1000, Efforts: []string{"minimal", "low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
		{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", Vendor: "openai", Context: 1000, Efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}, DefaultEffort: "medium", ServiceTiers: cursorFastTiers},
		{ID: "claude-sonnet-5-5", Name: "Claude Sonnet 5.5", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
		{ID: "claude-sonnet-5", Name: "Claude Sonnet 5", Vendor: "anthropic", Context: 1000, Recommended: true, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
		{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "max"}, DefaultEffort: "medium"},
		{ID: "composer-2", Name: "Composer 2", Vendor: "cursor", ServiceTiers: cursorFastTiers},
		{ID: "gpt-5.3-codex", Name: "Codex 5.3", Vendor: "openai", Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "claude-opus-4-7", Name: "Claude Opus 4.7", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "xhigh", ServiceTiers: cursorFastTiers},
		{ID: "gpt-5.4", Name: "GPT-5.4", Vendor: "openai", Context: 1000, Efforts: []string{"none", "low", "medium", "high", "xhigh"}, DefaultEffort: "medium", ServiceTiers: cursorFastTiers},
		{ID: "claude-opus-4-6", Name: "Claude Opus 4.6", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "max"}, DefaultEffort: "high"},
		{ID: "claude-opus-4-5", Name: "Claude Opus 4.5", Vendor: "anthropic"},
		{ID: "gpt-5.2", Name: "GPT-5.2", Vendor: "openai", Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "high", ServiceTiers: cursorFastTiers},
		{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", Vendor: "openai", Context: 1000, Efforts: []string{"none", "low", "medium", "high", "xhigh", "max"}, DefaultEffort: "medium", ServiceTiers: cursorFastTiers},
		{ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro", Vendor: "google"},
		{ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini", Vendor: "openai", Efforts: []string{"none", "low", "medium", "high", "xhigh"}, DefaultEffort: "medium"},
		{ID: "gpt-5.4-nano", Name: "GPT-5.4 Nano", Vendor: "openai", Efforts: []string{"none", "low", "medium", "high", "xhigh"}, DefaultEffort: "medium"},
		{ID: "claude-haiku-5-5", Name: "Claude Haiku 5.5", Vendor: "anthropic", Context: 1000, Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "high"},
		{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", Vendor: "anthropic"},
		{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Vendor: "anthropic", Context: 200},
		{ID: "gpt-5.1", Name: "GPT-5.1", Vendor: "openai", Efforts: []string{"low", "medium", "high"}, DefaultEffort: "medium"},
		{ID: "gemini-3-flash", Name: "Gemini 3 Flash", Vendor: "google"},
		{ID: "gemini-3.5-flash", Name: "Gemini 3.5 Flash", Vendor: "google"},
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", Vendor: "anthropic", Context: 200},
		{ID: "gpt-5-mini", Name: "GPT-5 Mini", Vendor: "openai"},
		{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Vendor: "google"},
		{ID: "kimi-k3", Name: "Kimi K3", Vendor: "moonshot", Efforts: []string{"low", "high", "max"}, DefaultEffort: "max"},
		{ID: "kimi-k2.7-code", Name: "Kimi K2.7 Code", Vendor: "moonshot"},
		{ID: "glm-5.2", Name: "GLM 5.2", Vendor: "zhipu", Efforts: []string{"high", "max"}, DefaultEffort: "high"},
		{ID: "glm-5p3", Name: "GLM 5.3", Vendor: "zhipu", Efforts: []string{"low", "high", "max"}, DefaultEffort: "max"},
		{ID: "glm-5p3-flash", Name: "GLM 5.3 Flash", Vendor: "zhipu", Efforts: []string{"low", "high", "max"}, DefaultEffort: "max"},
		{ID: "deepseek-v41-flash-1017", Name: "DeepSeek V4.1 Flash 1017", Vendor: "deepseek", Efforts: []string{"none", "low", "medium", "high", "max"}, DefaultEffort: "max"},
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
		{ID: "gpt-6.2-sol", Name: "GPT-6.2 Sol", Vendor: "openai", Tags: visionReasoning, Context: 500, Recommended: true, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-6.2-astra", Name: "GPT-6.2 Astra", Vendor: "openai", Tags: visionReasoning, Context: 272, Recommended: true, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-6.1-terra", Name: "GPT-6.1 Terra", Vendor: "openai", Tags: visionReasoning, Context: 500, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-6.1-sol", Name: "GPT-6.1 Sol", Vendor: "openai", Tags: visionReasoning, Context: 500, Recommended: true, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-6-astra", Name: "GPT-6 Astra", Vendor: "openai", Tags: visionReasoning, Context: 272, Recommended: true, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-6-sol", Name: "GPT-6 Sol", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-6-luna", Name: "GPT-6 Luna", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexLunaEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-5.6-sol", Name: "GPT-5.6 Sol", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-5.6-terra", Name: "GPT-5.6 Terra", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexEfforts, ServiceTiers: codexFastTiers},
		{ID: "gpt-5.6-luna", Name: "GPT-5.6 Luna", Vendor: "openai", Tags: visionReasoning, Context: 272, Efforts: codexLunaEfforts, ServiceTiers: codexFastTiers},
	},
	"codebuddy": {
		{ID: "claude-4.5-sonnet", Name: "Claude Sonnet 4.5", Vendor: "anthropic", Tags: visionReasoning, Context: 200, Recommended: true},
		{ID: "gpt-5", Name: "GPT-5", Vendor: "openai", Tags: visionReasoning, Context: 400, Recommended: true},
		{ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Vendor: "google", Tags: visionReasoning, Context: 1000},
		{ID: "deepseek-v3.1", Name: "DeepSeek V3.1", Vendor: "deepseek", Tags: reasoningOnly, Context: 128},
		{ID: "hunyuan-turbos", Name: "Hunyuan TurboS", Vendor: "tencent", Tags: fastOnly, Context: 128},
	},
	"deepseek_harness": {
		{ID: "deepseek-flash", Name: "DeepSeek Flash", Vendor: "deepseek", Recommended: true},
		{ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro", Vendor: "deepseek"},
	},
	"kimi_code": {
		{ID: "k3.1-pro", Name: "Kimi K3.1 Pro", Vendor: "kimi", Tags: visionReasoning, Aliases: []string{"kimi-k3.1-pro"}, Context: 1000, Recommended: true, Efforts: kimiEfforts},
		{ID: "k3.1-pro-highspeed", Name: "Kimi K3.1 Pro Highspeed", Vendor: "kimi", Tags: visionReasoning, Aliases: []string{"kimi-k3.1-pro-highspeed"}, Context: 1000, Efforts: kimiEfforts},
		{ID: "k3", Name: "Kimi K3", Vendor: "kimi", Tags: visionReasoning, Aliases: []string{"kimi-k3"}, Context: 1000, Efforts: kimiEfforts},
		{ID: "k3-256k", Name: "Kimi K3 256K", Vendor: "kimi", Tags: visionReasoning, Context: 256, Efforts: kimiEfforts},
		{ID: "kimi-for-coding", Name: "Kimi K2.8 Preview", Vendor: "kimi", Tags: visionReasoning, Context: 1000, Efforts: kimiEfforts},
		{ID: "kimi-for-coding-highspeed", Name: "Kimi K2.7 Code Highspeed", Vendor: "kimi", Tags: visionReasoning, Context: 256},
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
