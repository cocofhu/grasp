package handoff

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderKeepsEverythingThatFits(t *testing.T) {
	got := Render([]Entry{
		{Role: User, Text: "build a todo app"},
		{Role: Agent, Text: "which framework?", Details: []string{"问题: framework", "  - React", ""}},
	}, Options{TokenCap: 1000, Notes: []string{" read products first ", " "}})
	want := Notice + "read products first\n\n" +
		"[#1 用户] build a todo app\n" +
		"[#2 Agent] which framework?\n  问题: framework\n    - React\n"
	if got.Text != want {
		t.Fatalf("text = %q", got.Text)
	}
	if !reflect.DeepEqual(got.Included, []int{1, 2}) || got.Omitted != nil || got.Tokens != Tokens(want) {
		t.Fatalf("result = %+v", got)
	}
}

func TestRenderEmptyHistory(t *testing.T) {
	got := Render(nil, Options{TokenCap: 1000})
	if got.Text != Notice+"\n" || got.Included != nil {
		t.Fatalf("result = %+v", got)
	}
}

func TestRenderSelectsByPriorityWithinBudget(t *testing.T) {
	filler := strings.Repeat("填", 300)
	entries := []Entry{
		{Role: User, Text: "原始需求 " + filler},  // 1: first user message
		{Role: Agent, Text: "回复一 " + filler},  // 2
		{Role: User, Text: "补充一 " + filler},   // 3
		{Role: Agent, Text: "回复二 " + filler},  // 4
		{Role: User, Text: "补充二 " + filler},   // 5
		{Role: Agent, Text: "最新回复 " + filler}, // 6: last agent reply
		{Role: User, Text: "最新要求 " + filler},  // 7: last user message
	}
	limit := Tokens(Notice+"\n") + Tokens(omissionNote(span(1, 7))) + 4*320
	got := Render(entries, Options{TokenCap: limit})
	if !reflect.DeepEqual(got.Included, []int{1, 5, 6, 7}) || !reflect.DeepEqual(got.Omitted, []int{2, 3, 4}) {
		t.Fatalf("included %v omitted %v", got.Included, got.Omitted)
	}
	if got.Tokens > limit {
		t.Fatalf("tokens %d over cap %d", got.Tokens, limit)
	}
	if !strings.Contains(got.Text, "省略了 3 条记录：#2–#4") {
		t.Fatalf("omission note missing: %q", got.Text[:300])
	}
	order := []string{"[#1 用户] 原始需求", "[#5 用户] 补充二", "[#6 Agent] 最新回复", "[#7 用户] 最新要求"}
	last := -1
	for _, s := range order {
		i := strings.Index(got.Text, s)
		if i <= last {
			t.Fatalf("%q out of dialogue order", s)
		}
		last = i
	}
}

func TestRenderExcerptsOversizedPriorityEntry(t *testing.T) {
	entries := []Entry{
		{Role: User, Text: "old"},
		{Role: Agent, Text: "ok"},
		{Role: User, Text: strings.Repeat("长", 5000)},
	}
	got := Render(entries, Options{TokenCap: 800})
	if got.Tokens > 800 {
		t.Fatalf("tokens %d over cap", got.Tokens)
	}
	if !strings.Contains(got.Text, "[#3 用户] 长长") || !strings.Contains(got.Text, excerptMark) {
		t.Fatalf("last user message not excerpted: %q", got.Text)
	}
	if !reflect.DeepEqual(got.Included, []int{1, 2, 3}) {
		t.Fatalf("included %v", got.Included)
	}
}

func TestRenderOmitsWhatCannotBeExcerpted(t *testing.T) {
	entries := []Entry{
		{Role: User, Text: strings.Repeat("a", 4000)},
		{Role: Agent, Text: strings.Repeat("b", 4000)},
		{Role: User, Text: "latest"},
	}
	limit := Tokens(Notice+"\n") + Tokens(omissionNote(span(1, 3))) + 20
	got := Render(entries, Options{TokenCap: limit})
	if !reflect.DeepEqual(got.Included, []int{3}) || !reflect.DeepEqual(got.Omitted, []int{1, 2}) {
		t.Fatalf("included %v omitted %v", got.Included, got.Omitted)
	}
}

func TestRenderDefaultsToDefaultCap(t *testing.T) {
	entries := []Entry{{Role: User, Text: strings.Repeat("长", 2*DefaultTokenCap)}}
	if got := Render(entries, Options{}); got.Tokens > DefaultTokenCap || got.Included[0] != 1 {
		t.Fatalf("result tokens %d included %v", got.Tokens, got.Included)
	}
}

func TestTokensEstimate(t *testing.T) {
	for s, want := range map[string]int{"": 0, "abcd": 1, "abcde": 2, "中文": 2, "ab中": 2} {
		if got := Tokens(s); got != want {
			t.Errorf("Tokens(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestRanges(t *testing.T) {
	if got := ranges([]int{1, 3, 4, 5, 8, 9}); got != "#1、#3–#5、#8–#9" {
		t.Fatal(got)
	}
}
