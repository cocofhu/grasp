package payloadhold

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func TestHoldSlowPutReleasesSQLAndRenewsBeyondOriginalLease(t *testing.T) {
	db, m, objects, _ := holdFixture(t, func(o *Options) { o.LeaseTTL = 300 * time.Millisecond; o.RenewInterval = 40 * time.Millisecond })
	ctx := context.Background()
	readyID := holdIdentity("ready-first")
	readyPlan := holdPlan(t, m, "already verified")
	readyLease, err := m.BeginHold(ctx, readyID, readyPlan)
	if err != nil {
		t.Fatal(err)
	}
	readyProof, err := m.UploadAndVerify(ctx, readyLease, readyPlan)
	if err != nil {
		t.Fatal(err)
	}
	id := holdIdentity("slow")
	plan := holdPlan(t, m, "slow archive publication")
	lease, err := m.BeginHold(ctx, id, plan)
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var notify, unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects.beforePut = func(ctx context.Context, _ string) error {
		var err error
		notify.Do(func() {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				err = ctx.Err()
			}
		})
		return err
	}
	done := make(chan error, 1)
	go func() { _, err := m.UploadAndVerify(ctx, lease, plan); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("PUT never started")
	}
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, err := m.Get(probe, id); err != nil {
		t.Fatalf("upload held the sole SQL connection: %v", err)
	}
	if err := db.WithContext(probe).Transaction(func(tx *gorm.DB) error { _, err := m.CommitPinTx(tx, readyLease, readyProof); return err }); err != nil {
		t.Fatalf("blocked PUT held the final metadata publication: %v", err)
	}
	// Wait past the acknowledged original expiry, while renewals use the same
	// one-connection pool that the upload would otherwise occupy.
	timer := time.NewTimer(time.Until(lease.data.expiresAt) + 80*time.Millisecond)
	defer timer.Stop()
	<-timer.C
	row, err := m.Get(probe, id)
	if err != nil || !row.ExpiresAt.After(time.Now()) || !row.ExpiresAt.After(lease.data.expiresAt) {
		t.Fatalf("network streaming prevented lease renewal: %+v %v", row, err)
	}
	select {
	case err := <-done:
		t.Fatalf("live renewed upload ended early: %v", err)
	default:
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("released upload did not complete")
	}
}

func TestHoldShortDeadlineAndLostOwnerDoNotPublishReferences(t *testing.T) {
	for _, mode := range []string{"caller-deadline", "owner-lost"} {
		t.Run(mode, func(t *testing.T) {
			db, m, objects, _ := holdFixture(t, nil)
			id := holdIdentity(mode)
			plan := holdPlan(t, m, "immutable original bytes")
			lease, err := m.BeginHold(context.Background(), id, plan)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			var cancel context.CancelFunc
			if mode == "caller-deadline" {
				ctx, cancel = context.WithTimeout(ctx, 60*time.Millisecond)
				defer cancel()
				objects.beforePut = func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() }
			} else {
				objects.beforePut = func(context.Context, string) error {
					return db.Model(&models.PayloadUploadHold{}).Where("id = ?", lease.ID()).Updates(map[string]any{"owner": "new-generation", "generation": 2}).Error
				}
			}
			begin := time.Now()
			proof, err := m.UploadAndVerify(ctx, lease, plan)
			if proof.lease != nil || err == nil || mode == "caller-deadline" && (!errors.Is(err, context.DeadlineExceeded) || time.Since(begin) > time.Second) || mode == "owner-lost" && !errors.Is(err, ErrOwnerLost) {
				t.Fatalf("failed upload retained an executable proof: %+v %v", proof, err)
			}
			row, err := m.Get(context.Background(), id)
			if err != nil || row.ArchiveRef != "" || row.ManifestRef != "" || row.PinnedAt != nil {
				t.Fatalf("unowned/unverified upload published roots: %+v %v", row, err)
			}
			if mode == "owner-lost" && (row.Owner != "new-generation" || row.Generation != 2 || row.State != StateUploading) {
				t.Fatal("stale failure handling touched the replacement owner")
			}
		})
	}
}

func TestHoldSQLFailureAfterPutLeavesPlannedVerifiableObjectOnly(t *testing.T) {
	db, m, objects, _ := holdFixture(t, nil)
	id := holdIdentity("sql-fault")
	plan := holdPlan(t, m, "PUT completed before SQL stopped")
	lease, err := m.BeginHold(context.Background(), id, plan)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("reference CAS database failure")
	if err := db.Callback().Update().Before("gorm:update").Register("hold-ref-fault", func(tx *gorm.DB) {
		if changes, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "payload_upload_holds" && changes["archive_ref"] != nil {
			_ = tx.AddError(want)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.UploadAndVerify(context.Background(), lease, plan); !errors.Is(err, want) || objects.puts.Load() != 1 || objects.gets.Load() != 1 {
		t.Fatalf("failed reference publication was not isolated from storage: %v puts=%d gets=%d", err, objects.puts.Load(), objects.gets.Load())
	}
	if err := db.Callback().Update().Remove("hold-ref-fault"); err != nil {
		t.Fatal(err)
	}
	row, err := m.Get(context.Background(), id)
	if err != nil || row.State != StateUnknown || row.ArchiveRef != "" || row.ManifestRef != "" || row.ReservedBytes <= 0 {
		t.Fatalf("SQL failure fabricated a reference or discarded the intent: %+v %v", row, err)
	}
	if _, err := m.files.VerifyBounded(context.Background(), rawRef(filestore.Binary, row.ArchiveSHA256), filestore.Binary, row.ArchiveBytes); err != nil {
		t.Fatalf("completed orphan cannot be reconciled from its persisted plan: %v", err)
	}
	if err := db.Model(&row).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	_, verified, err := m.RecoverHold(context.Background(), id, time.Now().Add(time.Minute))
	if err == nil || verified.lease != nil || objects.puts.Load() != 1 {
		t.Fatal("recovery uploaded the missing manifest or fabricated a ready proof")
	}
	row, err = m.Get(context.Background(), id)
	if err != nil || row.ArchiveRef == "" || row.ManifestRef != "" || row.PinnedAt != nil {
		t.Fatal("partial reconciliation lost the now-verified archive root")
	}
}

func TestHoldBudgetsRetainExpiredUnknownAndPinnedReservations(t *testing.T) {
	db, m, objects, _ := holdFixture(t, func(o *Options) { o.MaxReservedBytes = 80; o.MaxScopeReservedBytes = 48 })
	plan := holdPlan(t, m, "archive")
	first := holdIdentity("reservation-one")
	lease, err := m.BeginHold(context.Background(), first, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.PayloadUploadHold{}).Where("id = ?", lease.ID()).Updates(map[string]any{"state": StateUnknown, "expires_at": time.Now().Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := m.BeginHold(context.Background(), holdIdentity("same-scope"), plan); !errors.Is(err, ErrLimit) {
		t.Fatalf("expiry or unknown state released the scope reservation: %v", err)
	}
	other := holdIdentity("other-scope")
	other.ScopeID = "solo-2"
	otherLease, err := m.BeginHold(context.Background(), other, plan)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := m.UploadAndVerify(context.Background(), otherLease, plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := m.CommitPinTx(tx, otherLease, proof); return err }); err != nil {
		t.Fatal(err)
	}
	third := holdIdentity("global-exhausted")
	third.ScopeID = "solo-3"
	if _, err := m.BeginHold(context.Background(), third, plan); !errors.Is(err, ErrLimit) || objects.puts.Load() != 2 {
		t.Fatalf("pin released the namespace reservation or rejected intent uploaded data: %v", err)
	}
}

func TestHoldProofsRejectForeignTransactionGenerationAndClosedStore(t *testing.T) {
	for _, mode := range []string{"outside-tx", "foreign-db", "foreign-manager", "generation", "close"} {
		t.Run(mode, func(t *testing.T) {
			db, m, _, _ := holdFixture(t, nil)
			id := holdIdentity(mode)
			plan := holdPlan(t, m, "verified only for its private owner")
			lease, err := m.BeginHold(context.Background(), id, plan)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := m.UploadAndVerify(context.Background(), lease, plan)
			if err != nil {
				t.Fatal(err)
			}
			target, manager := db, m
			want := ErrInvalid
			switch mode {
			case "outside-tx":
				if _, err := m.CommitPinTx(db, lease, proof); !errors.Is(err, ErrTransaction) {
					t.Fatalf("pin accepted no outer transaction: %v", err)
				}
				return
			case "foreign-db":
				target, _, _, _ = holdFixture(t, nil)
			case "foreign-manager":
				manager, err = New(db, m.files, m.options)
				if err != nil {
					t.Fatal(err)
				}
			case "generation":
				if err := db.Model(&models.PayloadUploadHold{}).Where("id = ?", lease.ID()).Update("generation", 2).Error; err != nil {
					t.Fatal(err)
				}
				want = ErrOwnerLost
			case "close":
				m.files.Close()
				want = filestore.ErrClosed
			}
			if err := target.Transaction(func(tx *gorm.DB) error { _, err := manager.CommitPinTx(tx, lease, proof); return err }); !errors.Is(err, want) {
				t.Fatalf("invalid pin proof accepted: %v want %v", err, want)
			}
			var row models.PayloadUploadHold
			if err := db.Take(&row, "id = ?", lease.ID()).Error; err != nil || row.State != StateVerified || row.PinnedAt != nil {
				t.Fatal("rejected proof changed persisted state")
			}
		})
	}
}

type cancellableStageReader struct {
	started, closed chan struct{}
	once, stop      sync.Once
}

func (r *cancellableStageReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.closed
	return 0, io.ErrClosedPipe
}

func (r *cancellableStageReader) Close() error { r.stop.Do(func() { close(r.closed) }); return nil }

func TestHoldPlanningIsBoundedCancelableAndRejectsTransactionHandles(t *testing.T) {
	db, m, objects, _ := holdFixture(t, nil)
	limits := DefaultLimits()
	limits.MaxArchiveBytes = 4
	if _, err := m.Stage(context.Background(), stringOpener("oversize"), stringOpener(`{}`), limits); !errors.Is(err, ErrLimit) {
		t.Fatalf("oversize local plan accepted: %v", err)
	}
	if _, err := m.Stage(context.Background(), stringOpener("body"), stringOpener("invalid JSON"), DefaultLimits()); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid manifest plan accepted")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := New(tx, m.files, m.options)
		if !errors.Is(err, ErrTransaction) {
			return errors.New("transaction-bound upload manager was accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source := &cancellableStageReader{started: make(chan struct{}), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := m.Stage(ctx, func(context.Context, int64) (io.ReadCloser, error) { return source, nil }, stringOpener(`{}`), DefaultLimits())
		done <- err
	}()
	<-source.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stage did not preserve caller cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stage left a detached reader after cancellation")
	}
	files, err := filepath.Glob(filepath.Join(m.options.StagingDir, "snapshot-plan-*"))
	if err != nil || len(files) != 0 || objects.puts.Load() != 0 || objects.gets.Load() != 0 {
		t.Fatalf("failed local plan leaked staging or performed object I/O: %v %v", files, err)
	}
	if _, err := New(db, m.files, Options{StorageFingerprint: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("manager supplied a permissive execution authority by default")
	}
	// Source contents that look like references remain ordinary archive bytes.
	plan := holdPlan(t, m, filestore.Prefix+filestore.Binary+"/"+strings.Repeat("0", 64))
	a, _, _, err := plan.details(m)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(a.path)
	if err != nil || !strings.HasPrefix(string(body), filestore.Prefix) {
		t.Fatal("local planning interpreted user bytes as another stored reference")
	}
}
