package workspacesnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceSnapshotLargeBinaryExceedsCommitImportLimitAndRestoresExactly(t *testing.T) {
	source := t.TempDir()
	filename := filepath.Join(source, "large.bin")
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64<<10)
	var seed uint64 = 0x7e21394714528f31
	want := sha256.New()
	for range 544 { // 34 MiB: larger than P4's 32-MiB immutable-file limit
		for i := range buffer {
			seed ^= seed << 13
			seed ^= seed >> 7
			seed ^= seed << 17
			buffer[i] = byte(seed)
		}
		if _, err := file.Write(buffer); err != nil {
			t.Fatal(err)
		}
		_, _ = want.Write(buffer)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	captured := captureFixture(t, source, []string{"."}, nil, Limits{})
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	if captured.receipt.Bytes != 34<<20 || captured.manifest.Files[0].SHA256 != hex.EncodeToString(want.Sum(nil)) {
		t.Fatal("binary file was truncated or reused the P4 quota")
	}
	parent := t.TempDir()
	if _, err := materializeFixture(t, captured, parent, "binary-copy"); err != nil {
		t.Fatal(err)
	}
	actual, err := os.Open(filepath.Join(parent, "binary-copy", "large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = actual.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, actual); err != nil || hex.EncodeToString(hash.Sum(nil)) != hex.EncodeToString(want.Sum(nil)) {
		t.Fatal("restored binary bytes differ", err)
	}
}

func TestWorkspaceSnapshotMergeConflictStagesAndPendingMergeArePreserved(t *testing.T) {
	source := t.TempDir()
	fixtureGit(t, source, "init", "--template=", ".")
	fixtureFile(t, source, "conflict.txt", "base\n")
	fixtureGit(t, source, "add", ".")
	fixtureGit(t, source, "commit", "-m", "base fixture")
	branch := fixtureGit(t, source, "symbolic-ref", "--short", "HEAD")
	fixtureGit(t, source, "checkout", "-b", "other")
	fixtureFile(t, source, "conflict.txt", "other\n")
	fixtureGit(t, source, "commit", "-am", "other fixture")
	fixtureGit(t, source, "checkout", branch)
	fixtureFile(t, source, "conflict.txt", "main\n")
	fixtureGit(t, source, "commit", "-am", "main fixture")
	merge := exec.CommandContext(t.Context(), "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "merge", "other")
	merge.Dir = source
	if err := merge.Run(); err == nil {
		t.Fatal("fixture did not create a conflict")
	}
	index := fixtureGit(t, source, "ls-files", "--unmerged", "-z")
	if index == "" {
		t.Fatal("fixture has no conflict stages")
	}
	captured := captureFixture(t, source, []string{"."}, nil, Limits{})
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	parent := t.TempDir()
	if _, err := materializeFixture(t, captured, parent, "conflict-copy"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "conflict-copy")
	if got := fixtureGit(t, target, "ls-files", "--unmerged", "-z"); got != index {
		t.Fatal("index conflict stages were not retained")
	}
	for _, name := range []string{"MERGE_HEAD", "MERGE_MSG", "ORIG_HEAD"} {
		before, err := os.ReadFile(filepath.Join(source, ".git", name))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(target, ".git", name))
		if err != nil || string(before) != string(after) {
			t.Fatal("safe pending merge state lost", name, err)
		}
	}
	before, _ := os.ReadFile(filepath.Join(source, "conflict.txt"))
	after, _ := os.ReadFile(filepath.Join(target, "conflict.txt"))
	if string(before) != string(after) {
		t.Fatal("merge conflict working bytes changed")
	}
}

func TestWorkspaceSnapshotQuotaExternalLinksAndPrivateIgnoredOverrideFailClosed(t *testing.T) {
	for _, name := range []string{"quota", "external-link", "metadata-link", "rebase"} {
		t.Run(name, func(t *testing.T) {
			source := t.TempDir()
			limits := DefaultLimits()
			switch name {
			case "quota":
				fixtureFile(t, source, "keep.txt", strings.Repeat("x", 2048))
				limits.MaxFileBytes = 1024
			case "external-link":
				if err := os.Symlink("../outside", filepath.Join(source, "escape")); err != nil {
					t.Fatal(err)
				}
			case "metadata-link", "rebase":
				fixtureGit(t, source, "init", "--template=", ".")
				fixtureFile(t, source, "keep.txt", "business bytes")
				fixtureGit(t, source, "add", ".")
				fixtureGit(t, source, "commit", "-m", "base fixture")
				if name == "rebase" {
					fixtureFile(t, source, ".git/rebase-merge/git-rebase-todo", "exec must-not-execute\n")
				} else {
					if err := os.Symlink(t.TempDir(), filepath.Join(source, ".git", "objects", "external")); err != nil {
						t.Fatal(err)
					}
				}
			}
			captured := captureFixture(t, source, []string{"."}, nil, limits)
			if captured.err == nil {
				t.Fatal("unsafe/over-quota source was reported complete")
			}
			if _, err := os.Stat(filepath.Join(captured.control, "data", "capture-original", ManifestName)); !os.IsNotExist(err) {
				t.Fatal("failed capture published a verified manifest")
			}
			if _, err := captured.engine.NewSpec([]string{"."}, []string{".codex/auth.json"}); !errors.Is(err, ErrUnsafe) {
				t.Fatal("ignored opt-in bypassed mandatory private exclusions", err)
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatal("unsupported original source removed")
			}
		})
	}
}

func TestWorkspaceSnapshotArchiveOrManifestCorruptionCannotCreateTarget(t *testing.T) {
	source := t.TempDir()
	fixtureFile(t, source, "keep.txt", "immutable fixture")
	captured := captureFixture(t, source, []string{"."}, nil, Limits{})
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	parent := t.TempDir()
	bad := captured
	bad.json = append([]byte(nil), captured.json...)
	bad.json[len(bad.json)/2] ^= 1
	if _, err := materializeFixture(t, bad, parent, "bad-manifest"); !errors.Is(err, ErrIntegrity) {
		t.Fatal("changed manifest accepted", err)
	}
	var malicious Manifest
	if err := json.Unmarshal(captured.json, &malicious); err != nil {
		t.Fatal(err)
	}
	malicious.Files[0].Path = "../escape"
	bad.json, _ = json.Marshal(malicious)
	hash := sha256.Sum256(bad.json)
	bad.receipt.Manifest.SHA256, bad.receipt.Manifest.Bytes = hex.EncodeToString(hash[:]), int64(len(bad.json))
	if _, err := materializeFixture(t, bad, parent, "bad-path"); !errors.Is(err, ErrUnsafe) {
		t.Fatal("unsafe manifest accepted", err)
	}
	archive := filepath.Join(captured.control, "data", "capture-original", ArchiveName)
	file, err := os.OpenFile(archive, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{0}, 20); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := materializeFixture(t, captured, parent, "bad-archive"); err == nil {
		t.Fatal("corrupt archive accepted")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatal("target created before complete integrity validation", entries, err)
	}
}

func TestWorkspaceSnapshotDetectsSourceChangeAfterWalkWithoutRevertingExternalEdit(t *testing.T) {
	source := t.TempDir()
	fixtureGit(t, source, "init", "--template=", ".")
	fixtureFile(t, source, "tracked.txt", "original business bytes")
	fixtureGit(t, source, "add", ".")
	fixtureGit(t, source, "commit", "-m", "fixture base")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	wrapper := filepath.Join(t.TempDir(), "git-fixture")
	script := "#!/bin/sh\nfor item do\nif [ \"$item\" = pack-objects ]; then printf '%s' 'external edit after scan' > " + quote(filepath.Join(source, "tracked.txt")) + "; fi\ndone\nexec " + quote(git) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	captured := captureFixture(t, source, []string{"."}, nil, Limits{}, func(cfg *Config) { cfg.GitBinary = wrapper })
	if !errors.Is(captured.err, ErrChanged) {
		t.Fatal("source changed after manifest walk but capture was accepted", captured.err)
	}
	if data, _ := os.ReadFile(filepath.Join(source, "tracked.txt")); string(data) != "external edit after scan" {
		t.Fatal("external source edit was reverted")
	}
	if _, err := os.Stat(filepath.Join(captured.control, "data", "capture-original", ManifestName)); !os.IsNotExist(err) {
		t.Fatal("changed source published a final manifest")
	}
}

type failSecondSnapshotSeek struct {
	io.ReadSeeker
	seeks int
}

func (r *failSecondSnapshotSeek) Seek(offset int64, whence int) (int64, error) {
	r.seeks++
	if r.seeks == 2 {
		return 0, io.ErrUnexpectedEOF
	}
	return r.ReadSeeker.Seek(offset, whence)
}

func TestWorkspaceSnapshotPartialMaterializationAndLaterEditsAreRetained(t *testing.T) {
	source := t.TempDir()
	fixtureFile(t, source, "keep.txt", "source retained")
	captured := captureFixture(t, source, []string{"."}, nil, Limits{})
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	parent := t.TempDir()
	_, err := materializeFixture(t, captured, parent, "partial-copy", func(reader io.ReadSeeker) io.ReadSeeker { return &failSecondSnapshotSeek{ReadSeeker: reader} })
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("failure after exclusive target creation was not surfaced", err)
	}
	if info, err := os.Stat(filepath.Join(parent, "partial-copy")); err != nil || !info.IsDir() {
		t.Fatal("partial target was removed", err)
	}
	fixtureFile(t, filepath.Join(parent, "partial-copy"), "later.txt", "later user edit")
	if _, err := materializeFixture(t, captured, parent, "partial-copy"); !errors.Is(err, os.ErrExist) {
		t.Fatal("retry overlaid a retained partial target", err)
	}
	if data, _ := os.ReadFile(filepath.Join(parent, "partial-copy", "later.txt")); string(data) != "later user edit" {
		t.Fatal("later edit was changed")
	}
	if data, _ := os.ReadFile(filepath.Join(source, "keep.txt")); string(data) != "source retained" {
		t.Fatal("source was changed")
	}
}
