package clicatalog

import (
	"fmt"
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

func TestEffortsAreCodexLevels(t *testing.T) {
	known := map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true, "ultra": true}
	for b, list := range catalog {
		for _, m := range list {
			if b != "codex" && len(m.Efforts) > 0 {
				t.Fatalf("%s/%s: only codex takes a separate effort", b, m.ID)
			}
			for _, e := range m.Efforts {
				if !known[e] {
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

func TestCursorCatalogMatchesCLISnapshot(t *testing.T) {
	list := Models("cursor")
	want := parseCursorSnapshot(t, cursorModelSnapshot)
	if len(want) != 264 {
		t.Fatalf("snapshot must lock 264 ids, got %d", len(want))
	}
	if len(list) != len(want) {
		t.Fatalf("cursor models: got %d, want %d", len(list), len(want))
	}
	recommended := map[string]bool{
		"claude-opus-5-5-medium": true,
		"gpt-5.6-sol-medium":     true,
		"claude-sonnet-5-high":   true,
	}
	seen := map[string]bool{}
	var gotRec []string
	for i, m := range list {
		if m.ID != want[i].id || m.Name != want[i].name {
			t.Fatalf("row %d: got %s %q, want %s %q", i, m.ID, m.Name, want[i].id, want[i].name)
		}
		if m.ID == "auto" || m.Name == "" || m.Vendor == "" {
			t.Fatalf("incomplete row %+v", m)
		}
		if seen[m.ID] {
			t.Fatalf("duplicate %s", m.ID)
		}
		seen[m.ID] = true
		if got := cursorVendor(m.ID); got == "" || m.Vendor != got {
			t.Fatalf("%s: vendor %q, want %q", m.ID, m.Vendor, got)
		}
		wantFast := strings.HasSuffix(m.ID, "-fast")
		hasFast := len(m.Tags) == 1 && m.Tags[0] == TagFast
		if wantFast != hasFast || (len(m.Tags) > 0 && !hasFast) {
			t.Fatalf("%s: tags %v", m.ID, m.Tags)
		}
		wantCtx := 0
		if strings.Contains(m.Name, "1M") {
			wantCtx = 1000
		}
		if m.Context != wantCtx {
			t.Fatalf("%s: context %d, want %d", m.ID, m.Context, wantCtx)
		}
		if m.Recommended != recommended[m.ID] {
			t.Fatalf("%s: recommended %v", m.ID, m.Recommended)
		}
		if m.Recommended {
			gotRec = append(gotRec, m.ID)
		}
		if len(m.Efforts) != 0 {
			t.Fatalf("%s: cursor models do not carry efforts", m.ID)
		}
		if strings.Contains(m.Name, "  ") || strings.ContainsRune(m.Name, '\u200b') || strings.TrimSpace(m.Name) != m.Name {
			t.Fatalf("%s: name not normalized %q", m.ID, m.Name)
		}
		if strings.Contains(want[i].name, "(NO ZDR)") && !strings.Contains(m.Name, "(NO ZDR)") {
			t.Fatalf("%s: dropped (NO ZDR)", m.ID)
		}
	}
	if len(gotRec) != len(recommended) {
		t.Fatalf("recommended ids %v", gotRec)
	}
	for id := range recommended {
		found := false
		for _, got := range gotRec {
			if got == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing recommended %s in %v", id, gotRec)
		}
	}

	xhigh := findCursorModel(t, "grok-4.7-xhigh")
	if xhigh.Name != "Grok 4.7 Extra High" || xhigh.Vendor != "xai" || xhigh.Context != 0 || len(xhigh.Tags) != 0 {
		t.Fatalf("grok-4.7-xhigh %+v", xhigh)
	}
	medium := findCursorModel(t, "grok-4.7-medium")
	if medium.Name != "Grok 4.7 Medium" || medium.Vendor != "xai" {
		t.Fatalf("grok-4.7-medium %+v", medium)
	}
	fast := findCursorModel(t, "composer-2.5-fast")
	if fast.Vendor != "cursor" || len(fast.Tags) != 1 || fast.Tags[0] != TagFast {
		t.Fatalf("composer-2.5-fast %+v", fast)
	}
	if tags := findCursorModel(t, "composer-2.5").Tags; len(tags) != 0 {
		t.Fatalf("composer-2.5 tags %v", tags)
	}
}

func TestNonCursorCatalogsUnchanged(t *testing.T) {
	want := map[string][]string{
		"claude_code": {
			"sonnet\tClaude Sonnet (latest)\tanthropic\tvision,reasoning\t200\ttrue\t",
			"opus\tClaude Opus (latest)\tanthropic\tvision,reasoning\t200\ttrue\t",
			"haiku\tClaude Haiku (latest)\tanthropic\tvision,fast\t200\tfalse\t",
			"claude-sonnet-4-5\tClaude Sonnet 4.5\tanthropic\tvision,reasoning\t200\tfalse\t",
			"claude-opus-4-1\tClaude Opus 4.1\tanthropic\tvision,reasoning\t200\tfalse\t",
		},
		"codex": {
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
			"kimi-k3.1-pro\tKimi K3.1 Pro\tkimi\tvision,reasoning\t1000\ttrue\t",
			"kimi-k3.1-pro-highspeed\tKimi K3.1 Pro Highspeed\tkimi\tvision,reasoning\t1000\tfalse\t",
			"kimi-k3\tKimi K3\tkimi\tvision,reasoning\t1000\tfalse\t",
			"kimi-k2.7-code\tKimi K2.7 Code\tkimi\treasoning\t256\tfalse\t",
			"kimi-k2.7-code-highspeed\tKimi K2.7 Code Highspeed\tkimi\treasoning\t256\tfalse\t",
			"kimi-k2.6\tKimi K2.6\tkimi\tvision,reasoning\t256\tfalse\t",
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

func findCursorModel(t *testing.T, id string) Model {
	t.Helper()
	for _, m := range Models("cursor") {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("missing %s", id)
	return Model{}
}

type cursorSnapRow struct {
	id, name string
}

func parseCursorSnapshot(t *testing.T, raw string) []cursorSnapRow {
	t.Helper()
	var out []cursorSnapRow
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		id, name, ok := strings.Cut(line, "\t")
		if !ok || id == "" || name == "" {
			t.Fatalf("bad snapshot line %q", line)
		}
		out = append(out, cursorSnapRow{id: id, name: name})
	}
	return out
}

func cursorVendor(id string) string {
	switch {
	case strings.HasPrefix(id, "claude"):
		return "anthropic"
	case strings.HasPrefix(id, "gpt"):
		return "openai"
	case strings.HasPrefix(id, "gemini"):
		return "google"
	case strings.HasPrefix(id, "cursor-grok"), strings.HasPrefix(id, "grok"):
		return "xai"
	case strings.HasPrefix(id, "composer"):
		return "cursor"
	case strings.HasPrefix(id, "kimi"):
		return "moonshot"
	case strings.HasPrefix(id, "muse"):
		return "muse"
	case strings.HasPrefix(id, "glm"):
		return "zhipu"
	default:
		return ""
	}
}

func modelSig(m Model) string {
	rec := "false"
	if m.Recommended {
		rec = "true"
	}
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s", m.ID, m.Name, m.Vendor, strings.Join(m.Tags, ","), m.Context, rec, strings.Join(m.Efforts, ","))
}

// cursorModelSnapshot is the non-auto output of `cursor-agent models`: id, tab, normalized display name.
const cursorModelSnapshot = `
gpt-5.3-codex-low	Codex 5.3 Low
gpt-5.3-codex-low-fast	Codex 5.3 Low Fast
gpt-5.3-codex	Codex 5.3
gpt-5.3-codex-fast	Codex 5.3 Fast
gpt-5.3-codex-high	Codex 5.3 High
gpt-5.3-codex-high-fast	Codex 5.3 High Fast
gpt-5.3-codex-xhigh	Codex 5.3 Extra High
gpt-5.3-codex-xhigh-fast	Codex 5.3 Extra High Fast
gpt-5.2	GPT-5.2
composer-2.5	Composer 2.5
claude-opus-5-thinking-high	Claude Opus 5 1M Thinking
claude-opus-5-thinking-high-fast	Claude Opus 5 1M Thinking Fast
gpt-5.6-sol-high	GPT-5.6 Sol 1M High
gpt-5.6-sol-high-fast	GPT-5.6 Sol 1M High Fast
gpt-5.6-sol-xhigh	GPT-5.6 Sol 1M Extra High
gpt-5.6-sol-xhigh-fast	GPT-5.6 Sol 1M Extra High Fast
claude-fable-5-thinking-high	Claude Fable 5 1M Thinking (NO ZDR)
claude-fable-5-thinking-xhigh	Claude Fable 5 1M Extra High Thinking (NO ZDR)
cursor-grok-4.5-high	Grok 4.5
cursor-grok-4.5-high-fast	Grok 4.5 Fast
gemini-3.7-flash-high	Gemini 3.7 Flash
claude-sonnet-5-thinking-high	Claude Sonnet 5 1M Thinking
claude-sonnet-5-thinking-xhigh	Claude Sonnet 5 1M Extra High Thinking
gpt-5.6-luna-high	GPT-5.6 Luna 1M High
grok-4.7-low	Grok 4.7 Low
grok-4.7-low-fast	Grok 4.7 Low Fast
grok-4.7-medium	Grok 4.7 Medium
grok-4.7-medium-fast	Grok 4.7 Medium Fast
grok-4.7-high	Grok 4.7 High
grok-4.7-high-fast	Grok 4.7 High Fast
grok-4.7-xhigh	Grok 4.7 Extra High
grok-4.7-xhigh-fast	Grok 4.7 Extra High Fast
cursor-grok-4.6-low	Grok 4.6 Low
cursor-grok-4.6-low-fast	Grok 4.6 Low Fast
cursor-grok-4.6-medium	Grok 4.6 Medium
cursor-grok-4.6-medium-fast	Grok 4.6 Medium Fast
cursor-grok-4.6-high	Grok 4.6
cursor-grok-4.6-high-fast	Grok 4.6 Fast
cursor-grok-4.6-xhigh	Grok 4.6 Extra High
cursor-grok-4.6-xhigh-fast	Grok 4.6 Extra High Fast
composer-2.5-fast	Composer 2.5 Fast
claude-opus-5-5-low	Claude Opus 5.5 1M Low
claude-opus-5-5-low-fast	Claude Opus 5.5 1M Low Fast
claude-opus-5-5-medium	Claude Opus 5.5 1M
claude-opus-5-5-medium-fast	Claude Opus 5.5 1M Fast
claude-opus-5-5-high	Claude Opus 5.5 1M High
claude-opus-5-5-high-fast	Claude Opus 5.5 1M High Fast
claude-opus-5-5-xhigh	Claude Opus 5.5 1M Extra High
claude-opus-5-5-xhigh-fast	Claude Opus 5.5 1M Extra High Fast
claude-opus-5-5-max	Claude Opus 5.5 1M Max
claude-opus-5-5-max-fast	Claude Opus 5.5 1M Max Fast
claude-opus-5-low	Claude Opus 5 1M Low
claude-opus-5-low-fast	Claude Opus 5 1M Low Fast
claude-opus-5-medium	Claude Opus 5 1M Medium
claude-opus-5-medium-fast	Claude Opus 5 1M Medium Fast
claude-opus-5-high	Claude Opus 5 1M
claude-opus-5-high-fast	Claude Opus 5 1M Fast
claude-opus-5-thinking-low	Claude Opus 5 1M Low Thinking
claude-opus-5-thinking-low-fast	Claude Opus 5 1M Low Thinking Fast
claude-opus-5-thinking-medium	Claude Opus 5 1M Medium Thinking
claude-opus-5-thinking-medium-fast	Claude Opus 5 1M Medium Thinking Fast
claude-opus-5-thinking-xhigh	Claude Opus 5 1M Extra High Thinking
claude-opus-5-thinking-xhigh-fast	Claude Opus 5 1M Extra High Thinking Fast
claude-opus-5-thinking-max	Claude Opus 5 1M Max Thinking
claude-opus-5-thinking-max-fast	Claude Opus 5 1M Max Thinking Fast
claude-opus-4-8-low	Claude Opus 4.8 1M Low
claude-opus-4-8-low-fast	Claude Opus 4.8 1M Low Fast
claude-opus-4-8-medium	Claude Opus 4.8 1M Medium
claude-opus-4-8-medium-fast	Claude Opus 4.8 1M Medium Fast
claude-opus-4-8-high	Claude Opus 4.8 1M
claude-opus-4-8-high-fast	Claude Opus 4.8 1M Fast
claude-opus-4-8-xhigh	Claude Opus 4.8 1M Extra High
claude-opus-4-8-xhigh-fast	Claude Opus 4.8 1M Extra High Fast
claude-opus-4-8-max	Claude Opus 4.8 1M Max
claude-opus-4-8-max-fast	Claude Opus 4.8 1M Max Fast
claude-opus-4-8-thinking-low	Claude Opus 4.8 1M Low Thinking
claude-opus-4-8-thinking-low-fast	Claude Opus 4.8 1M Low Thinking Fast
claude-opus-4-8-thinking-medium	Claude Opus 4.8 1M Medium Thinking
claude-opus-4-8-thinking-medium-fast	Claude Opus 4.8 1M Medium Thinking Fast
claude-opus-4-8-thinking-high	Claude Opus 4.8 1M Thinking
claude-opus-4-8-thinking-high-fast	Claude Opus 4.8 1M Thinking Fast
claude-opus-4-8-thinking-xhigh	Claude Opus 4.8 1M Extra High Thinking
claude-opus-4-8-thinking-xhigh-fast	Claude Opus 4.8 1M Extra High Thinking Fast
claude-opus-4-8-thinking-max	Claude Opus 4.8 1M Max Thinking
claude-opus-4-8-thinking-max-fast	Claude Opus 4.8 1M Max Thinking Fast
gemini-4-argon-low	Gemini 4 Argon Low
gemini-4-argon-medium	Gemini 4 Argon Medium
gemini-4-argon-high	Gemini 4 Argon High
muse-wonder-1.0-minimal	Muse Wonder Minimal
muse-wonder-1.0-low	Muse Wonder Low
muse-wonder-1.0-medium	Muse Wonder Medium
muse-wonder-1.0-high	Muse Wonder High
muse-wonder-1.0-xhigh	Muse Wonder Extra High
muse-wonder-1.0-max	Muse Wonder Max
gpt-5.6-sol-none	GPT-5.6 Sol 1M None
gpt-5.6-sol-none-fast	GPT-5.6 Sol 1M None Fast
gpt-5.6-sol-low	GPT-5.6 Sol 1M Low
gpt-5.6-sol-low-fast	GPT-5.6 Sol 1M Low Fast
gpt-5.6-sol-medium	GPT-5.6 Sol 1M
gpt-5.6-sol-medium-fast	GPT-5.6 Sol 1M Fast
gpt-5.6-sol-max	GPT-5.6 Sol 1M Max
gpt-5.6-sol-max-fast	GPT-5.6 Sol 1M Max Fast
gpt-5.5-none	GPT-5.5 1M None
gpt-5.5-none-fast	GPT-5.5 None Fast
gpt-5.5-low	GPT-5.5 1M Low
gpt-5.5-low-fast	GPT-5.5 Low Fast
gpt-5.5-medium	GPT-5.5 1M
gpt-5.5-medium-fast	GPT-5.5 Fast
gpt-5.5-high	GPT-5.5 1M High
gpt-5.5-high-fast	GPT-5.5 High Fast
gpt-5.5-extra-high	GPT-5.5 1M Extra High
gpt-5.5-extra-high-fast	GPT-5.5 Extra High Fast
claude-fable-5-1-low	Claude Fable 5.1 1M Low (NO ZDR)
claude-fable-5-1-medium	Claude Fable 5.1 1M Medium (NO ZDR)
claude-fable-5-1-high	Claude Fable 5.1 1M (NO ZDR)
claude-fable-5-1-xhigh	Claude Fable 5.1 1M Extra High (NO ZDR)
claude-fable-5-1-max	Claude Fable 5.1 1M Max (NO ZDR)
claude-fable-5-1-thinking-low	Claude Fable 5.1 1M Low Thinking (NO ZDR)
claude-fable-5-1-thinking-medium	Claude Fable 5.1 1M Medium Thinking (NO ZDR)
claude-fable-5-1-thinking-high	Claude Fable 5.1 1M Thinking (NO ZDR)
claude-fable-5-1-thinking-xhigh	Claude Fable 5.1 1M Extra High Thinking (NO ZDR)
claude-fable-5-1-thinking-max	Claude Fable 5.1 1M Max Thinking (NO ZDR)
claude-fable-5-low	Claude Fable 5 1M Low (NO ZDR)
claude-fable-5-medium	Claude Fable 5 1M Medium (NO ZDR)
claude-fable-5-high	Claude Fable 5 1M (NO ZDR)
claude-fable-5-xhigh	Claude Fable 5 1M Extra High (NO ZDR)
claude-fable-5-max	Claude Fable 5 1M Max (NO ZDR)
claude-fable-5-thinking-low	Claude Fable 5 1M Low Thinking (NO ZDR)
claude-fable-5-thinking-medium	Claude Fable 5 1M Medium Thinking (NO ZDR)
claude-fable-5-thinking-max	Claude Fable 5 1M Max Thinking (NO ZDR)
cursor-grok-4.5-low	Grok 4.5 Low
cursor-grok-4.5-low-fast	Grok 4.5 Low Fast
cursor-grok-4.5-medium	Grok 4.5 Medium
cursor-grok-4.5-medium-fast	Grok 4.5 Medium Fast
gemini-3.8-flash-low	Gemini 3.8 Flash Low
gemini-3.8-flash-medium	Gemini 3.8 Flash Medium
gemini-3.8-flash-high	Gemini 3.8 Flash High
gemini-3.7-flash-low	Gemini 3.7 Flash Low
gemini-3.7-flash-medium	Gemini 3.7 Flash Medium
muse-spark-1.3-minimal	Muse Spark 1.3 1M Minimal
muse-spark-1.3-low	Muse Spark 1.3 1M Low
muse-spark-1.3-medium	Muse Spark 1.3 1M Medium
muse-spark-1.3-high	Muse Spark 1.3 1M
muse-spark-1.3-xhigh	Muse Spark 1.3 1M Extra High
muse-spark-1.3-max	Muse Spark 1.3 1M Max
gpt-5.6-terra-none	GPT-5.6 Terra 1M None
gpt-5.6-terra-none-fast	GPT-5.6 Terra 1M None Fast
gpt-5.6-terra-low	GPT-5.6 Terra 1M Low
gpt-5.6-terra-low-fast	GPT-5.6 Terra 1M Low Fast
gpt-5.6-terra-medium	GPT-5.6 Terra 1M
gpt-5.6-terra-medium-fast	GPT-5.6 Terra 1M Fast
gpt-5.6-terra-high	GPT-5.6 Terra 1M High
gpt-5.6-terra-high-fast	GPT-5.6 Terra 1M High Fast
gpt-5.6-terra-xhigh	GPT-5.6 Terra 1M Extra High
gpt-5.6-terra-xhigh-fast	GPT-5.6 Terra 1M Extra High Fast
gpt-5.6-terra-max	GPT-5.6 Terra 1M Max
gpt-5.6-terra-max-fast	GPT-5.6 Terra 1M Max Fast
claude-sonnet-5-5-low	Claude Sonnet 5.5 Low
claude-sonnet-5-5-medium	Claude Sonnet 5.5 Medium
claude-sonnet-5-5-high	Claude Sonnet 5.5 High
claude-sonnet-5-5-xhigh	Claude Sonnet 5.5 Extra High
claude-sonnet-5-5-max	Claude Sonnet 5.5 Max
claude-sonnet-5-low	Claude Sonnet 5 1M Low
claude-sonnet-5-medium	Claude Sonnet 5 1M Medium
claude-sonnet-5-high	Claude Sonnet 5 1M
claude-sonnet-5-xhigh	Claude Sonnet 5 1M Extra High
claude-sonnet-5-max	Claude Sonnet 5 1M Max
claude-sonnet-5-thinking-low	Claude Sonnet 5 1M Low Thinking
claude-sonnet-5-thinking-medium	Claude Sonnet 5 1M Medium Thinking
claude-sonnet-5-thinking-max	Claude Sonnet 5 1M Max Thinking
claude-4.6-sonnet-medium	Claude Sonnet 4.6 1M
claude-4.6-sonnet-medium-thinking	Claude Sonnet 4.6 1M Thinking
claude-opus-4-7-low	Claude Opus 4.7 1M Low
claude-opus-4-7-low-fast	Claude Opus 4.7 1M Low Fast
claude-opus-4-7-medium	Claude Opus 4.7 1M Medium
claude-opus-4-7-medium-fast	Claude Opus 4.7 1M Medium Fast
claude-opus-4-7-high	Claude Opus 4.7 1M High
claude-opus-4-7-high-fast	Claude Opus 4.7 1M High Fast
claude-opus-4-7-xhigh	Claude Opus 4.7 1M
claude-opus-4-7-xhigh-fast	Claude Opus 4.7 1M Fast
claude-opus-4-7-max	Claude Opus 4.7 1M Max
claude-opus-4-7-max-fast	Claude Opus 4.7 1M Max Fast
claude-opus-4-7-thinking-low	Claude Opus 4.7 1M Low Thinking
claude-opus-4-7-thinking-low-fast	Claude Opus 4.7 1M Low Thinking Fast
claude-opus-4-7-thinking-medium	Claude Opus 4.7 1M Medium Thinking
claude-opus-4-7-thinking-medium-fast	Claude Opus 4.7 1M Medium Thinking Fast
claude-opus-4-7-thinking-high	Claude Opus 4.7 1M High Thinking
claude-opus-4-7-thinking-high-fast	Claude Opus 4.7 1M High Thinking Fast
claude-opus-4-7-thinking-xhigh	Claude Opus 4.7 1M Thinking
claude-opus-4-7-thinking-xhigh-fast	Claude Opus 4.7 1M Thinking Fast
claude-opus-4-7-thinking-max	Claude Opus 4.7 1M Max Thinking
claude-opus-4-7-thinking-max-fast	Claude Opus 4.7 1M Max Thinking Fast
gpt-5.4-low	GPT-5.4 1M Low
gpt-5.4-medium	GPT-5.4 1M
gpt-5.4-medium-fast	GPT-5.4 Fast
gpt-5.4-high	GPT-5.4 1M High
gpt-5.4-high-fast	GPT-5.4 High Fast
gpt-5.4-xhigh	GPT-5.4 1M Extra High
gpt-5.4-xhigh-fast	GPT-5.4 Extra High Fast
claude-4.6-opus-high	Claude Opus 4.6 1M
claude-4.6-opus-max	Claude Opus 4.6 1M Max
claude-4.6-opus-high-thinking	Claude Opus 4.6 1M Thinking
claude-4.6-opus-max-thinking	Claude Opus 4.6 1M Max Thinking
claude-4.5-opus-high	Claude Opus 4.5
claude-4.5-opus-high-thinking	Claude Opus 4.5 Thinking
gpt-5.2-low	GPT-5.2 Low
gpt-5.2-low-fast	GPT-5.2 Low Fast
gpt-5.2-fast	GPT-5.2 Fast
gpt-5.2-high	GPT-5.2 High
gpt-5.2-high-fast	GPT-5.2 High Fast
gpt-5.2-xhigh	GPT-5.2 Extra High
gpt-5.2-xhigh-fast	GPT-5.2 Extra High Fast
gpt-5.6-luna-none	GPT-5.6 Luna 1M None
gpt-5.6-luna-none-fast	GPT-5.6 Luna 1M None Fast
gpt-5.6-luna-low	GPT-5.6 Luna 1M Low
gpt-5.6-luna-low-fast	GPT-5.6 Luna 1M Low Fast
gpt-5.6-luna-medium	GPT-5.6 Luna 1M
gpt-5.6-luna-medium-fast	GPT-5.6 Luna 1M Fast
gpt-5.6-luna-high-fast	GPT-5.6 Luna 1M High Fast
gpt-5.6-luna-xhigh	GPT-5.6 Luna 1M Extra High
gpt-5.6-luna-xhigh-fast	GPT-5.6 Luna 1M Extra High Fast
gpt-5.6-luna-max	GPT-5.6 Luna 1M Max
gpt-5.6-luna-max-fast	GPT-5.6 Luna 1M Max Fast
gemini-3.6-flash-minimal	Gemini 3.6 Flash Minimal
gemini-3.6-flash-low	Gemini 3.6 Flash Low
gemini-3.6-flash-medium	Gemini 3.6 Flash Medium
gemini-3.6-flash-high	Gemini 3.6 Flash
gemini-3.1-pro	Gemini 3.1 Pro
gpt-5.4-mini-none	GPT-5.4 Mini None
gpt-5.4-mini-low	GPT-5.4 Mini Low
gpt-5.4-mini-medium	GPT-5.4 Mini
gpt-5.4-mini-high	GPT-5.4 Mini High
gpt-5.4-mini-xhigh	GPT-5.4 Mini Extra High
gpt-5.4-nano-none	GPT-5.4 Nano None
gpt-5.4-nano-low	GPT-5.4 Nano Low
gpt-5.4-nano-medium	GPT-5.4 Nano
gpt-5.4-nano-high	GPT-5.4 Nano High
gpt-5.4-nano-xhigh	GPT-5.4 Nano Extra High
claude-haiku-5-5-low	Claude Haiku 5.5 Low No Thinking
claude-haiku-5-5-medium	Claude Haiku 5.5 Medium No Thinking
claude-haiku-5-5-high	Claude Haiku 5.5 High No Thinking
claude-haiku-5-5-xhigh	Claude Haiku 5.5 Extra High No Thinking
claude-haiku-5-5-max	Claude Haiku 5.5 Max No Thinking
claude-haiku-5-5-thinking-low	Claude Haiku 5.5 Low
claude-haiku-5-5-thinking-medium	Claude Haiku 5.5 Medium
claude-haiku-5-5-thinking-high	Claude Haiku 5.5 High
claude-haiku-5-5-thinking-xhigh	Claude Haiku 5.5 Extra High
claude-haiku-5-5-thinking-max	Claude Haiku 5.5 Max
claude-4.5-sonnet	Claude Sonnet 4.5
claude-4.5-sonnet-thinking	Claude Sonnet 4.5 Thinking
gpt-5.1-low	GPT-5.1 Low
gpt-5.1	GPT-5.1
gpt-5.1-high	GPT-5.1 High
gemini-3-flash	Gemini 3 Flash
gemini-3.5-flash	Gemini 3.5 Flash
claude-4-sonnet	Claude Sonnet 4
claude-4-sonnet-thinking	Claude Sonnet 4 Thinking
gpt-5-mini	GPT-5 Mini
kimi-k3-low	Kimi K3 Low
kimi-k3-high	Kimi K3 High
kimi-k3-max	Kimi K3
kimi-k2.7-code	Kimi K2.7 Code
glm-5.2-high	GLM 5.2
glm-5.2-max	GLM 5.2 Max
`
