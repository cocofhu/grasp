package engine

import "testing"

func TestTaskPreview(t *testing.T) {
	exact := ""
	for i := 0; i < TaskPreviewRunes; i++ {
		exact += "字"
	}
	if got := TaskPreview("  " + exact + "\n"); got != exact {
		t.Fatalf("280 runes must not be cut: %d", len([]rune(got)))
	}
	if got := TaskPreview(exact + "多"); got != exact+"…" {
		t.Fatalf("281 runes: %q", got)
	}
	if TaskPreview("   ") != "" {
		t.Fatal("blank prompt → empty")
	}
}
