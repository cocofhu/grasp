package clicatalog

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestEveryBackendHasAUsableList(t *testing.T) {
	for _, b := range []string{"cursor", "claude_code", "codex", "codebuddy", "trae", "deepseek_harness", "kimi_code"} {
		list := Models(b)
		if len(list) == 0 {
			t.Fatalf("%s: empty list", b)
		}
		seen := map[string]bool{}
		recommended := 0
		for _, m := range list {
			if m.ID == "" || m.Name == "" || m.Vendor == "" {
				t.Fatalf("%s: incomplete entry %+v", b, m)
			}
			if m.ID == "auto" {
				t.Fatalf("%s: auto is the empty choice, not a list entry", b)
			}
			if seen[m.ID] {
				t.Fatalf("%s: duplicate id %q", b, m.ID)
			}
			seen[m.ID] = true
			for _, tag := range m.Tags {
				if tag != TagVision && tag != TagReasoning && tag != TagFast {
					t.Fatalf("%s/%s: unknown tag %q", b, m.ID, tag)
				}
			}
			if m.Recommended {
				recommended++
			}
		}
		if recommended == 0 {
			t.Fatalf("%s: nothing recommended", b)
		}
	}
}

func TestEffortsAreSupportedCLILevels(t *testing.T) {
	known := map[string]map[string]bool{
		"codex":     {"low": true, "medium": true, "high": true, "xhigh": true, "max": true, "ultra": true},
		"kimi_code": {"low": true, "high": true, "max": true},
		// The bridge maps xhigh onto extra-high for models that spell it so.
		"cursor": {"none": true, "minimal": true, "low": true, "medium": true, "high": true, "xhigh": true, "max": true},
	}
	for b, list := range catalog {
		for _, m := range list {
			for _, e := range m.Efforts {
				if !known[b][e] {
					t.Fatalf("%s/%s: unknown effort %q", b, m.ID, e)
				}
			}
		}
	}
	for _, m := range Models("codex") {
		if len(m.Efforts) == 0 {
			t.Fatalf("codex/%s: every Codex model lists its efforts", m.ID)
		}
	}
}

func TestKimiCodeCatalogKeepsOnlyExactSavedK3Aliases(t *testing.T) {
	aliases := map[string][]string{}
	for _, model := range Models("kimi_code") {
		if len(model.Aliases) > 0 {
			aliases[model.ID] = model.Aliases
		}
	}
	want := map[string][]string{
		"k3.1-pro":           {"kimi-k3.1-pro"},
		"k3.1-pro-highspeed": {"kimi-k3.1-pro-highspeed"},
		"k3":                 {"kimi-k3"},
	}
	if !reflect.DeepEqual(aliases, want) {
		t.Fatalf("saved model aliases = %v, want %v", aliases, want)
	}
}

func TestUnknownBackendsGetNothing(t *testing.T) {
	for _, b := range []string{"opencode", "", "nope"} {
		if got := Models(b); len(got) != 0 {
			t.Fatalf("%q: %+v", b, got)
		}
	}
}

func TestModelsReturnsACopy(t *testing.T) {
	a := Models("codex")
	a[0].ID = "mutated"
	if Models("codex")[0].ID == "mutated" {
		t.Fatal("caller mutation leaked into the catalog")
	}
}

func TestBackendNameIsNormalized(t *testing.T) {
	if len(Models(" Claude_Code ")) == 0 {
		t.Fatal("backend id should be trimmed and lowercased")
	}
}

// TestCursorCatalogMatchesSDKModels locks the Cursor rows to the base models
// of Cursor.models.list() (@cursor/sdk 1.4.2): id, name, vendor, largest
// context, recommendation, efforts, default effort and speed tiers.
func TestCursorCatalogMatchesSDKModels(t *testing.T) {
	want := strings.Split(strings.TrimSpace(cursorModelSnapshot), "\n")
	list := Models("cursor")
	if len(list) != len(want) {
		t.Fatalf("cursor models: got %d, want %d", len(list), len(want))
	}
	for i, m := range list {
		if got := cursorSig(m); got != want[i] {
			t.Fatalf("row %d:\n got %s\nwant %s", i, got, want[i])
		}
		if m.DefaultEffort != "" && !slices.Contains(m.Efforts, m.DefaultEffort) {
			t.Fatalf("%s: default effort %q is not offered", m.ID, m.DefaultEffort)
		}
		if len(m.Tags) != 0 {
			t.Fatalf("%s: speed is a tier, not a tag: %v", m.ID, m.Tags)
		}
	}
}

func TestNonCursorCatalogSnapshots(t *testing.T) {
	want := map[string][]string{
		"claude_code": {
			"sonnet\tClaude Sonnet (latest)\tanthropic\tvision,reasoning\t200\ttrue\t",
			"opus\tClaude Opus (latest)\tanthropic\tvision,reasoning\t200\ttrue\t",
			"haiku\tClaude Haiku (latest)\tanthropic\tvision,fast\t200\tfalse\t",
			"claude-sonnet-4-5\tClaude Sonnet 4.5\tanthropic\tvision,reasoning\t200\tfalse\t",
			"claude-opus-4-1\tClaude Opus 4.1\tanthropic\tvision,reasoning\t200\tfalse\t",
		},
		"codex": {
			"gpt-6.2-sol\tGPT-6.2 Sol\topenai\tvision,reasoning\t500\ttrue\tlow,medium,high,xhigh,max,ultra",
			"gpt-6.2-astra\tGPT-6.2 Astra\topenai\tvision,reasoning\t272\ttrue\tlow,medium,high,xhigh,max,ultra",
			"gpt-6.1-terra\tGPT-6.1 Terra\topenai\tvision,reasoning\t500\tfalse\tlow,medium,high,xhigh,max,ultra",
			"gpt-6.1-sol\tGPT-6.1 Sol\topenai\tvision,reasoning\t500\ttrue\tlow,medium,high,xhigh,max,ultra",
			"gpt-6-astra\tGPT-6 Astra\topenai\tvision,reasoning\t272\ttrue\tlow,medium,high,xhigh,max,ultra",
			"gpt-6-sol\tGPT-6 Sol\topenai\tvision,reasoning\t272\tfalse\tlow,medium,high,xhigh,max,ultra",
			"gpt-6-luna\tGPT-6 Luna\topenai\tvision,reasoning\t272\tfalse\tlow,medium,high,xhigh,max",
			"gpt-5.6-sol\tGPT-5.6 Sol\topenai\tvision,reasoning\t272\tfalse\tlow,medium,high,xhigh,max,ultra",
			"gpt-5.6-terra\tGPT-5.6 Terra\topenai\tvision,reasoning\t272\tfalse\tlow,medium,high,xhigh,max,ultra",
			"gpt-5.6-luna\tGPT-5.6 Luna\topenai\tvision,reasoning\t272\tfalse\tlow,medium,high,xhigh,max",
		},
		"codebuddy": {
			"claude-4.5-sonnet\tClaude Sonnet 4.5\tanthropic\tvision,reasoning\t200\ttrue\t",
			"gpt-5\tGPT-5\topenai\tvision,reasoning\t400\ttrue\t",
			"gemini-2.5-pro\tGemini 2.5 Pro\tgoogle\tvision,reasoning\t1000\tfalse\t",
			"deepseek-v3.1\tDeepSeek V3.1\tdeepseek\treasoning\t128\tfalse\t",
			"hunyuan-turbos\tHunyuan TurboS\ttencent\tfast\t128\tfalse\t",
		},
		"kimi_code": {
			"k3.1-pro\tKimi K3.1 Pro\tkimi\tvision,reasoning\t1000\ttrue\tlow,high,max",
			"k3.1-pro-highspeed\tKimi K3.1 Pro Highspeed\tkimi\tvision,reasoning\t1000\tfalse\tlow,high,max",
			"k3\tKimi K3\tkimi\tvision,reasoning\t1000\tfalse\tlow,high,max",
			"k3-256k\tKimi K3 256K\tkimi\tvision,reasoning\t256\tfalse\tlow,high,max",
			"kimi-for-coding\tKimi K2.8 Preview\tkimi\tvision,reasoning\t1000\tfalse\tlow,high,max",
			"kimi-for-coding-highspeed\tKimi K2.7 Code Highspeed\tkimi\tvision,reasoning\t256\tfalse\t",
		},
		"trae": {
			"claude-sonnet-4\tClaude Sonnet 4\tanthropic\tvision,reasoning\t200\ttrue\t",
			"gpt-5\tGPT-5\topenai\tvision,reasoning\t400\ttrue\t",
			"gemini-2.5-pro\tGemini 2.5 Pro\tgoogle\tvision,reasoning\t1000\tfalse\t",
			"kimi-k2\tKimi K2\tmoonshot\tfast\t128\tfalse\t",
			"doubao-seed-code\tDoubao Seed Code\tbytedance\tvision,fast\t256\tfalse\t",
		},
	}
	for backend, rows := range want {
		list := Models(backend)
		if len(list) != len(rows) {
			t.Fatalf("%s: got %d models, want %d", backend, len(list), len(rows))
		}
		for i, m := range list {
			if got := modelSig(m); got != rows[i] {
				t.Fatalf("%s row %d:\n got %s\nwant %s", backend, i, got, rows[i])
			}
		}
	}
	if got := Models("opencode"); len(got) != 0 {
		t.Fatalf("opencode: %+v", got)
	}
}

func modelSig(m Model) string {
	rec := "false"
	if m.Recommended {
		rec = "true"
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s", m.ID, m.Name, m.Vendor, strings.Join(m.Tags, ","), m.Context, rec, strings.Join(m.Efforts, ","))
}

func cursorSig(m Model) string {
	return fmt.Sprintf("%s\t%s", modelSig(m), strings.Join(append([]string{m.DefaultEffort}, m.ServiceTiers...), ","))
}

// cursorModelSnapshot: modelSig, then the default effort and speed tiers.
const cursorModelSnapshot = `
grok-4.7	Grok 4.7	xai		500	false	low,medium,high,xhigh	high,fast
grok-4.6	Grok 4.6	xai		0	false	low,medium,high,xhigh	high,fast
composer-2.5	Composer 2.5	cursor		0	false		,fast
claude-opus-5-5	Claude Opus 5.5	anthropic		1000	true	low,medium,high,xhigh,max	medium,fast
claude-opus-5	Claude Opus 5	anthropic		1000	false	low,medium,high,xhigh,max	high,fast
claude-opus-4-8	Claude Opus 4.8	anthropic		1000	false	low,medium,high,xhigh,max	high,fast
gemini-4-neon	Gemini 4 Neon	google		1000	false	low,medium,high,xhigh	high
gemini-4-carbon	Gemini 4 Carbon	google		1000	false	low,medium,high	medium
gemini-4-hydrogen	Gemini 4 Hydrogen	google		1000	false	low,medium,high,xhigh	high
gemini-4-argon	Gemini 4 Argon	google		1000	false	low,medium,high	medium
muse-wonder-1.0	Muse Wonder	muse		1000	false	minimal,low,medium,high,xhigh,max	xhigh
gpt-5.6-sol	GPT-5.6 Sol	openai		1000	true	none,low,medium,high,xhigh,max	medium,fast
gpt-5.5	GPT-5.5	openai		1000	false	none,low,medium,high,xhigh	medium,fast
claude-fable-5-2	Claude Fable 5.2	anthropic		1000	false	low,medium,high,xhigh,max	high,fast
claude-fable-5-1	Claude Fable 5.1	anthropic		1000	false	low,medium,high,xhigh,max	high
claude-fable-5	Claude Fable 5	anthropic		1000	false	low,medium,high,xhigh,max	high
grok-4.5	Grok 4.5	xai		0	false	low,medium,high	high,fast
gemini-3.8-flash	Gemini 3.8 Flash	google		0	false	low,medium,high	high
muse-spark-1.3	Muse Spark 1.3	muse		1000	false	minimal,low,medium,high,xhigh,max	high
gpt-5.6-terra	GPT-5.6 Terra	openai		1000	false	none,low,medium,high,xhigh,max	medium,fast
claude-sonnet-5-5	Claude Sonnet 5.5	anthropic		1000	false	low,medium,high,xhigh,max	high
claude-sonnet-5	Claude Sonnet 5	anthropic		1000	true	low,medium,high,xhigh,max	high
claude-sonnet-4-6	Claude Sonnet 4.6	anthropic		1000	false	low,medium,high,max	medium
composer-2	Composer 2	cursor		0	false		,fast
gpt-5.3-codex	Codex 5.3	openai		0	false	low,medium,high,xhigh	high,fast
claude-opus-4-7	Claude Opus 4.7	anthropic		1000	false	low,medium,high,xhigh,max	xhigh,fast
gpt-5.4	GPT-5.4	openai		1000	false	none,low,medium,high,xhigh	medium,fast
claude-opus-4-6	Claude Opus 4.6	anthropic		1000	false	low,medium,high,max	high
claude-opus-4-5	Claude Opus 4.5	anthropic		0	false		
gpt-5.2	GPT-5.2	openai		0	false	low,medium,high,xhigh	high,fast
gpt-5.6-luna	GPT-5.6 Luna	openai		1000	false	none,low,medium,high,xhigh,max	medium,fast
gemini-3.1-pro	Gemini 3.1 Pro	google		0	false		
gpt-5.4-mini	GPT-5.4 Mini	openai		0	false	none,low,medium,high,xhigh	medium
gpt-5.4-nano	GPT-5.4 Nano	openai		0	false	none,low,medium,high,xhigh	medium
claude-haiku-5-5	Claude Haiku 5.5	anthropic		1000	false	low,medium,high,xhigh,max	high
claude-haiku-4-5	Claude Haiku 4.5	anthropic		0	false		
claude-sonnet-4-5	Claude Sonnet 4.5	anthropic		200	false		
gpt-5.1	GPT-5.1	openai		0	false	low,medium,high	medium
gemini-3-flash	Gemini 3 Flash	google		0	false		
gemini-3.5-flash	Gemini 3.5 Flash	google		0	false		
claude-sonnet-4	Claude Sonnet 4	anthropic		200	false		
gpt-5-mini	GPT-5 Mini	openai		0	false		
gemini-2.5-flash	Gemini 2.5 Flash	google		0	false		
kimi-k3	Kimi K3	moonshot		0	false	low,high,max	max
kimi-k2.7-code	Kimi K2.7 Code	moonshot		0	false		
glm-5.2	GLM 5.2	zhipu		0	false	high,max	high
glm-5p3	GLM 5.3	zhipu		0	false	low,high,max	max
glm-5p3-flash	GLM 5.3 Flash	zhipu		0	false	low,high,max	max
deepseek-v41-flash-1017	DeepSeek V4.1 Flash 1017	deepseek		0	false	none,low,medium,high,max	max
`
