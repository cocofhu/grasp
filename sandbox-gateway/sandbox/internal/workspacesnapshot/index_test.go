package workspacesnapshot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestWorkspaceSnapshotIndexVersionsFlagsAndIntentRoundTripWithNativeGit(t *testing.T) {
	for _, version := range []int{2, 3, 4} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			source := t.TempDir()
			fixtureGit(t, source, "init", "--template=", ".")
			fixtureFile(t, source, "assume.txt", "base")
			fixtureFile(t, source, "skip.txt", "base")
			fixtureGit(t, source, "add", ".")
			fixtureGit(t, source, "commit", "-m", "flags fixture")
			fixtureGit(t, source, "update-index", "--assume-unchanged", "assume.txt")
			if version > 2 {
				fixtureGit(t, source, "update-index", "--skip-worktree", "skip.txt")
				fixtureFile(t, source, "intent.txt", "intent working bytes")
				fixtureGit(t, source, "add", "--intent-to-add", "intent.txt")
			}
			fixtureGit(t, source, "update-index", "--index-version="+strconv.Itoa(version))
			before, err := os.ReadFile(filepath.Join(source, ".git", "index"))
			if err != nil || int(binary.BigEndian.Uint32(before[4:8])) != version {
				t.Fatal("native fixture did not use requested index version", err)
			}
			sourceEntries, _, err := decodeIndex(before, 100)
			if err != nil {
				t.Fatal("documented native index rejected", err)
			}
			captured := captureFixture(t, source, []string{"."}, nil, Limits{})
			if captured.err != nil {
				t.Fatal(captured.err)
			}
			parent := t.TempDir()
			if _, err := materializeFixture(t, captured, parent, "flags-copy"); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(parent, "flags-copy")
			after, err := os.ReadFile(filepath.Join(target, ".git", "index"))
			if err != nil {
				t.Fatal(err)
			}
			targetEntries, _, err := decodeIndex(after, 100)
			if err != nil || len(targetEntries) != len(sourceEntries) {
				t.Fatal("reconstructed index is invalid", err)
			}
			for i := range sourceEntries {
				if sourceEntries[i] != targetEntries[i] {
					t.Fatalf("logical index flags changed: %+v != %+v", sourceEntries[i], targetEntries[i])
				}
			}
			if got := fixtureGit(t, target, "diff", "--cached", "--name-only"); got != "" {
				t.Fatal("intent-to-add was incorrectly converted into a staged empty file", got)
			}
			current, err := os.ReadFile(filepath.Join(source, ".git", "index"))
			if err != nil || !bytes.Equal(before, current) {
				t.Fatal("capture refreshed source index", err)
			}
		})
	}
}

func TestWorkspaceSnapshotUnbornSHA256RepositoryRetainsObjectFormat(t *testing.T) {
	for _, staged := range []bool{false, true} {
		label := "untracked"
		if staged {
			label = "staged"
		}
		t.Run(label, func(t *testing.T) {
			source := t.TempDir()
			fixtureGit(t, source, "init", "--template=", "--object-format=sha256", ".")
			fixtureFile(t, source, "new.txt", "no implicit first commit")
			if staged {
				fixtureGit(t, source, "add", "new.txt")
			}
			captured := captureFixture(t, source, []string{"."}, nil, Limits{})
			if captured.err != nil {
				t.Fatal(captured.err)
			}
			parent := t.TempDir()
			if _, err := materializeFixture(t, captured, parent, "unborn-copy"); err != nil {
				t.Fatal(err)
			}
			if format := fixtureGit(t, filepath.Join(parent, "unborn-copy"), "rev-parse", "--show-object-format"); format != "sha256" {
				t.Fatal("unborn Git format changed", format)
			}
		})
	}
}

func TestWorkspaceSnapshotIndexDecoderRejectsTruncationAndUnsafePaths(t *testing.T) {
	for size := 1; size < 100; size++ {
		if _, _, err := decodeIndex(make([]byte, size), 10); err == nil {
			t.Fatal("invalid index accepted", size)
		}
	}
	if _, _, err := decodeIndex(nil, 10); err != nil {
		t.Fatal("missing unborn index must be supported", err)
	}
	if _, err := encodeIndex(nil, "unsupported", 100); !errors.Is(err, ErrGitUnsupported) {
		t.Fatal(err)
	}
}
