package runtime

import (
	"path/filepath"
	"testing"
)

func TestSafeProfileNameAndDir(t *testing.T) {
	t.Parallel()
	if safeProfileName("..") != "" {
		t.Fatal(".. must be rejected")
	}
	if safeProfileName("../evil") != "" && safeProfileName("../evil") == ".." {
		t.Fatal("traversal must be rejected")
	}
	if safeProfileName("good-agent") != "good-agent" {
		t.Fatalf("got %q", safeProfileName("good-agent"))
	}
	if safeProfileName("has spaces") != "" {
		t.Fatal("spaces must be rejected")
	}
	if safeProfileName("clarify.v1") != "" {
		t.Fatalf("dotted profile must be rejected: got %q", safeProfileName("clarify.v1"))
	}
	if safeProfileName("Approve需求澄清视觉研发") != "Approve需求澄清视觉研发" {
		t.Fatalf("chinese profile: got %q", safeProfileName("Approve需求澄清视觉研发"))
	}

	root := t.TempDir()
	dir, err := profileDir(root, "Approve需求澄清视觉研发")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(filepath.Join(root, "Approve需求澄清视觉研发"))
	if dir != want {
		t.Fatalf("got %q want %q", dir, want)
	}
	if _, err := profileDir(root, "clarify.v1"); err == nil {
		t.Fatal("expected error for dotted profile")
	}
	if _, err := profileDir(root, ".."); err == nil {
		t.Fatal("expected error for ..")
	}
}
