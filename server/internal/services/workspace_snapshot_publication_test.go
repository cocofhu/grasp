package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/workspacearchive"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
)

type snapshotObjects struct {
	mu         sync.Mutex
	objects    map[string][]byte
	infos      map[string]objectstore.Info
	puts, gets atomic.Int32
	beforePut  func(context.Context) error
	probe      func(context.Context) error
}

func (s *snapshotObjects) Put(ctx context.Context, key string, reader io.ReadSeeker, info objectstore.Info) error {
	s.puts.Add(1)
	if s.probe != nil {
		if err := s.probe(ctx); err != nil {
			return err
		}
	}
	if s.beforePut != nil {
		if err := s.beforePut(ctx); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	s.infos[key] = info
	return nil
}
func (s *snapshotObjects) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	s.gets.Add(1)
	if s.probe != nil {
		if err := s.probe(ctx); err != nil {
			return nil, objectstore.Info{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, objectstore.Info{}, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), s.infos[key], nil
}
func (s *snapshotObjects) Head(_ context.Context, key string) (objectstore.Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	info, ok := s.infos[key]
	if !ok {
		return info, os.ErrNotExist
	}
	return info, nil
}
func (s *snapshotObjects) Delete(context.Context, string) error {
	return errors.New("destructive GC is disabled")
}
func (s *snapshotObjects) List(context.Context, string, string) (objectstore.Page, error) {
	return objectstore.Page{}, nil
}

type snapshotFileRemote struct {
	mu                        sync.Mutex
	caps                      sandbox.WorkspaceFileCapabilities
	archive, manifest         []byte
	receipt                   workspacefiles.Receipt
	jobs                      map[string]sandbox.WorkspaceJobReceipt
	creates, renews, finishes int
	lostFinish                bool
	corrupt                   bool
	beforeFinish              func()
	capsUnavailable           bool
}

func (r *snapshotFileRemote) Capabilities(context.Context) (sandbox.WorkspaceJobCapabilities, error) {
	return r.caps.Jobs, nil
}
func (r *snapshotFileRemote) FileCapabilities(context.Context) (sandbox.WorkspaceFileCapabilities, error) {
	if r.capsUnavailable {
		return sandbox.WorkspaceFileCapabilities{}, errors.New("provider offline")
	}
	return r.caps, nil
}
func (r *snapshotFileRemote) Create(_ context.Context, in sandbox.WorkspaceJobCreate) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creates++
	receipt := sandbox.WorkspaceJobReceipt{Version: 1, ID: in.Spec.ID, Kind: in.Spec.Kind, Resource: in.Spec.Resource, Lineage: in.Spec.Lineage, SHA256: in.SHA256, Generation: in.Fence.Generation, ExpiresAt: in.Fence.ExpiresAt, Deadline: in.Spec.Deadline, State: "running", CreatedAt: time.Now().UTC()}
	if in.Spec.Kind == "capture_files" {
		receipt.State = "completed"
		receipt.Result, _ = json.Marshal(r.receipt)
	}
	r.jobs[in.Spec.ID] = receipt
	return receipt, nil
}
func (r *snapshotFileRemote) Get(_ context.Context, id string) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.jobs[id]
	if !ok {
		return value, &sandbox.WorkspaceJobError{Status: 404}
	}
	return value, nil
}
func (r *snapshotFileRemote) Renew(_ context.Context, id string, in sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.renews++
	value := r.jobs[id]
	if value.State != "running" || !value.ExpiresAt.After(time.Now()) || value.Generation != in.Fence.Generation {
		return value, &sandbox.WorkspaceJobError{Status: 409}
	}
	value.ExpiresAt = in.Fence.ExpiresAt
	r.jobs[id] = value
	return value, nil
}
func (r *snapshotFileRemote) Cancel(_ context.Context, id string, _ sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value := r.jobs[id]
	if value.State == "running" {
		value.State = "cancelled"
		r.jobs[id] = value
	}
	return value, nil
}
func (r *snapshotFileRemote) ReadWorkspaceChunk(_ context.Context, id string, fence sandbox.WorkspaceJobRenew, part string, offset int64, length int) ([]byte, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	job := r.jobs[id]
	if job.State != "running" || !job.ExpiresAt.After(time.Now()) || job.Generation != fence.Fence.Generation {
		return nil, false, &sandbox.WorkspaceJobError{Status: 409}
	}
	data := r.archive
	if part == "manifest" {
		data = r.manifest
	}
	if offset < 0 || offset > int64(len(data)) {
		return nil, false, ErrWorkspaceJobInvalid
	}
	last := min(int64(len(data)), offset+int64(length))
	body := bytes.Clone(data[offset:last])
	if r.corrupt && part == "archive" && len(body) > 0 {
		body[0] ^= 1
	}
	return body, last == int64(len(data)), nil
}
func (r *snapshotFileRemote) WriteWorkspaceChunk(context.Context, string, sandbox.WorkspaceJobRenew, string, int64, []byte) error {
	return errors.New("fixture has no materializer")
}
func (r *snapshotFileRemote) FinishWorkspaceTransfer(_ context.Context, id string, _ sandbox.WorkspaceJobRenew) error {
	if r.beforeFinish != nil {
		r.beforeFinish()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishes++
	value := r.jobs[id]
	value.State = "completed"
	value.Result, _ = json.Marshal(r.receipt)
	r.jobs[id] = value
	if r.lostFinish {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func snapshotArchiveFixture(t *testing.T) *snapshotFileRemote {
	t.Helper()
	var archive bytes.Buffer
	z := gzip.NewWriter(&archive)
	tw := tar.NewWriter(z)
	for _, h := range []*tar.Header{{Name: "workspace", Typeflag: tar.TypeDir, Mode: 0700}, {Name: "workspace/dirty.bin", Typeflag: tar.TypeReg, Mode: 0755, Size: 17}} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte("uncommitted bytes")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := errors.Join(tw.Close(), z.Close()); err != nil {
		t.Fatal(err)
	}
	manifest, err := workspacearchive.Inspect(t.Context(), bytes.NewReader(archive.Bytes()), workspacearchive.Limits{MaxEntries: 100, MaxBytes: 1 << 20, MaxFileBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	limits := sandbox.WorkspaceFileLimits{MaxFileBytes: 512 << 20, MaxBytes: 2 << 30, MaxGitBytes: 2 << 30, MaxArchiveBytes: 3 << 30, MaxManifestBytes: 64 << 20, MaxSpoolBytes: 8 << 30, MaxEntries: 100000, MaxRepositories: 64, MaxScanEntries: 500000}
	files := []workspacearchive.Entry{manifest.Entries[1]}
	files[0].Path = "dirty.bin"
	body, err := json.Marshal(workspacefiles.Manifest{Version: 1, PolicyVersion: workspacefiles.PolicyVersion, PolicySHA256: strings.Repeat("a", 64), Consistency: "verified_copy", Limits: limits, Paths: []string{"."}, Files: files, Repositories: []workspacefiles.Repository{}, Excluded: []workspacefiles.Exclusion{}, Archive: manifest, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	aHash, mHash := sha256.Sum256(archive.Bytes()), sha256.Sum256(body)
	receipt := workspacefiles.Receipt{Version: 1, Consistency: "verified_copy", Files: 1, Bytes: 17, Archive: workspacefiles.Artifact{Name: "workspace.tar.gz", SHA256: hex.EncodeToString(aHash[:]), Bytes: int64(archive.Len())}, Manifest: workspacefiles.Artifact{Name: "manifest.json", SHA256: hex.EncodeToString(mHash[:]), Bytes: int64(len(body))}}
	return &snapshotFileRemote{caps: sandbox.WorkspaceFileCapabilities{Version: 1, Capture: true, Read: true, Restore: true, Consistency: "verified_copy", PolicyVersion: workspacefiles.PolicyVersion, PolicySHA256: strings.Repeat("a", 64), Limits: limits, MaxChunkBytes: 1 << 20, Jobs: sandbox.WorkspaceJobCapabilities{Version: 1, Resource: "resource-one", Lineage: "lineage-original", RenewableLease: true, PersistentGeneration: true, Kinds: []string{"capture_files", "read_snapshot", "restore_files"}, MaxLeaseMillis: 45000, MaxDurationMillis: 1800000}}, archive: archive.Bytes(), manifest: body, receipt: receipt, jobs: map[string]sandbox.WorkspaceJobReceipt{}}
}

func publicationFixture(t *testing.T, remote WorkspaceJobRemote, mutate ...func(*payloadhold.Options, *WorkspaceJobOptions)) (*WorkspaceSnapshotPipeline, *snapshotObjects, *jobAuthority, *atomic.Bool, uint) {
	t.Helper()
	base, _, authority, input := snapshotJobs(t)
	db := base.db
	if err := db.AutoMigrate(&models.PayloadUploadHold{}, &models.PayloadUploadBudget{}); err != nil {
		t.Fatal(err)
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	objects := &snapshotObjects{objects: map[string][]byte{}, infos: map[string]objectstore.Info{}}
	objects.probe = func(ctx context.Context) error {
		call, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		return db.WithContext(call).Exec("SELECT 1").Error
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	revoked := &atomic.Bool{}
	authorize := func(ctx context.Context, tx *gorm.DB, row models.WorkspaceSnapshotJob) error {
		if revoked.Load() {
			return errors.New("source grant revoked")
		}
		return base.options.Authorize(ctx, tx, row)
	}
	holdOptions := payloadhold.Options{StorageFingerprint: strings.Repeat("f", 64), Authority: authority, Authorize: func(ctx context.Context, tx *gorm.DB, id payloadhold.Identity) error {
		var row models.WorkspaceSnapshotJob
		if err := tx.Take(&row, "id = ?", id.OperationID).Error; err != nil {
			return err
		}
		if row.ActorID != id.ActorID {
			return ErrWorkspaceJobConflict
		}
		return authorize(ctx, tx, row)
	}, LeaseTTL: 300 * time.Millisecond, RenewInterval: 75 * time.Millisecond, MaxDuration: 2 * time.Minute}
	options := base.options
	options.Authorize = authorize
	options.Remote = func(context.Context, uint) (WorkspaceJobRemote, error) { return remote, nil }
	options.MaxDuration = 2 * time.Minute
	for _, fn := range mutate {
		fn(&holdOptions, &options)
	}
	holds, err := payloadhold.New(db, files, holdOptions)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewWorkspaceSnapshotPipeline(db, base.sandbox, holds, files, payloadhold.DefaultLimits(), options)
	if err != nil {
		t.Fatal(err)
	}
	return p, objects, authority, revoked, input.SandboxID
}

func beginPublication(t *testing.T, p *WorkspaceSnapshotPipeline, sandboxID uint) (models.WorkspaceSnapshotJob, models.WorkspaceSnapshotJob) {
	t.Helper()
	capture, err := p.Capture(t.Context(), "alice", WorkspaceCaptureInput{ClientRequestID: "capture-original", ScopeKind: "solo", ScopeID: "solo-original", SandboxID: sandboxID, Paths: []string{"."}, Duration: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Jobs.RunOne(t.Context(), capture.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.QueuePublication(t.Context(), "alice", capture.ID); err != nil {
		t.Fatal(err)
	}
	var snapshot models.WorkspaceSnapshot
	if err := p.db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
		t.Fatal(err)
	}
	var publication models.WorkspaceSnapshotJob
	if err := p.db.Take(&publication, "id = ?", snapshot.PublicationJobID).Error; err != nil {
		t.Fatal(err)
	}
	if !publication.Deadline.Equal(capture.Deadline) {
		t.Fatal("publication extended the original finite job deadline")
	}
	return capture, publication
}

func TestWorkspaceSnapshotPublicationPinsOnlyAfterSemanticAndFullObjectVerification(t *testing.T) {
	remote := snapshotArchiveFixture(t)
	p, objects, _, _, sandboxID := publicationFixture(t, remote)
	objects.beforePut = func(ctx context.Context) error {
		timer := time.NewTimer(350 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-timer.C:
			return nil
		}
	}
	capture, publication := beginPublication(t, p, sandboxID)
	if err := p.Jobs.RunOne(t.Context(), publication.ID); err != nil {
		t.Fatal(err)
	}
	var snapshot models.WorkspaceSnapshot
	if err := p.db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
		t.Fatal(err)
	}
	var hold models.PayloadUploadHold
	if err := p.db.Take(&hold, "operation_id = ?", publication.ID).Error; err != nil {
		t.Fatal(err)
	}
	var job models.WorkspaceSnapshotJob
	if err := p.db.Take(&job, "id = ?", publication.ID).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot.State != "ready" || snapshot.ArchiveRef == "" || snapshot.ManifestRef == "" || snapshot.VerifiedAt == nil || hold.State != payloadhold.StatePinned || job.State != "executed" || snapshot.SourceGeneration == 0 || snapshot.SourceGeneration >= job.RuntimeGeneration {
		t.Fatalf("missing atomic publication/provenance: snapshot=%+v hold=%s job=%s", snapshot, hold.State, job.State)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.creates != 2 || remote.finishes != 1 || remote.renews < 3 || objects.puts.Load() != 2 || objects.gets.Load() < 2 {
		t.Fatalf("lease/copy proof missing: creates=%d finishes=%d renews=%d put=%d get=%d", remote.creates, remote.finishes, remote.renews, objects.puts.Load(), objects.gets.Load())
	}
}

func TestWorkspaceSnapshotPublicationUnknownFinishReconcilesReadOnlyWithoutReplay(t *testing.T) {
	remote := snapshotArchiveFixture(t)
	remote.lostFinish = true
	p, objects, _, _, sandboxID := publicationFixture(t, remote)
	capture, publication := beginPublication(t, p, sandboxID)
	if err := p.Jobs.RunOne(t.Context(), publication.ID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal("lost response was claimed successful", err)
	}
	var snapshot models.WorkspaceSnapshot
	if err := p.db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot.State == "ready" {
		t.Fatal("ambiguous finish became ready")
	}
	puts := objects.puts.Load()
	var held models.PayloadUploadHold
	if err := p.db.Take(&held, "operation_id = ?", publication.ID).Error; err != nil {
		t.Fatal(err)
	}
	// An explicit checker cannot steal a still-live upload owner. Wait for its
	// original acknowledged deadline; no native or storage write is retried.
	if wait := time.Until(held.ExpiresAt); wait > 0 {
		timer := time.NewTimer(wait + 2*time.Millisecond)
		select {
		case <-t.Context().Done():
			timer.Stop()
			t.Fatal(context.Cause(t.Context()))
		case <-timer.C:
		}
	}
	reconciled, err := p.Jobs.Reconcile(t.Context(), "alice", publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if reconciled.State != "executed" || snapshot.State != "ready" || remote.creates != 2 || remote.finishes != 1 || objects.puts.Load() != puts {
		t.Fatal("reconcile replayed native/storage writes or failed to publish")
	}
}

func TestWorkspaceSnapshotPublicationRejectsCorruptArchiveAndRevokedFinalGrant(t *testing.T) {
	for _, scenario := range []string{"corrupt_archive", "grant_revoked_after_upload"} {
		t.Run(scenario, func(t *testing.T) {
			remote := snapshotArchiveFixture(t)
			p, objects, _, revoked, sandboxID := publicationFixture(t, remote)
			if scenario == "corrupt_archive" {
				remote.corrupt = true
			} else {
				remote.beforeFinish = func() { revoked.Store(true) }
			}
			capture, publication := beginPublication(t, p, sandboxID)
			if err := p.Jobs.RunOne(t.Context(), publication.ID); err == nil {
				t.Fatal("invalid publication succeeded")
			}
			var snapshot models.WorkspaceSnapshot
			if err := p.db.Take(&snapshot, "id = ?", capture.ID).Error; err != nil {
				t.Fatal(err)
			}
			if snapshot.State == "ready" || snapshot.ArchiveRef != "" || snapshot.ManifestRef != "" {
				t.Fatal("unverified or unauthorized refs became live")
			}
			if scenario == "corrupt_archive" && objects.puts.Load() != 0 {
				t.Fatal("semantic rejection happened after object upload")
			}
			var pinned int64
			p.db.Model(&models.PayloadUploadHold{}).Where("state = ?", payloadhold.StatePinned).Count(&pinned)
			if pinned != 0 {
				t.Fatal("rejected publication committed its pin")
			}
		})
	}
}

func TestWorkspaceSnapshotCaptureRetryUsesFrozenPolicyWithoutProviderRequest(t *testing.T) {
	remote := snapshotArchiveFixture(t)
	p, _, _, _, sandboxID := publicationFixture(t, remote)
	input := WorkspaceCaptureInput{ClientRequestID: "stable-capture-request", ScopeKind: "solo", ScopeID: "solo-original", SandboxID: sandboxID, Paths: []string{"."}, Duration: 20 * time.Second}
	first, err := p.Capture(t.Context(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	remote.capsUnavailable = true
	retry, err := p.Capture(t.Context(), "alice", input)
	if err != nil || retry.ID != first.ID || retry.Body != first.Body || !retry.Deadline.Equal(first.Deadline) {
		t.Fatal("retry fetched capabilities or changed frozen intent", err)
	}
	input.Paths = []string{"different"}
	if _, err := p.Capture(t.Context(), "alice", input); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("same nonce accepted different source paths", err)
	}
}
