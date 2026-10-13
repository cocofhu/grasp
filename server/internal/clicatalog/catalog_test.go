package clicatalog

import "testing"

func TestEveryBackendHasAUsableList(t *testing.T) {
	for _, b := range []string{"cursor", "claude_code", "codex", "codebuddy", "trae"} {
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
