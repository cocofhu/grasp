package payloadhold

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type holdAuthority struct{ deny atomic.Bool }

func (a *holdAuthority) Verify(context.Context, string) error {
	if a.deny.Load() {
		return ErrAuthority
	}
	return nil
}

type holdObject struct {
	body []byte
	info objectstore.Info
}

type holdObjects struct {
	objectstore.Store
	mu         sync.Mutex
	objects    map[string]holdObject
	puts, gets atomic.Int32
	beforePut  func(context.Context, string) error
	beforeGet  func(context.Context, string) error
}

func (s *holdObjects) Put(ctx context.Context, key string, reader io.ReadSeeker, info objectstore.Info) error {
	s.puts.Add(1)
	if s.beforePut != nil {
		if err := s.beforePut(ctx, key); err != nil {
			return err
		}
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = holdObject{body: body, info: info}
	s.mu.Unlock()
	return nil
}

func (s *holdObjects) Open(ctx context.Context, key string) (io.ReadCloser, objectstore.Info, error) {
	s.gets.Add(1)
	if s.beforeGet != nil {
		if err := s.beforeGet(ctx, key); err != nil {
			return nil, objectstore.Info{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.objects[key]
	if !ok {
		return nil, objectstore.Info{}, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(value.body)), value.info, nil
}

func holdFixture(t *testing.T, mutate func(*Options)) (*gorm.DB, *Manager, *holdObjects, *holdAuthority) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "hold.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := filestore.Attach(db, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	files, _ := filestore.FromDB(db)
	t.Cleanup(files.Close)
	objects := &holdObjects{objects: map[string]holdObject{}}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.PayloadUploadHold{}, &models.PayloadUploadBudget{}, &models.WorkspaceSnapshot{}); err != nil {
		t.Fatal(err)
	}
	authority := &holdAuthority{}
	options := Options{StorageFingerprint: strings.Repeat("a", 64), Authority: authority, Authorize: func(context.Context, *gorm.DB, Identity) error { return nil }}
	if mutate != nil {
		mutate(&options)
	}
	m, err := New(db, files, options)
	if err != nil {
		t.Fatal(err)
	}
	return db, m, objects, authority
}

func holdIdentity(operation string) Identity {
	return canonicalIdentity(Identity{ScopeKind: "solo", ScopeID: "solo-1", ActorID: "actor-1", OperationID: operation, SnapshotID: "snapshot-" + operation, Lineage: "external-execution-lineage", Deadline: time.Now().UTC().Add(time.Minute)})
}

func stringOpener(value string) Opener {
	return func(context.Context, int64) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(value)), nil
	}
}

func holdPlan(t *testing.T, m *Manager, archive string) Plan {
	t.Helper()
	plan, err := m.Stage(context.Background(), stringOpener(archive), stringOpener(`{"version":1,"entries":[]}`), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plan.Close() })
	return plan
}

func TestHoldMustCommitBeforePutAndIdentityCannotChangeContents(t *testing.T) {
	db, m, objects, authority := holdFixture(t, nil)
	plan := holdPlan(t, m, "immutable archive")
	id := holdIdentity("first")
	if objects.puts.Load() != 0 {
		t.Fatal("local planning uploaded an object before the reservation")
	}
	want := errors.New("SQL admission failed")
	if err := db.Callback().Create().Before("gorm:create").Register("hold-admission-fault", func(tx *gorm.DB) {
		if tx.Statement.Table == "payload_upload_holds" {
			_ = tx.AddError(want)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.BeginHold(context.Background(), id, plan); !errors.Is(err, want) || objects.puts.Load() != 0 {
		t.Fatalf("failed intent admission reached object storage: %v puts=%d", err, objects.puts.Load())
	}
	if err := db.Callback().Create().Remove("hold-admission-fault"); err != nil {
		t.Fatal(err)
	}
	lease, err := m.BeginHold(context.Background(), id, plan)
	if err != nil {
		t.Fatal(err)
	}
	if lease.ID() != HoldID(id) || objects.puts.Load() != 0 {
		t.Fatal("admission did not preserve its stable SQL-only receipt")
	}
	if _, err := m.BeginHold(context.Background(), id, plan); !errors.Is(err, ErrBusy) {
		t.Fatalf("duplicate admission silently acquired an existing upload: %v", err)
	}
	changed := holdPlan(t, m, "changed archive")
	if _, err := m.BeginHold(context.Background(), id, changed); !errors.Is(err, ErrConflict) || objects.puts.Load() != 0 {
		t.Fatal("same identity accepted changed contents or uploaded before conflict checking")
	}
	authority.deny.Store(true)
	if _, err := m.UploadAndVerify(context.Background(), lease, plan); !errors.Is(err, ErrAuthority) || objects.puts.Load() != 0 {
		t.Fatal("missing external execution authority allowed an upload")
	}
	authority.deny.Store(false)
	verified, err := m.UploadAndVerify(context.Background(), lease, plan)
	if err != nil || verified.lease == nil || objects.puts.Load() != 2 || objects.gets.Load() != 2 {
		t.Fatalf("expected two bounded publications and full read-backs: %v puts=%d gets=%d", err, objects.puts.Load(), objects.gets.Load())
	}
	if _, err := m.UploadAndVerify(context.Background(), lease, plan); !errors.Is(err, ErrReconcile) || objects.puts.Load() != 2 {
		t.Fatal("used upload capability was silently replayed")
	}
}

func TestHoldVerifiedPinAndReadyPublicationRollBackTogether(t *testing.T) {
	db, m, objects, _ := holdFixture(t, nil)
	id := holdIdentity("atomic")
	plan := holdPlan(t, m, "verified snapshot archive")
	lease, err := m.BeginHold(context.Background(), id, plan)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := m.UploadAndVerify(context.Background(), lease, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkspaceSnapshot{ID: id.SnapshotID, State: "uploading"}).Error; err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("snapshot or job CAS failed")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := m.CommitPinTx(tx, lease, verified); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	row, err := m.Get(context.Background(), id)
	if err != nil || row.State != StateVerified || row.PinnedAt != nil || row.ArchiveRef == "" || row.ManifestRef == "" {
		t.Fatalf("failed ready publication lost roots or committed a pin: %+v %v", row, err)
	}
	beforePuts, beforeGets := objects.puts.Load(), objects.gets.Load()
	if err := db.Transaction(func(tx *gorm.DB) error {
		pinned, err := m.CommitPinTx(tx, lease, verified)
		if err != nil {
			return err
		}
		result := tx.Model(&models.WorkspaceSnapshot{}).Where("id = ? AND state = ?", pinned.SnapshotID, "uploading").Updates(map[string]any{"state": "ready", "archive_ref": pinned.ArchiveRef, "manifest_ref": pinned.ManifestRef})
		if result.RowsAffected != 1 && result.Error == nil {
			return rollback
		}
		return result.Error
	}); err != nil {
		t.Fatal(err)
	}
	row, err = m.Get(context.Background(), id)
	var snapshot models.WorkspaceSnapshot
	if err != nil || db.Take(&snapshot, "id = ?", id.SnapshotID).Error != nil || row.State != StatePinned || snapshot.State != "ready" || snapshot.ArchiveRef != row.ArchiveRef || snapshot.ManifestRef != row.ManifestRef || objects.puts.Load() != beforePuts || objects.gets.Load() != beforeGets {
		t.Fatalf("pin was not SQL-only and atomic with ready: hold=%+v snapshot=%+v %v", row, snapshot, err)
	}
}

func TestHoldPartialFailureRetainsVerifiedRootAndReservation(t *testing.T) {
	db, m, objects, _ := holdFixture(t, nil)
	id := holdIdentity("partial")
	plan := holdPlan(t, m, "archive survives a failed manifest upload")
	lease, err := m.BeginHold(context.Background(), id, plan)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("manifest object unavailable")
	var attempt atomic.Int32
	objects.beforePut = func(context.Context, string) error {
		if attempt.Add(1) == 2 {
			return want
		}
		return nil
	}
	if _, err := m.UploadAndVerify(context.Background(), lease, plan); !errors.Is(err, want) {
		t.Fatal(err)
	}
	row, err := m.Get(context.Background(), id)
	if err != nil || row.State != StateUnknown || row.ArchiveRef == "" || row.ManifestRef != "" || row.PinnedAt != nil {
		t.Fatalf("partial failure lost its known root or fabricated ready data: %+v %v", row, err)
	}
	var budget models.PayloadUploadBudget
	if err := db.Take(&budget, "id = ?", m.options.StorageFingerprint).Error; err != nil || budget.ReservedBytes != row.ReservedBytes || budget.HoldCount != 1 {
		t.Fatalf("unknown upload released its persisted reservation: %+v %v", budget, err)
	}
	if _, _, err := m.RecoverHold(context.Background(), id, time.Now().Add(time.Minute)); !errors.Is(err, ErrBusy) {
		t.Fatalf("explicit recovery displaced a still-live owner: %v", err)
	}
	if _, err := m.UploadAndVerify(context.Background(), lease, plan); !errors.Is(err, ErrReconcile) {
		t.Fatalf("unknown upload was silently dispatched again: %v", err)
	}
}

func TestHoldCrashAfterPutRecoversOnlyTheOriginalObjectsWithoutPut(t *testing.T) {
	db, m, objects, authority := holdFixture(t, nil)
	id := holdIdentity("crash")
	plan := holdPlan(t, m, "crashed publisher's original archive")
	lease, err := m.BeginHold(context.Background(), id, plan)
	if err != nil {
		t.Fatal(err)
	}
	a, b, _, _ := plan.details(m)
	// Model process loss after both immutable PUTs, before any verification SQL.
	for _, part := range []struct {
		stagedPart
		encoding string
	}{{a, filestore.Binary}, {b, filestore.JSON}} {
		body, err := os.Open(part.path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.files.PrepareBounded(context.Background(), body, part.encoding, part.size)
		_ = body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&models.PayloadUploadHold{}).Where("id = ?", lease.ID()).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.BeginHold(context.Background(), id, plan); !errors.Is(err, ErrReconcile) || objects.puts.Load() != 2 {
		t.Fatal("expiry automatically replayed an unknown dispatched upload")
	}
	authority.deny.Store(true)
	if _, _, err := m.RecoverHold(context.Background(), id, time.Now().Add(time.Minute)); !errors.Is(err, ErrAuthority) || objects.gets.Load() != 0 {
		t.Fatal("restored SQL without current authority read/recovered objects")
	}
	authority.deny.Store(false)
	recovered, verified, err := m.RecoverHold(context.Background(), id, time.Now().Add(time.Minute))
	if err != nil || recovered.data.generation != lease.data.generation+1 || objects.puts.Load() != 2 || objects.gets.Load() != 2 {
		t.Fatalf("reconciliation changed original content or replayed PUT: %v puts=%d gets=%d", err, objects.puts.Load(), objects.gets.Load())
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := m.CommitPinTx(tx, lease, verified); return err }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("new verification authorized the old generation: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := m.CommitPinTx(tx, recovered, verified); return err }); err != nil {
		t.Fatal(err)
	}
}
