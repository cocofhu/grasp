package workspacefiles

import "testing"

func TestValidRefsAcceptsClonedSymbolicRefs(t *testing.T) {
	oid := "0123456789abcdef0123456789abcdef01234567"
	refs, ok := validRefs([]GitRef{
		{Name: "refs/heads/main", OID: oid},
		{Name: "refs/remotes/origin/main", OID: oid},
		{Name: "refs/remotes/origin/HEAD", Target: "refs/remotes/origin/main"},
		{Name: "refs/heads/alias", Target: "refs/heads/main"},
	}, "sha1")
	if !ok {
		t.Fatal("ordinary clone refs rejected")
	}
	if got := resolveRef(refs, "refs/heads/alias"); got != oid {
		t.Fatal("symbolic branch did not resolve", got)
	}
	if got := resolveRef(refs, "refs/heads/missing"); got != "" {
		t.Fatal("dangling ref resolved", got)
	}
}

func TestValidRefsRequireExactlyOneValidTarget(t *testing.T) {
	oid := "0123456789abcdef0123456789abcdef01234567"
	for name, ref := range map[string]GitRef{
		"both oid and target": {Name: "refs/remotes/origin/HEAD", OID: oid, Target: "refs/heads/main"},
		"neither":             {Name: "refs/remotes/origin/HEAD"},
		"self target":         {Name: "refs/remotes/origin/HEAD", Target: "refs/remotes/origin/HEAD"},
		"non-ref target":      {Name: "refs/remotes/origin/HEAD", Target: "HEAD"},
		"unsafe target":       {Name: "refs/remotes/origin/HEAD", Target: "refs/../../config"},
		"wrong oid format":    {Name: "refs/heads/main", OID: oid[:12]},
	} {
		if _, ok := validRefs([]GitRef{ref}, "sha1"); ok {
			t.Fatal(name, "accepted")
		}
	}
	if _, ok := validRefs([]GitRef{{Name: "refs/heads/main", OID: oid}, {Name: "refs/heads/main", Target: "refs/heads/other"}}, "sha1"); ok {
		t.Fatal("duplicate ref accepted")
	}
	cycle := map[string]GitRef{"refs/heads/a": {Name: "refs/heads/a", Target: "refs/heads/b"}, "refs/heads/b": {Name: "refs/heads/b", Target: "refs/heads/a"}}
	if got := resolveRef(cycle, "refs/heads/a"); got != "" {
		t.Fatal("cyclic ref resolved", got)
	}
}
