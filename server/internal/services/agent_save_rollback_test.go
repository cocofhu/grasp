package services

import (
	"errors"
	"testing"
)

// conflictingFiles passes validation but fails on write: "a" is written as a
// file, then "a/b" needs "a" to be a directory.
var conflictingFiles = []AgentFile{{Path: "a", Content: "x"}, {Path: "a/b", Content: "y"}}

func TestSave_validationFailureTouchesNoDisk(t *testing.T) {
	s := NewAgentService(t.TempDir())
	err := s.Save(Agent{Name: "probe", Files: []AgentFile{}})
	if !errors.Is(err, ErrInvalidAcpBackend) {
		t.Fatalf("want ErrInvalidAcpBackend, got %v", err)
	}
	if s.Exists("probe") {
		t.Fatal("agent dir created before acpBackend validation")
	}
}

func TestSave_writeFailureRollsBackNewAgent(t *testing.T) {
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{Name: "probe", AcpBackend: AcpBackendCursor, Files: conflictingFiles}); err == nil {
		t.Fatal("want a write error")
	}
	if s.Exists("probe") {
		t.Fatal("half-written new agent left on disk")
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List() = %v, want empty", got)
	}
}

func TestSave_writeFailureKeepsExistingAgent(t *testing.T) {
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{Name: "keep", AcpBackend: AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Agent{Name: "keep", AcpBackend: AcpBackendCursor, Files: conflictingFiles}); err == nil {
		t.Fatal("want a write error")
	}
	if !s.Exists("keep") {
		t.Fatal("failed update deleted an existing agent")
	}
}

// SaveAgentWithVcs must validate before EnsureBaseline, which would otherwise
// commit a baseline revision for a save that is then rejected.
func TestSaveAgentWithVcs_validationFailureRecordsNothing(t *testing.T) {
	s := NewAgentService(t.TempDir())
	if err := s.Save(Agent{Name: "v1", AcpBackend: AcpBackendCursor}); err != nil {
		t.Fatal(err)
	}
	_, err := s.SaveAgentWithVcs(Agent{Name: "v1", AcpBackend: AcpBackendCursor,
		CredentialBindings: map[string]*string{" padded": nil}}, "u", "r", true)
	revs, listErr := s.Vcs.ListRevisions("v1", 10)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(revs) != 0 {
		t.Fatalf("rejected save recorded %d revisions", len(revs))
	}
	if !errors.Is(err, ErrInvalidAgent) {
		t.Fatalf("want ErrInvalidAgent, got %v", err)
	}
}
