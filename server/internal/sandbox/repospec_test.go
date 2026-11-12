package sandbox

import (
	"testing"
)

func TestRepoNameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://git.host.cc/org/web.git":  "web",
		"https://git.host.cc/org/web":      "web",
		"git@github.com:org/api.git":       "api",
		"ssh://git@host/org/infra.git/":    "infra",
		"https://git.host.cc/org/web.git/": "web",
		"":                                 "",
		"   ":                              "",
	}
	for in, want := range cases {
		if got := RepoNameFromURL(in); got != want {
			t.Errorf("RepoNameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeRepos(t *testing.T) {
	// name|url with an optional |branch, comma-separated.
	got := EncodeRepos([]RepoSpec{
		{Name: "web", URL: "https://h/w.git", Branch: "dev"},
		{Name: "api", URL: "https://h/a.git"},
	})
	want := "web|https://h/w.git|dev,api|https://h/a.git"
	if got != want {
		t.Errorf("EncodeRepos = %q, want %q", got, want)
	}

	// Entries missing a name or URL are skipped; surrounding whitespace trimmed.
	got2 := EncodeRepos([]RepoSpec{
		{Name: " web ", URL: " https://h/w.git "},
		{Name: "", URL: "https://h/x.git"},
		{Name: "api", URL: ""},
	})
	if got2 != "web|https://h/w.git" {
		t.Errorf("EncodeRepos (skip/trim) = %q", got2)
	}

	if got3 := EncodeRepos(nil); got3 != "" {
		t.Errorf("nil repos should encode to empty, got %q", got3)
	}
}

func TestDecodeRepos(t *testing.T) {
	got := DecodeRepos("web|https://h/w.git|dev,api|https://h/a.git")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
	if got[0] != (RepoSpec{Name: "web", URL: "https://h/w.git", Branch: "dev"}) {
		t.Errorf("entry0 = %+v", got[0])
	}
	if got[1] != (RepoSpec{Name: "api", URL: "https://h/a.git"}) {
		t.Errorf("entry1 = %+v", got[1])
	}

	// Blank name derives from URL; unsafe / duplicate names dropped.
	got2 := DecodeRepos("|https://h/org/demo.git|main,../etc|https://h/x.git,demo|https://h/org/demo.git|main")
	if len(got2) != 1 || got2[0].Name != "demo" || got2[0].Branch != "main" {
		t.Errorf("derive/dedupe/unsafe = %+v", got2)
	}

	if DecodeRepos("") != nil || DecodeRepos("   ") != nil || DecodeRepos("not-a-wire") != nil {
		t.Errorf("blank/invalid wire should be nil")
	}

	// Round-trip with EncodeRepos.
	wire := "demo|https://github.com/heroku/nodejs-getting-started.git|main"
	if enc := EncodeRepos(DecodeRepos(wire)); enc != wire {
		t.Errorf("round-trip = %q, want %q", enc, wire)
	}
}
