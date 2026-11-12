package models

import (
	"errors"
	"testing"
)

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
			if _, err := NormalizeGitScope(raw); !errors.Is(err, ErrGitScope) {
				t.Fatalf("scope %q accepted: %v", raw, err)
			}
		})
	}
}
