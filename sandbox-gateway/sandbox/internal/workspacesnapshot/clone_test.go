package workspacesnapshot

import (
	"os"
	"path/filepath"
	"testing"
)

// A workflow sandbox starts from `git clone`, which writes the symbolic ref
// refs/remotes/origin/HEAD, packed refs and peeled tags. Agents then branch,
// stash and leave dirty files. All of that must capture and restore exactly.
func TestWorkspaceSnapshotClonedRepositoryRoundTripsSymbolicRefs(t *testing.T) {
	upstream := t.TempDir()
	fixtureGit(t, upstream, "init", "--template=", "-b", "main", ".")
	fixtureFile(t, upstream, "tracked.txt", "upstream base")
	fixtureGit(t, upstream, "add", ".")
	fixtureGit(t, upstream, "commit", "-m", "upstream base")
	fixtureGit(t, upstream, "tag", "-a", "v1", "-m", "annotated fixture tag")
	fixtureGit(t, upstream, "branch", "feature")

	source := t.TempDir()
	fixtureGit(t, source, "clone", "--template=", upstream, "repo")
	repo := filepath.Join(source, "repo")
	if got := fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD"); got != "refs/remotes/origin/main" {
		t.Fatal("fixture clone did not create origin/HEAD", got)
	}
	fixtureGit(t, repo, "checkout", "-b", "work")
	fixtureFile(t, repo, "tracked.txt", "stashed edit")
	fixtureGit(t, repo, "stash")
	fixtureFile(t, repo, "tracked.txt", "dirty edit")
	fixtureFile(t, repo, "untracked.txt", "untracked bytes")
	fixtureGit(t, repo, "symbolic-ref", "refs/heads/alias", "refs/heads/work")

	captured := captureFixture(t, source, []string{"."}, nil, Limits{})
	if captured.err != nil {
		t.Fatal(captured.err)
	}
	if len(captured.manifest.Repositories) != 1 {
		t.Fatal(captured.manifest.Repositories)
	}
	symbolic := map[string]string{}
	for _, ref := range captured.manifest.Repositories[0].Refs {
		if ref.Target != "" {
			symbolic[ref.Name] = ref.Target
		}
	}
	if symbolic["refs/remotes/origin/HEAD"] != "refs/remotes/origin/main" || symbolic["refs/heads/alias"] != "refs/heads/work" {
		t.Fatal("symbolic refs were not represented", symbolic)
	}

	parent := t.TempDir()
	if _, err := materializeFixture(t, captured, parent, "restored"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "restored", "repo")
	for _, name := range []string{"HEAD", "refs/heads/main", "refs/heads/work", "refs/remotes/origin/main", "refs/remotes/origin/feature", "refs/tags/v1", "refs/stash"} {
		if got, want := fixtureGit(t, target, "rev-parse", name), fixtureGit(t, repo, "rev-parse", name); got != want {
			t.Fatal(name, got, want)
		}
	}
	for name, want := range map[string]string{"HEAD": "refs/heads/work", "refs/remotes/origin/HEAD": "refs/remotes/origin/main", "refs/heads/alias": "refs/heads/work"} {
		if got := fixtureGit(t, target, "symbolic-ref", name); got != want {
			t.Fatal(name, got, want)
		}
	}
	if got := fixtureGit(t, target, "show", "refs/stash:tracked.txt"); got != "stashed edit" {
		t.Fatal("stash contents changed", got)
	}
	for name, want := range map[string]string{"tracked.txt": "dirty edit", "untracked.txt": "untracked bytes"} {
		if data, err := os.ReadFile(filepath.Join(target, name)); err != nil || string(data) != want {
			t.Fatal(name, string(data), err)
		}
	}
}

func TestWorkspaceSnapshotManifestRefsRequireExactlyOneValidTarget(t *testing.T) {
	oid := "0123456789abcdef0123456789abcdef01234567"
	if !validRefs([]GitRef{{Name: "refs/heads/main", OID: oid}, {Name: "refs/remotes/origin/HEAD", Target: "refs/remotes/origin/main"}}, "sha1") {
		t.Fatal("direct and symbolic refs rejected")
	}
	for name, ref := range map[string]GitRef{
		"both oid and target": {Name: "refs/remotes/origin/HEAD", OID: oid, Target: "refs/heads/main"},
		"neither":             {Name: "refs/remotes/origin/HEAD"},
		"self target":         {Name: "refs/remotes/origin/HEAD", Target: "refs/remotes/origin/HEAD"},
		"non-ref target":      {Name: "refs/remotes/origin/HEAD", Target: "HEAD"},
		"unsafe target":       {Name: "refs/remotes/origin/HEAD", Target: "refs/../../config"},
		"wrong oid format":    {Name: "refs/heads/main", OID: oid[:12]},
	} {
		if validRefs([]GitRef{ref}, "sha1") {
			t.Fatal(name, "accepted")
		}
	}
	if validRefs([]GitRef{{Name: "refs/heads/main", OID: oid}, {Name: "refs/heads/main", Target: "refs/heads/other"}}, "sha1") {
		t.Fatal("duplicate ref accepted")
	}
}
