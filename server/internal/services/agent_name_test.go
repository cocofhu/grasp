package services

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func TestSuggestAgentRename_v2v3(t *testing.T) {
	existing := map[string]struct{}{"agent": {}, "agent_v2": {}}
	got := SuggestAgentRename("agent", existing)
	if got != "agent_v3" {
		t.Fatalf("got %q", got)
	}
	if _, err := NormalizeAndValidateAgentName(got); err != nil {
		t.Fatal(err)
	}
	zh := SuggestAgentRename("视觉研发", map[string]struct{}{"视觉研发": {}})
	if zh != "视觉研发_v2" {
		t.Fatalf("zh got %q", zh)
	}
}

func TestNormalizeAndValidateAgentName_demoSamples(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		wantOK  bool
		wantOut string
	}{
		{name: "screenshot chinese mix", input: "Approve需求澄清视觉研发", wantOK: true, wantOut: "Approve需求澄清视觉研发"},
		{name: "ascii mixed hyphen", input: "Approve-需求澄清", wantOK: true, wantOut: "Approve-需求澄清"},
		{name: "underscore only", input: "___", wantOK: true, wantOut: "___"},
		{name: "platform ascii", input: "Agent_1-test", wantOK: true, wantOut: "Agent_1-test"},
		{name: "space rejected", input: "Approve 需求", wantOK: false},
		{name: "dot rejected write", input: "clarify.v1", wantOK: false},
		{name: "fullwidth hyphen", input: "需求－澄清", wantOK: false},
		{name: "slash", input: "a/b", wantOK: false},
		{name: "backslash", input: `a\b`, wantOK: false},
		{name: "empty", input: "  ", wantOK: false},
		{name: "dot alone", input: ".", wantOK: false},
		{name: "dotdot", input: "..", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAndValidateAgentName(tc.input)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("NormalizeAndValidateAgentName(%q): %v", tc.input, err)
				}
				if got != tc.wantOut {
					t.Fatalf("got %q want %q", got, tc.wantOut)
				}
				return
			}
			if err == nil {
				t.Fatalf("NormalizeAndValidateAgentName(%q) = %q, want error", tc.input, got)
			}
			if !errors.Is(err, ErrInvalidAgentName) {
				t.Fatalf("error %v should wrap ErrInvalidAgentName", err)
			}
		})
	}
}

func TestNormalizeAndValidateAgentName_NFCAndRuneLimit(t *testing.T) {
	t.Parallel()
	// U+0041 + combining acute → NFC Á
	decomposed := "A\u0301gent"
	got, err := NormalizeAndValidateAgentName(decomposed)
	if err != nil {
		t.Fatal(err)
	}
	want := norm.NFC.String(decomposed)
	if got != want {
		t.Fatalf("NFC got %q (%v) want %q", got, []rune(got), want)
	}
	precomposed, err := NormalizeAndValidateAgentName("Ágent")
	if err != nil {
		t.Fatal(err)
	}
	if got != precomposed {
		t.Fatalf("NFC forms diverge: %q vs %q", got, precomposed)
	}

	tooLong := strings.Repeat("中", MaxAgentNameRunes+1)
	if _, err := NormalizeAndValidateAgentName(tooLong); err == nil {
		t.Fatal("expected too-long rejection")
	}
	exact := strings.Repeat("中", MaxAgentNameRunes)
	if utf8.RuneCountInString(exact) != MaxAgentNameRunes {
		t.Fatalf("fixture rune count = %d", utf8.RuneCountInString(exact))
	}
	if _, err := NormalizeAndValidateAgentName(exact); err != nil {
		t.Fatalf("64-rune chinese should pass: %v", err)
	}
}

func TestSanitizeAgentPath_dotRejectedAndUnicode(t *testing.T) {
	t.Parallel()
	if got := sanitizeAgentPath("clarify.v1"); got != "" {
		t.Fatalf("dotted name must be rejected: got %q", got)
	}
	if got := sanitizeAgentPath("Approve需求澄清视觉研发"); got != "Approve需求澄清视觉研发" {
		t.Fatalf("unicode path: got %q", got)
	}
	if sanitizeAgentPath("has space") != "" {
		t.Fatal("spaces must be rejected on path layer")
	}
	if sanitizeAgentPath("a/b") != "b" && sanitizeAgentPath("a/b") != "" {
		t.Fatalf("unexpected path sanitize for a/b: %q", sanitizeAgentPath("a/b"))
	}
	if sanitizeAgentPath("..") != "" {
		t.Fatal(".. must be rejected")
	}
}

func TestSave_rejectsWriteInvalidViaSanitize(t *testing.T) {
	t.Parallel()
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{Name: "bad name"}); err == nil {
		t.Fatal("expected invalid agent name")
	}
}
