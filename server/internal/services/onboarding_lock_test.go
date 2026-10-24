package services

import (
	"errors"
	"testing"
)

func TestProtectedOnboardingAgentDeleteAndRename(t *testing.T) {
	s := NewAgentService(t.TempDir())
	names := append(append([]string{}, OnboardingAgentNames...), OnboardingAgentNamesEN...)
	for _, name := range names {
		if err := s.Save(Agent{Name: name, AcpBackend: AcpBackendCursor}); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
		if err := s.Delete(name); !errors.Is(err, ErrProtectedOnboardingAgent) {
			t.Fatalf("delete %s: %v", name, err)
		}
		if _, ok := s.Get(name); !ok {
			t.Fatalf("%s was removed", name)
		}
		if err := s.Rename(name, "moved-"+name); !errors.Is(err, ErrProtectedOnboardingAgent) {
			t.Fatalf("rename %s: %v", name, err)
		}
		if _, ok := s.Get(name); !ok {
			t.Fatalf("%s was renamed", name)
		}
	}

	fresh := NewAgentService(t.TempDir())
	if err := fresh.Save(Agent{Name: "scratch", AcpBackend: AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Rename("scratch", "Deliver"); !errors.Is(err, ErrProtectedOnboardingAgent) {
		t.Fatalf("rename onto a missing canonical name: %v", err)
	}
	if _, ok := fresh.Get("scratch"); !ok {
		t.Fatal("custom agent was renamed")
	}
	if _, ok := fresh.Get("Deliver"); ok {
		t.Fatal("canonical name was created by rename")
	}
	if err := fresh.Rename("scratch", "still-scratch"); err != nil {
		t.Fatalf("rename custom: %v", err)
	}
	if err := fresh.Delete("still-scratch"); err != nil {
		t.Fatalf("delete custom: %v", err)
	}
	if fresh.Exists("still-scratch") {
		t.Fatal("custom agent still exists")
	}
}
