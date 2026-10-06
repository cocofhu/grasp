package services

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestNoteLiveCodexAuth(t *testing.T) {
	s := &SandboxService{live: map[uint]*liveSandbox{
		1: {codexAuthPath: "/root/.codex/auth.json"},
		2: {},
	}}
	rotated := "Rotated the refresh token and kept the session"
	s.noteLiveCodexAuth(1, &sandbox.ChatResult{ErrorText: rotated}, nil)
	if s.live[1].codexAuthRejected {
		t.Fatal("a successful mention of refresh token must not block writeback")
	}
	s.noteLiveCodexAuth(1, &sandbox.ChatResult{
		Failed:    true,
		ErrorText: "unexpected status 401 Unauthorized: Missing bearer, url: https://api.openai.com/v1/responses",
	}, nil)
	if !s.live[1].codexAuthRejected {
		t.Fatal("a 401 login failure must keep the previous credential")
	}
	s.noteLiveCodexAuth(1, &sandbox.ChatResult{}, nil)
	if s.live[1].codexAuthRejected {
		t.Fatal("a later successful turn must allow writeback again")
	}
	s.noteLiveCodexAuth(1, nil, errors.New("failed to refresh token"))
	if !s.live[1].codexAuthRejected {
		t.Fatal("a refresh failure error must block writeback")
	}
	s.noteLiveCodexAuth(2, &sandbox.ChatResult{Failed: true, ErrorText: "invalid_grant"}, nil)
	if s.live[2].codexAuthRejected {
		t.Fatal("non-codex sandboxes do not track login refusal")
	}
}
