package models

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeGitBindingsExplicitSelection(t *testing.T) {
	if got, err := NormalizeGitBindings(nil); got == nil || got.Items == nil || len(got.Items) != 0 || err != nil {
		t.Fatalf("omitted selection = %+v, %v", got, err)
	}
	empty, err := NormalizeGitBindings(&GitBindings{})
	if err != nil || empty == nil || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("explicit empty = %+v, %v", empty, err)
	}
	in := &GitBindings{Items: []GitCredentialBinding{{CredentialID: " a ", Scope: " GIT.Example.com:8443 ", SshHostsCredentialID: " hosts "}, {CredentialID: "b"}}, PreferredCredentialID: " b "}
	out, err := NormalizeGitBindings(in)
	if err != nil || out.PreferredCredentialID != "b" || out.Items[0].CredentialID != "a" || out.Items[0].Scope != "git.example.com:8443" || out.Items[0].SshHostsCredentialID != "hosts" {
		t.Fatalf("normalized = %+v, %v", out, err)
	}
	if in.Items[0].CredentialID != " a " {
		t.Fatal("normalization mutated the caller")
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip GitBindings
	if err := json.Unmarshal(raw, &roundtrip); err != nil || !reflect.DeepEqual(out, &roundtrip) {
		t.Fatalf("roundtrip = %+v, %v", roundtrip, err)
	}
	for _, bad := range []*GitBindings{
		{PreferredCredentialID: "a"},
		{Items: []GitCredentialBinding{{CredentialID: "a"}}},
		{Items: []GitCredentialBinding{{CredentialID: "a"}}, PreferredCredentialID: "other"},
		{Items: []GitCredentialBinding{{CredentialID: " "}}, PreferredCredentialID: "a"},
		{Items: []GitCredentialBinding{{CredentialID: "a"}, {CredentialID: " a "}}, PreferredCredentialID: "a"},
		{Items: []GitCredentialBinding{{CredentialID: "a", Scope: "https://user:secret@host/repo"}}, PreferredCredentialID: "a"},
	} {
		if _, err := NormalizeGitBindings(bad); !errors.Is(err, ErrGitBindings) {
			t.Fatalf("accepted bad selection %+v: %v", bad, err)
		}
	}
}

func TestNormalizeGitScope(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""}, {" Example.COM. ", "example.com"}, {"localhost", "localhost"}, {"127.0.0.1:1234", "127.0.0.1:1234"}, {"[::1]", "[::1]"}, {"[::1]:2222", "[::1]:2222"},
		{"https://GIT.Example.com/", "https://git.example.com"}, {"http://host:8080", "http://host:8080"},
		{"https://GIT.Example.com/group/repo.git/", "https://git.example.com/group/repo.git"},
		{"ssh://git@GIT.Example.com:2222/group/repo.git", "ssh://git@git.example.com:2222/group/repo.git"},
		{"ssh://host/repo", "ssh://host/repo"}, {"git@GIT.Example.com:group/repo.git/", "git@git.example.com:group/repo.git"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := NormalizeGitScope(tc.in)
			if err != nil || got != tc.want {
				t.Fatalf("scope %q = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}
	for _, raw := range []string{
		"host\nother", "host\tother", "host name", "host\\path", "host?token=x", "host#ref", "host/path", "https://host/repo?x=1", "https://host/repo#secret",
		"https://user@host/repo", "ssh://git:secret@host/repo", "ssh://bad!user@host/repo", "ssh://@host/repo", "git@:path", "@host:path", "git@host:", "git@host:path//repo", "git@host:../repo",
		"https://host/../repo", "https://host/a//repo", "https://host/a/./repo", "https://host/a/%0Arepo", "https://host/%5Crepo", "https://host/%zz", "ftp://host/repo", "https:///repo", "https://-bad/repo", "https://host:bad/repo",
		"host:", "host:0", "host:65536", "host:bad", "-host", "host-", ".host", "host..example", "host!", "https://host/./", "git@bad!host:path",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NormalizeGitScope(raw); !errors.Is(err, ErrGitBindings) {
				t.Fatalf("scope %q accepted: %v", raw, err)
			}
		})
	}
}
