package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func TestWorkspaceCaptureOriginalReceiptSurvivesStoppedOrRevokedSource(t *testing.T) {
	remote := snapshotArchiveFixture(t)
	p, objects, _, revoked, sandboxID := publicationFixture(t, remote)
	allowed, offline, resolutions := true, false, 0
	readScope := func(_ context.Context, _ *gorm.DB, actor, kind, id string) error {
		if !allowed || actor != "alice" || kind != "solo" || id != "solo-original" {
			return gorm.ErrRecordNotFound
		}
		return nil
	}
	p.Jobs.options.ReadScope = readScope
	catalog, err := NewWorkspaceSnapshotCatalog(p.db, readScope)
	if err != nil {
		t.Fatal(err)
	}
	binding := WorkspaceActiveBinding{SandboxID: sandboxID, Fingerprint: strings.Repeat("a", 64), Reference: sandbox.WorkspaceBindingReference{ID: "original-binding", Revision: 1, Fingerprint: strings.Repeat("b", 64)}}
	service, err := NewWorkspaceCaptureService(p.db, p, catalog, WorkspaceCaptureOptions{ResolveBinding: func(context.Context, string, string, string, uint) (WorkspaceActiveBinding, error) {
		resolutions++
		if offline {
			return WorkspaceActiveBinding{}, errors.New("native environment is stopped")
		}
		return binding, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	in := WorkspaceCaptureInput{ClientRequestID: "capture-original-request", ScopeKind: "solo", ScopeID: "solo-original", SandboxID: sandboxID, ExpectedBinding: binding.Fingerprint, Paths: []string{"."}, Duration: 20 * time.Second}
	first, err := service.Admit(t.Context(), "alice", in)
	if err != nil || first.ID == "" || first.State != "capturing" || resolutions != 1 {
		t.Fatalf("fresh capture did not record one bound intent: %+v %v", first, err)
	}
	var snapshot models.WorkspaceSnapshot
	if err := p.db.First(&snapshot, "id = ?", first.ID).Error; err != nil || snapshot.SourceBindingFingerprint != binding.Fingerprint {
		t.Fatal("logical source was not frozen in the admission transaction", snapshot.SourceBindingFingerprint, err)
	}
	offline, remote.capsUnavailable = true, true
	revoked.Store(true)
	beforePuts, beforeGets := objects.puts.Load(), objects.gets.Load()
	retry, err := service.Admit(t.Context(), "alice", in)
	if err != nil || retry.ID != first.ID || resolutions != 1 || objects.puts.Load() != beforePuts || objects.gets.Load() != beforeGets {
		t.Fatal("original receipt required execution/provider/storage access", retry, err, resolutions)
	}
	variants := []WorkspaceCaptureInput{in, in, in, in}
	variants[0].Duration += time.Second
	variants[1].ExpectedBinding = strings.Repeat("c", 64)
	variants[2].Paths = []string{"different"}
	variants[3].SandboxID++
	for _, variant := range variants {
		if _, err := service.Admit(t.Context(), "alice", variant); !errors.Is(err, ErrWorkspaceJobConflict) {
			t.Fatal("same nonce accepted a different complete request", variant, err)
		}
	}
	if resolutions != 1 {
		t.Fatal("conflicting receipt read contacted the current provider", resolutions)
	}
	allowed = false
	if _, err := service.Admit(t.Context(), "alice", in); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("read permission withdrawal exposed an old receipt", err)
	}
	var jobs int64
	if err := p.db.Model(&models.WorkspaceSnapshotJob{}).Count(&jobs).Error; err != nil || jobs != 1 || remote.creates != 0 {
		t.Fatal("HTTP admission or receipt replay dispatched native work", jobs, remote.creates, err)
	}
}

func TestWorkspaceCaptureRequiresCurrentCanonicalBindingAndSeparateReadPolicy(t *testing.T) {
	remote := snapshotArchiveFixture(t)
	p, _, _, _, sandboxID := publicationFixture(t, remote)
	readScope := func(context.Context, *gorm.DB, string, string, string) error { return nil }
	catalog, err := NewWorkspaceSnapshotCatalog(p.db, readScope)
	if err != nil {
		t.Fatal(err)
	}
	options := WorkspaceCaptureOptions{ResolveBinding: func(context.Context, string, string, string, uint) (WorkspaceActiveBinding, error) {
		return WorkspaceActiveBinding{SandboxID: sandboxID, Fingerprint: strings.Repeat("a", 64), Reference: sandbox.WorkspaceBindingReference{ID: "binding-current", Revision: 2, Fingerprint: strings.Repeat("b", 64)}}, nil
	}}
	if _, err := NewWorkspaceCaptureService(p.db, p, catalog, options); !errors.Is(err, ErrWorkspaceJobAuthority) {
		t.Fatal("missing metadata scope policy became a permissive product API", err)
	}
	p.Jobs.options.ReadScope = readScope
	s, err := NewWorkspaceCaptureService(p.db, p, catalog, options)
	if err != nil {
		t.Fatal(err)
	}
	in := WorkspaceCaptureInput{ClientRequestID: "wrong-binding-request", ScopeKind: "solo", ScopeID: "solo-original", SandboxID: sandboxID, ExpectedBinding: strings.Repeat("c", 64), Paths: []string{"."}, Duration: time.Minute}
	if _, err := s.Admit(t.Context(), "alice", in); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("stale logical binding was silently replaced by current root", err)
	}
	if _, err := p.Capture(t.Context(), "alice", in); !errors.Is(err, ErrWorkspaceJobInvalid) {
		t.Fatal("a new bound capture lacked the platform native reference", err)
	}
	var jobs int64
	if err := p.db.Model(&models.WorkspaceSnapshotJob{}).Count(&jobs).Error; err != nil || jobs != 0 {
		t.Fatal("rejected stale source created a job", jobs, err)
	}
}
