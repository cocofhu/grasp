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
	t.Setenv("TMPDIR", t.TempDir())
	png := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	files, err := MaterializeAttachments([]PromptImage{
		{Data: png, MimeType: "image/png", Name: ".."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("files=%v", files)
	}
	assertInside(t, attachmentRoot(), files[0].Path)
}

func TestMaterializeAttachmentsImageAndFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	png := base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	txt := base64.StdEncoding.EncodeToString([]byte("hello"))
	files, err := MaterializeAttachments([]PromptImage{
		{Data: png, MimeType: "image/png", Name: "a.png"},
		{Data: txt, MimeType: "text/plain", Name: "b.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files=%v", files)
	}
	for _, f := range files {
		if !filepath.IsAbs(f.Path) {
			t.Fatalf("want abs path, got %q", f.Path)
		}
		if _, err := os.Stat(f.Path); err != nil {
			t.Fatal(err)
		}
	}
	got := AppendAttachmentRefs("看一下", files)
	if !strings.HasPrefix(got, "看一下\n\n<attached_files>\n") || !strings.HasSuffix(got, "</attached_files>\n") {
		t.Fatalf("refs not fenced from user text: %q", got)
	}
	if !strings.Contains(got, files[0].Path+" (image/png)") || !strings.Contains(got, files[1].Path+" (text/plain)") {
		t.Fatalf("refs=%q", got)
	}
	if strings.Contains(got, "此前对话") {
		t.Fatalf("no history attachments expected: %q", got)
	}
}

func TestMaterializeAttachmentsStablePathByRef(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	raw := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d}
	img := PromptImage{Data: base64.StdEncoding.EncodeToString(raw), MimeType: "image/png", Name: "IMG_6293.png", Ref: "blob:892fe2a4a9814de3952c5d45d8aad778"}
	first, err := MaterializeAttachments([]PromptImage{img})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(attachmentRoot(), "892fe2a4a9814de3952c5d45d8aad778", "IMG_6293.png")
	if len(first) != 1 || first[0].Path != want {
		t.Fatalf("files=%v, want %s", first, want)
	}
	img.History = true
	again, err := MaterializeAttachments([]PromptImage{img})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 1 || again[0].Path != want || !again[0].History {
		t.Fatalf("second turn files=%v", again)
	}
	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("bytes=%v", got)
	}
}

func TestMaterializeAttachmentsInvalidRefFallsBackToHash(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	data := base64.StdEncoding.EncodeToString([]byte("x"))
	files, err := MaterializeAttachments([]PromptImage{{Data: data, MimeType: "image/png", Name: "a.png", Ref: "blob:../../etc"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.Contains(files[0].Path, string(filepath.Separator)+"sha256-") {
		t.Fatalf("files=%v", files)
	}
	assertInside(t, attachmentRoot(), files[0].Path)
}

func TestMaterializeAttachmentsDuplicateNamesDoNotCollide(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	first := []byte{0x89, 0x50, 0x4e, 0x47, 0x01}
	second := []byte{0x89, 0x50, 0x4e, 0x47, 0x02}
	files, err := MaterializeAttachments([]PromptImage{
		{Data: base64.StdEncoding.EncodeToString(first), MimeType: "image/png", Name: "image.png"},
		{Data: base64.StdEncoding.EncodeToString(second), MimeType: "image/png", Name: "image.png"},
		{Data: base64.StdEncoding.EncodeToString(first), MimeType: "image/png", Name: "image.png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path == files[1].Path {
		t.Fatalf("files=%v", files)
	}
	for i, want := range [][]byte{first, second} {
		if filepath.Base(files[i].Path) != "image.png" {
			t.Fatalf("path %d = %q", i, files[i].Path)
		}
		got, err := os.ReadFile(files[i].Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("file %d bytes = %v, want %v", i, got, want)
		}
	}
	refs := AppendAttachmentRefs("图一和图二", files)
	if strings.Contains(refs, base64.StdEncoding.EncodeToString(first)) {
		t.Fatalf("refs inlined image bytes: %q", refs)
	}
	if strings.Index(refs, files[0].Path) > strings.Index(refs, files[1].Path) {
		t.Fatalf("paths out of order in %q", refs)
	}
}

func TestAppendAttachmentRefsSeparatesHistory(t *testing.T) {
	got := AppendAttachmentRefs("你看看图片内容啊", []Attachment{
		{Path: "/tmp/sbx-attach/old/IMG_6293.png", MimeType: "image/png", History: true},
		{Path: "/tmp/sbx-attach/new/b.txt", MimeType: "text/plain"},
	})
	cur := strings.Index(got, "本轮用户上传的附件：")
	hist := strings.Index(got, "此前对话中上传的附件：")
	if cur < 0 || hist < cur {
		t.Fatalf("groups missing or out of order: %q", got)
	}
	if p := strings.Index(got, "/tmp/sbx-attach/old/IMG_6293.png"); p < hist {
		t.Fatalf("history path not under history group: %q", got)
	}
	if p := strings.Index(got, "/tmp/sbx-attach/new/b.txt"); p < cur || p > hist {
		t.Fatalf("current path not under current group: %q", got)
	}
	if AppendAttachmentRefs("hi", nil) != "hi" {
		t.Fatal("no attachments should leave text untouched")
	}
}

func assertInside(t *testing.T, dir, path string) {
	t.Helper()
	rel, err := filepath.Rel(dir, path)
	if err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("path escaped attachment root: %q (rel=%q err=%v)", path, rel, err)
	}
}
