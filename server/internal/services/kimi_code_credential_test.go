package services

import (
	"errors"
	"testing"
)

func TestKimiCodeRejectsOAuthLoginFile(t *testing.T) {
	setCredentialKey(t)
	service := NewCredentialService(newTestDB(t))
	plain, err := service.Create(CredentialInput{
		Purpose: "tool_auth", ToolID: "kimi_code", Name: "Platform", Value: "sk-kimi-plain-test",
	})
	if err != nil || plain.EnvKey != "GRASP_KIMI_CODE_API_KEY" || plain.SecretKind != "api_key" {
		t.Fatalf("plain key: %+v %v", plain, err)
	}
	login := `{"access_token":"tok","refresh_token":"ref"}`
	if _, err := service.Create(CredentialInput{Purpose: "tool_auth", ToolID: "kimi_code", Name: "Login", Value: login}); !errors.Is(err, ErrKimiCodeAPIKeyOnly) {
		t.Fatalf("login file accepted on create: %v", err)
	}
	nested := `{"credentials":[{"refresh_token":"ref"}]}`
	if _, err := service.Update(plain.ID, CredentialInput{Value: nested}); !errors.Is(err, ErrKimiCodeAPIKeyOnly) {
		t.Fatalf("nested login file accepted on update: %v", err)
	}
	if _, err := service.Update(plain.ID, CredentialInput{Value: "sk-rotated-kimi-test"}); err != nil {
		t.Fatal(err)
	}
}
