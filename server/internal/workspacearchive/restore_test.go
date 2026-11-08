package workspacearchive

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func TestRestoreNewDirectoryPreservesOriginalAndFileState(t *testing.T) {
	parent, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close() }()
	if err := parent.Mkdir("original", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := parent.WriteFile("original/dirty", []byte("original draft"), 0o600); err != nil {
		t.Fatal(err)
	}
	var canonical bytes.Buffer
	manifest, err := Canonicalize(context.Background(), bytes.NewReader(archive(t,
		member{name: "repo/.git/HEAD", body: "ref: refs/heads/work\n", mode: 0o644},
		member{name: "repo/dirty", body: "uncommitted work", mode: 0o644},
		member{name: "untracked.bin", body: "\x00\xff\x00", mode: 0o600},
		member{name: "non-git/run.sh", body: "#!/bin/sh\n", mode: 0o755},
		member{name: "symbolic", kind: tar.TypeSymlink, link: "repo/dirty"},
		member{name: "hard", kind: tar.TypeLink, link: "untracked.bin"},
		member{name: "restricted-directory", kind: tar.TypeDir, mode: 0},
	)), &canonical, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	if err := Restore(context.Background(), bytes.NewReader(canonical.Bytes()), parent, "restored", manifest, testLimits); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"original/dirty": "original draft", "restored/repo/dirty": "uncommitted work", "restored/untracked.bin": "\x00\xff\x00", "restored/repo/.git/HEAD": "ref: refs/heads/work\n"} {
		got, err := parent.ReadFile(name)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
	info, err := parent.Stat("restored/non-git/run.sh")
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("executable mode lost: %v %v", info, err)
	}
	link, err := parent.Readlink("restored/symbolic")
	if err != nil || link != "repo/dirty" {
		t.Fatalf("symbolic link lost: %q %v", link, err)
	}
	left, err := parent.Stat("restored/untracked.bin")
	if err != nil {
		t.Fatal(err)
	}
	right, err := parent.Stat("restored/hard")
	if err != nil || !os.SameFile(left, right) {
		t.Fatalf("hardlink identity lost: %v", err)
	}
	info, err = parent.Stat("restored/restricted-directory")
	if err != nil || info.Mode().Perm() != 0 {
		t.Fatalf("restrictive directory permissions changed: %v %v", info, err)
	}
	if err := Restore(context.Background(), bytes.NewReader(canonical.Bytes()), parent, "original", manifest, testLimits); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing workspace may not be replaced: %v", err)
	}
	got, err := parent.ReadFile("original/dirty")
	if err != nil || string(got) != "original draft" {
		t.Fatal("original draft changed")
	}
}

func TestRestoreRejectsMismatchAndRemovesOnlyFailedNewTarget(t *testing.T) {
	parent, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close() }()
	var one, two bytes.Buffer
	manifest, err := Canonicalize(context.Background(), bytes.NewReader(archive(t, member{name: "data", body: "first"})), &one, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Canonicalize(context.Background(), bytes.NewReader(archive(t, member{name: "data", body: "changed"})), &two, testLimits); err != nil {
		t.Fatal(err)
	}
	if err := Restore(context.Background(), bytes.NewReader(two.Bytes()), parent, "mismatch", manifest, testLimits); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("wrong object accepted: %v", err)
	}
	if _, err := parent.Lstat("mismatch"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unverified target was created")
	}
	changing := &changingReader{Reader: bytes.NewReader(one.Bytes()), replacement: two.Bytes()}
	if err := Restore(context.Background(), changing, parent, "interrupted", manifest, testLimits); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("changed stream accepted: %v", err)
	}
	if _, err := parent.Lstat("interrupted"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed partial restoration was published")
	}
	for _, name := range []string{"../outside", "/absolute", ".", "..", "a/b"} {
		if err := Restore(context.Background(), bytes.NewReader(one.Bytes()), parent, name, manifest, testLimits); !errors.Is(err, ErrUnsafe) {
			t.Fatalf("invalid target %q accepted: %v", name, err)
		}
	}
}

type changingReader struct {
	*bytes.Reader
	replacement []byte
	starts      int
}

func (r *changingReader) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekStart {
		r.starts++
		if r.starts == 2 {
			r.Reader = bytes.NewReader(r.replacement)
		}
	}
	return r.Reader.Seek(offset, whence)
}
