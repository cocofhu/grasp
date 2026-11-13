package services

import (
	"context"
	"testing"
)

func TestManualGitHubTokenRecordsAccountForGitHubDotComOnly(t *testing.T) {
	setCredentialKey(t)
	s := NewCredentialService(newTestDB(t))
	var asked []string
	s.SetGitHubLoginLookup(func(ctx context.Context, token string) string {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("account lookup must be bounded")
		}
		asked = append(asked, token)
		if token == "ghp_unknown" {
			return ""
		}
		return "login-of-" + token
	})

	key, err := s.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "Mine", Value: " ghp_first \n"})
	if err != nil || key.Metadata["githubLogin"] != "login-of-ghp_first" {
		t.Fatalf("manual github.com token: %+v, %v", key.Metadata, err)
	}
	if value, ok := s.ResolveGitValue(key.ID); !ok || value.Login != "login-of-ghp_first" {
		t.Fatalf("resolved login = %q", value.Login)
	}
	if key, err = s.Update(key.ID, CredentialInput{Name: "Renamed"}); err != nil || key.Metadata["githubLogin"] != "login-of-ghp_first" {
		t.Fatalf("rename kept account: %+v, %v", key.Metadata, err)
	}
	if key, err = s.Update(key.ID, CredentialInput{Value: "ghp_second"}); err != nil || key.Metadata["githubLogin"] != "login-of-ghp_second" {
		t.Fatalf("rotation looks the account up again: %+v, %v", key.Metadata, err)
	}
	if key, err = s.Update(key.ID, CredentialInput{Value: "ghp_unknown"}); err != nil || key.Metadata["githubLogin"] != nil {
		t.Fatalf("failed lookup must save without the previous account: %+v, %v", key.Metadata, err)
	}
	if len(asked) != 3 {
		t.Fatalf("lookups = %v", asked)
	}

	asked = nil
	for _, in := range []CredentialInput{
		{Purpose: "git", Provider: "github", Host: "https://ghe.example", Name: "Enterprise", Value: "ghp_enterprise"},
		{Purpose: "git", Provider: "gitlab", Name: "GitLab", Value: "glpat_synthetic"},
	} {
		if key, err := s.Create(in); err != nil || key.Metadata["githubLogin"] != nil {
			t.Fatalf("%s: %+v, %v", in.Name, key.Metadata, err)
		}
	}
	if len(asked) != 0 {
		t.Fatalf("only github.com tokens may be sent to the lookup: %v", asked)
	}
}

func TestGitHubDeviceFlowServiceLooksUpManualTokenAccount(t *testing.T) {
	svc, creds, _, _ := newDeviceFlowFixture(t, "client")
	creds.SetGitHubLoginLookup(svc.LookupLogin)
	key, err := creds.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "Mine", Value: "gho_synthetic_token"})
	if err != nil || key.Metadata["githubLogin"] != "octocat" || key.Metadata["authMethod"] != nil {
		t.Fatalf("metadata = %+v, %v", key.Metadata, err)
	}
	if key, err = creds.Create(CredentialInput{Purpose: "git", Provider: "github", Name: "Rejected", Value: "ghp_rejected"}); err != nil || key.Metadata["githubLogin"] != nil {
		t.Fatalf("rejected token: %+v, %v", key.Metadata, err)
	}
}
