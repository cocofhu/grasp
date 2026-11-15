package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/solobackup"
)

type deleteFailStore struct {
	solobackup.Store
	fail bool
}

func (s *deleteFailStore) Delete(ctx context.Context, key string) error {
	if s.fail {
		return errors.New("storage offline")
	}
	return s.Store.Delete(ctx, key)
}

func TestCancellationRetriesPhysicalBackupDeletion(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	row := f.recovery(t)
	key := row.RecoveryPoints[0].Key
	store := &deleteFailStore{Store: f.store, fail: true}
	f.solo.SetBackupStore(store)
	if _, err := f.solo.Close(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	pending, err := f.sbx.Get(row.ID)
	if err != nil || !pending.CleanupPending || pending.Status != "stopped" || len(pending.RecoveryPoints) != 1 || !pending.RecoveryPoints[0].Deleting {
		t.Fatalf("cleanup intent lost: %+v %v", pending, err)
	}
	if capacity, err := f.sbx.Capacity(); err != nil || capacity.Used != 0 {
		t.Fatalf("released workload occupied capacity: %+v %v", capacity, err)
	}
	if !strings.Contains(f.row(t).Error, "清理未完成") {
		t.Fatal("cancel falsely claimed all storage released")
	}
	store.fail = false
	f.sbx.RetryRecoveryCleanup(context.Background())
	if _, err := f.sbx.Get(row.ID); err == nil {
		t.Fatal("successful cleanup kept row")
	}
	if rc, err := f.store.Open(context.Background(), key); err == nil {
		_ = rc.Close()
		t.Fatal("retry left object")
	}
}

func TestCancelledLegacyArchiveRetriesWithoutSandboxRow(t *testing.T) {
	for _, tableRecord := range []bool{false, true} {
		t.Run(map[bool]string{false: "session-key", true: "backup-table"}[tableRecord], func(t *testing.T) {
			f := newStopFixture(t)
			row := f.row(t)
			if err := f.sbx.CancelSandbox(context.Background(), row.SandboxID); err != nil {
				t.Fatal(err)
			}
			const key = "solo/cancelled-legacy/archive.tar.gz"
			if _, err := f.store.Put(context.Background(), key, strings.NewReader("saved workspace")); err != nil {
				t.Fatal(err)
			}
			updates := map[string]any{"sandbox_id": 0, "status": models.SoloStatusStopped}
			if tableRecord {
				f.db.Create(&models.SoloBackup{SessionID: f.id, Store: f.store.Kind(), ObjectKey: key})
			} else {
				updates["backup_store"], updates["backup_key"] = f.store.Kind(), key
			}
			f.db.Model(&models.SoloSession{}).Where("id = ?", f.id).Updates(updates)
			store := &deleteFailStore{Store: f.store, fail: true}
			f.solo.SetBackupStore(store)
			if _, err := f.solo.Close(context.Background(), f.id, "alice"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(f.row(t).Error, "清理未完成") {
				t.Fatal("missing durable cleanup error")
			}
			store.fail = false
			f.sbx.RetryRecoveryCleanup(context.Background())
			if rc, err := f.store.Open(context.Background(), key); err == nil {
				_ = rc.Close()
				t.Fatal("cancelled legacy object survived cleanup retry")
			}
			if row := f.row(t); row.BackupKey != "" || row.Error != "" || row.Status != models.SoloStatusClosed {
				t.Fatalf("legacy cancellation not reconciled: status=%s error=%s key=%s", row.Status, row.Error, row.BackupKey)
			}
		})
	}
}

func TestRetentionDeleteFailureKeepsRetryKeys(t *testing.T) {
	f := newStopFixture(t)
	f.stop(t)
	first := f.recovery(t).RecoveryPoints[0]
	if _, err := f.solo.Resume(context.Background(), f.id, "alice"); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(t, models.SoloStatusRunning)
	store := &deleteFailStore{Store: f.store, fail: true}
	f.solo.SetBackupStore(store)
	f.stop(t)
	row := f.recovery(t)
	if len(row.RecoveryPoints) != 2 || !row.RecoveryPoints[1].Deleting {
		t.Fatalf("prune lost failed key: %+v", row.RecoveryPoints)
	}
	store.fail = false
	f.sbx.RetryRecoveryCleanup(context.Background())
	row = f.recovery(t)
	if len(row.RecoveryPoints) != 1 || row.RecoveryPoints[0].Key == first.Key {
		t.Fatal("prune retry did not finish")
	}
}

func TestOldStoppedArchivesMigrateWithoutDeletingData(t *testing.T) {
	f := newStopFixture(t)
	row := f.row(t)
	const content = "legacy-workspace"
	digest := sha256.Sum256([]byte(content))
	key := "solo/legacy/archive.tar.gz"
	obj, err := f.store.Put(context.Background(), key, strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	f.db.Model(&models.SoloSession{}).Where("id = ?", row.ID).Updates(map[string]any{"backup_store": f.store.Kind(), "backup_key": key, "backup_sha256": hex.EncodeToString(digest[:]), "backup_size": obj.Size, "backup_at": now})
	if err := f.solo.adoptLegacyBackups(); err != nil {
		t.Fatal(err)
	}
	if err := f.solo.adoptLegacyBackups(); err != nil {
		t.Fatal(err)
	}
	sb := f.recovery(t)
	if len(sb.RecoveryPoints) != 1 || sb.RecoveryPoints[0].Key != key {
		t.Fatalf("migration = %+v", sb.RecoveryPoints)
	}
	if rc, err := f.store.Open(context.Background(), key); err != nil {
		t.Fatal("migration removed object")
	} else {
		_ = rc.Close()
	}
}
