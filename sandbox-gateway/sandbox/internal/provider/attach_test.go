package provider

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAttachmentFileNameRejectsDotDot(t *testing.T) {
	name := AttachmentFileName(0, PromptImage{Name: "..", MimeType: "image/png"})
	if name == ".." || name == "." {
		t.Fatalf("AttachmentFileName(..) = %q, want fallback", name)
	}
	if name != "attachment-1.png" {
		t.Fatalf("got %q", name)
	}
}

func TestMaterializeAttachmentsRejectsDotDotName(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	dir, paths, err := MaterializeAttachments([]PromptImage{
		{Data: png, MimeType: "image/png", Name: ".."},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if len(paths) != 1 {
		t.Fatalf("paths=%v", paths)
	}
	rel, err := filepath.Rel(dir, paths[0])
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("path escaped temp dir: %q (rel=%q err=%v)", paths[0], rel, err)
	}
}

func TestMaterializeAttachmentsImageAndFile(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	txt := base64.StdEncoding.EncodeToString([]byte("hello"))
	dir, paths, err := MaterializeAttachments([]PromptImage{
		{Data: png, MimeType: "image/png", Name: "a.png"},
		{Data: txt, MimeType: "text/plain", Name: "b.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if len(paths) != 2 {
		t.Fatalf("paths=%v", paths)
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			t.Fatalf("want abs path, got %q", p)
		}
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}
	got := AppendAttachmentRefs("看一下", paths)
	if !strings.Contains(got, "看一下") || !strings.Contains(got, "a.png") || !strings.Contains(got, "b.txt") {
		t.Fatalf("refs=%q", got)
	}
}

func TestMaterializeAttachmentsKeepsSingleImageName(t *testing.T) {
	raw := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d}
	dir, paths, err := MaterializeAttachments([]PromptImage{
		{Data: base64.StdEncoding.EncodeToString(raw), MimeType: "image/png", Name: "image.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if len(paths) != 1 || filepath.Base(paths[0]) != "image.png" {
		t.Fatalf("paths=%v", paths)
	}
	got, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("bytes=%v", got)
	}
	assertInside(t, dir, paths[0])
}

func TestMaterializeAttachmentsDuplicateImagePNG(t *testing.T) {
	first := []byte{0x89, 0x50, 0x4e, 0x47, 0x01}
	second := []byte{0x89, 0x50, 0x4e, 0x47, 0x02}
	third := []byte{0x89, 0x50, 0x4e, 0x47, 0x03}
	dir, paths, err := MaterializeAttachments([]PromptImage{
		{Data: base64.StdEncoding.EncodeToString(first), MimeType: "image/png", Name: "image.png"},
		{Data: base64.StdEncoding.EncodeToString(second), MimeType: "image/png", Name: "image.png"},
		{Data: base64.StdEncoding.EncodeToString(third), MimeType: "image/png", Name: "image.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	wantNames := []string{"image.png", "image-2.png", "image-3.png"}
	wantBytes := [][]byte{first, second, third}
	if len(paths) != len(wantNames) {
		t.Fatalf("paths=%v", paths)
	}
	for i, p := range paths {
		if filepath.Base(p) != wantNames[i] {
			t.Fatalf("path %d = %q, want %s", i, p, wantNames[i])
		}
		assertInside(t, dir, p)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wantBytes[i]) {
			t.Fatalf("file %s bytes = %v, want %v", wantNames[i], got, wantBytes[i])
		}
	}
	if paths[0] == paths[1] || paths[1] == paths[2] {
		t.Fatalf("paths collided: %v", paths)
	}
	refs := AppendAttachmentRefs("图一和图二", paths)
	if !strings.Contains(refs, "用户上传了以下附件，请直接读取这些本地文件路径：") {
		t.Fatalf("refs=%q", refs)
	}
	prev := -1
	for _, p := range paths {
		idx := strings.Index(refs, p)
		if idx < 0 || idx <= prev {
			t.Fatalf("path %q missing or out of order in %q", p, refs)
		}
		prev = idx
	}
	if strings.Contains(refs, base64.StdEncoding.EncodeToString(first)) {
		t.Fatalf("refs inlined image bytes: %q", refs)
	}
}

func TestMaterializeAttachmentsDuplicateSkipsTakenSuffix(t *testing.T) {
	taken := []byte("taken")
	first := []byte("first-image")
	second := []byte("second-image")
	dir, paths, err := MaterializeAttachments([]PromptImage{
		{Data: base64.StdEncoding.EncodeToString(taken), MimeType: "image/png", Name: "image-2.png"},
		{Data: base64.StdEncoding.EncodeToString(first), MimeType: "image/png", Name: "image.png"},
		{Data: base64.StdEncoding.EncodeToString(second), MimeType: "image/png", Name: "image.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	want := []struct {
		name string
		raw  []byte
	}{
		{"image-2.png", taken},
		{"image.png", first},
		{"image-3.png", second},
	}
	if len(paths) != len(want) {
		t.Fatalf("paths=%v", paths)
	}
	for i, p := range paths {
		if filepath.Base(p) != want[i].name {
			t.Fatalf("path %d = %q, want %s", i, p, want[i].name)
		}
		assertInside(t, dir, p)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want[i].raw) {
			t.Fatalf("file %s bytes = %q, want %q", want[i].name, got, want[i].raw)
		}
	}
}

func assertInside(t *testing.T, dir, path string) {
	t.Helper()
	rel, err := filepath.Rel(dir, path)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("path escaped temp dir: %q (rel=%q err=%v)", path, rel, err)
	}
}
