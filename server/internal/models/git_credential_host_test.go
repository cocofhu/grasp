package models

import (
	"errors"
	"testing"
)

func TestNormalizeGitCredentialHost(t *testing.T) {
	for _, tc := range []struct{ provider, input, want string }{
		{"gitlab", "", "https://gitlab.com"}, {"github", "", "https://github.com"},
		{"glab", "  GIT.Example.COM.:8443/  ", "https://git.example.com:8443"},
		{"gh", "https://Git.Example.com/", "https://git.example.com"},
		{"gitlab", "http://git.internal:8080/", "http://git.internal:8080"},
		{"gitlab", "https://[2001:db8::1]:8443/", "https://[2001:db8::1]:8443"},
	} {
		got, err := NormalizeGitCredentialHost(tc.provider, tc.input)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %q", tc.input, got, err, tc.want)
		}
	}
	for _, input := range []string{
		"https://", "ftp://gitlab.com", "https:///missing", "https://gitlab.com/api/v4",
		"https://user:token@gitlab.com", "https://user@gitlab.com", "https://gitlab.com?token=secret",
		"https://gitlab.com?", "https://gitlab.com#", "https://gitlab.com/#fragment", "git lab.com", "https://gitlab.com:0",
		"https://gitlab.com:65536", "https://gitlab.com:", "https://-invalid.test", "https://gitlab.com//",
	} {
		if _, err := NormalizeGitCredentialHost("gitlab", input); !errors.Is(err, ErrGitCredentialHost) {
			t.Errorf("invalid host %q accepted: %v", input, err)
		}
	}
	if _, err := NormalizeGitCredentialHost("unknown", ""); !errors.Is(err, ErrGitCredentialHost) {
		t.Fatal("unknown provider received an implicit host")
	}
}
