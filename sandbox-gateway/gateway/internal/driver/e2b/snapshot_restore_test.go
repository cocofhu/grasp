package e2b

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"testing"

	"sandbox-gateway/internal/driver"
)

func checkpointedLifecycle(t *testing.T, id string) (*Driver, *lifecycleProvider, Options, *driver.Handle) {
	t.Helper()
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	h, err := d.Create(t.Context(), lifecycleSpec(id))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.PauseSnapshot(t.Context(), id, 2); err != nil {
		t.Fatal(err)
	}
	return d, f, opts, h
}

func recoveryRecord(t *testing.T, d *Driver, id string) record {
	t.Helper()
	e, err := d.lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	r := e.record
	r.RecoveryPoints = append([]recoveryPoint(nil), r.RecoveryPoints...)
	if r.Restore != nil {
		attempt := *r.Restore
		r.Restore = &attempt
	}
	return r
}

func TestSnapshotRestoreLatestKeepsBusinessIdentityAndEndpoints(t *testing.T) {
	d, f, _, original := checkpointedLifecycle(t, "restore-latest")
	before := recoveryRecord(t, d, original.ID)
	if before.LatestSnapshotID == "" || latestRecoveryPoint(&before) == nil {
		t.Fatal("pause did not persist the latest completed checkpoint")
	}
	f.mu.Lock()
	delete(f.sandboxes, original.Name)
	f.mu.Unlock()
	if h, err := d.Get(t.Context(), original.ID); err != nil || h.Status != driver.StatusStopped {
		t.Fatalf("retained checkpoint was treated as a lost sandbox: %v, %v", h, err)
	}
	if err := d.Start(t.Context(), original.ID); err != nil {
		t.Fatal(err)
	}
	h, err := d.Get(t.Context(), original.ID)
	if err != nil || h.ID != original.ID || h.Name == original.Name || h.Status != driver.StatusRunning || !reflect.DeepEqual(h.Endpoints, original.Endpoints) {
		t.Fatalf("checkpoint restore changed the business identity or endpoint bindings: %v, %v", h, err)
	}
	after := recoveryRecord(t, d, original.ID)
	if after.Restore != nil || after.LatestSnapshotID != "" || after.Phase != "running" || after.RelayKey != before.RelayKey {
		t.Fatal("successful restore lost the guest marker or left an incomplete intent")
	}
	if after.Provider.EnvdAccessToken != "resumed-envd-secret" || after.Provider.TrafficAccessToken != "resumed-traffic-secret" {
		t.Fatal("restore reused the vanished source guest's credentials")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	guest := f.sandboxes[h.Name]
	if f.created != 2 || f.connected != 1 || guest.TemplateID != before.LatestSnapshotID+":default" || guest.Metadata[idLabel] != original.ID || guest.Metadata[restoreLabel] == "" || f.relayKeys[h.Name] != before.RelayKey || len(f.env) != 0 {
		t.Fatal("restore did not use the pinned memory checkpoint and existing guest relay identity")
	}
}

func TestSnapshotStartReconcilesLostSourceDeleteAfterRestart(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	h, err := d.Create(t.Context(), lifecycleSpec("start-after-release"))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.beforeRequest = func(r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/sandboxes/"+h.Name {
			delete(f.sandboxes, h.Name)
			f.deleted++
			f.deleteStatus = http.StatusInternalServerError
			f.beforeRequest = nil
		}
	}
	f.mu.Unlock()
	if err := d.PauseSnapshot(t.Context(), h.ID, 3); err == nil {
		t.Fatal("fixture did not lose the accepted source deletion response")
	}
	saved := recoveryRecord(t, d, h.ID)
	if saved.Phase != "releasing" || saved.LatestSnapshotID == "" || saved.BackupKeep != 3 {
		t.Fatal("lost deletion response discarded the committed checkpoint or retention policy")
	}
	if status, err := d.Status(t.Context(), h.ID); err != nil || status != driver.StatusStopped {
		t.Fatalf("confirmed source absence was not observable: %s %v", status, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = openLifecycleDriver(t, opts)
	f.mu.Lock()
	f.deleteStatus = 0
	f.mu.Unlock()
	if err := d.Start(t.Context(), h.ID); err != nil {
		t.Fatalf("direct Start could not finish the persisted release intent: %v", err)
	}
	restored, err := d.Get(t.Context(), h.ID)
	if err != nil || restored.ID != h.ID || restored.Name == h.Name || restored.Status != driver.StatusRunning || !reflect.DeepEqual(restored.Endpoints, h.Endpoints) {
		t.Fatalf("release reconciliation lost the stable sandbox identity/endpoints: %v %v", restored, err)
	}
	finished := recoveryRecord(t, d, h.ID)
	if finished.Phase != "running" || finished.Restore != nil || finished.BackupKeep != 3 || len(finished.RecoveryPoints) != 1 || finished.RecoveryPoints[0].ID != saved.LatestSnapshotID {
		t.Fatal("direct Start replaced the saved checkpoint or retention policy")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.created != 2 || f.deleted != 1 || f.snapshotCreateRequests != 1 || f.paused != 0 {
		t.Fatal("release recovery recreated a checkpoint or duplicated provider workloads")
	}
}

func TestSnapshotStartPreservesUncertainReleaseOnProviderErrors(t *testing.T) {
	for _, failure := range []string{"get-401", "get-500", "delete-500", "ownership"} {
		t.Run(failure, func(t *testing.T) {
			f := newLifecycleProvider(t)
			opts := lifecycleOptions(t, f)
			d := openLifecycleDriver(t, opts)
			h, err := d.Create(t.Context(), lifecycleSpec("start-release-error"))
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			f.deleteStatus = http.StatusInternalServerError
			f.mu.Unlock()
			if err := d.PauseSnapshot(t.Context(), h.ID, 3); err == nil {
				t.Fatal("source release failure was hidden")
			}
			saved := recoveryRecord(t, d, h.ID)
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			d = openLifecycleDriver(t, opts)
			f.mu.Lock()
			f.deleteStatus = 0
			switch failure {
			case "get-401":
				f.getStatus = http.StatusUnauthorized
			case "get-500":
				f.getStatus = http.StatusInternalServerError
			case "delete-500":
				f.deleteStatus = http.StatusInternalServerError
			case "ownership":
				f.sandboxes[h.Name].Metadata[ownerLabel] = "other-owner"
			}
			f.mu.Unlock()
			if err := d.Start(t.Context(), h.ID); err == nil {
				t.Fatal("Start ignored provider failure or foreign source ownership")
			}
			after := recoveryRecord(t, d, h.ID)
			if after.Phase != "releasing" || after.Restore != nil || after.BackupKeep != 3 || after.LatestSnapshotID != saved.LatestSnapshotID {
				t.Fatal("failed Start erased the release intent or began an unsafe restore")
			}
			f.mu.Lock()
			if f.created != 1 || f.deleted != 0 || f.snapshotCreateRequests != 1 || f.sandboxes[h.Name] == nil {
				t.Error("Start created a clone or deleted an unverified source")
			}
			f.getStatus, f.deleteStatus = 0, 0
			f.sandboxes[h.Name].Metadata[ownerLabel] = opts.OwnerID
			f.mu.Unlock()
			if err := d.Start(t.Context(), h.ID); err != nil {
				t.Fatalf("Start could not retry once source release was safe: %v", err)
			}
		})
	}
}

func TestSnapshotRestoreDoesNotTreatProviderErrorsAsNativeLoss(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			d, f, _, h := checkpointedLifecycle(t, "provider-error")
			before := recoveryRecord(t, d, h.ID)
			f.mu.Lock()
			f.getStatus = status
			f.mu.Unlock()
			if err := d.Start(t.Context(), h.ID); err == nil {
				t.Fatal("provider failure was converted to a successful restore")
			}
			after := recoveryRecord(t, d, h.ID)
			if after.Restore != nil || after.LatestSnapshotID != before.LatestSnapshotID || after.Provider.ID != h.Name || after.Phase != "paused" {
				t.Fatal("provider failure changed the paused recovery identity")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.created != 1 || f.connected != 0 {
				t.Fatal("authentication/server failure created or resumed another guest")
			}
		})
	}
}

func TestSnapshotRestoreMissingLatestNeverUsesOlderCheckpoint(t *testing.T) {
	d, f, _, h := checkpointedLifecycle(t, "missing-latest")
	first := recoveryRecord(t, d, h.ID).LatestSnapshotID
	if err := d.Start(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.PauseSnapshot(t.Context(), h.ID, 3); err != nil {
		t.Fatal(err)
	}
	before := recoveryRecord(t, d, h.ID)
	latest := before.LatestSnapshotID
	if first == latest || len(before.RecoveryPoints) != 2 {
		t.Fatal("test requires an older and a newer retained checkpoint")
	}
	f.mu.Lock()
	delete(f.sandboxes, h.Name)
	delete(f.snapshots, latest)
	f.mu.Unlock()
	for range 2 {
		if err := d.Start(t.Context(), h.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing latest checkpoint did not surface provider rejection: %v", err)
		}
	}
	after := recoveryRecord(t, d, h.ID)
	if after.LatestSnapshotID != latest || after.Restore == nil || after.Restore.SnapshotID != latest || after.Restore.Dispatched || after.Restore.SandboxID != "" {
		t.Fatal("latest checkpoint rejection erased or replaced the recovery intent")
	}
	if status, err := d.Status(t.Context(), h.ID); err != nil || status != driver.StatusStopped {
		t.Fatalf("explicit restore rejection retained a nonexistent compute allocation: %s %v", status, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.created != 2 || f.snapshots[first] == nil || len(f.sandboxes) != 0 {
		t.Fatal("missing latest checkpoint silently restored an older or empty sandbox")
	}
}

func TestSnapshotRestoreLostCreateResponseReconcilesWithoutAnotherPost(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(fmt.Sprintf("restart=%t", restart), func(t *testing.T) {
			d, f, opts, h := checkpointedLifecycle(t, "restore-uncertain")
			lists := 0
			f.mu.Lock()
			delete(f.sandboxes, h.Name)
			f.createStatus = http.StatusInternalServerError
			f.beforeRequest = func(r *http.Request) {
				if r.URL.Path == "/v2/sandboxes" {
					lists++
				}
			}
			f.mu.Unlock()
			if err := d.Start(t.Context(), h.ID); err == nil {
				t.Fatal("fixture did not lose the restore response")
			}
			attempt := recoveryRecord(t, d, h.ID).Restore
			if attempt == nil || !attempt.Dispatched || attempt.SandboxID != "" || attempt.SourceID != h.Name {
				t.Fatal("ambiguous restore was not durably journaled before retry")
			}
			if status, err := d.Status(t.Context(), h.ID); err != nil || status != driver.StatusPending {
				t.Fatalf("uncertain allocation released its quota: %s, %v", status, err)
			}
			if restart {
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
				d = openLifecycleDriver(t, opts)
				if got := recoveryRecord(t, d, h.ID).Restore; got == nil || got.Token != attempt.Token {
					t.Fatal("restart lost the restore request's idempotency identity")
				}
			}
			f.mu.Lock()
			f.createStatus = 0
			f.mu.Unlock()
			if err := d.Start(t.Context(), h.ID); err != nil {
				t.Fatal(err)
			}
			restored, err := d.Get(t.Context(), h.ID)
			if err != nil || restored.ID != h.ID || restored.Name != "guest-2" || restored.Status != driver.StatusRunning {
				t.Fatalf("owned restore guest was not adopted: %v, %v", restored, err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.created != 2 || f.connected != 1 || lists != 1 || f.sandboxes[restored.Name].Metadata[restoreLabel] != attempt.Token {
				t.Fatal("retry recreated the guest instead of discovering the owned attempt")
			}
		})
	}
}

func TestSnapshotRestoreCancellationKeepsUnknownAllocationUntilReconciled(t *testing.T) {
	d, f, opts, h := checkpointedLifecycle(t, "cancel-uncertain")
	f.mu.Lock()
	delete(f.sandboxes, h.Name)
	f.createStatus = http.StatusInternalServerError
	f.mu.Unlock()
	if err := d.Start(t.Context(), h.ID); err == nil {
		t.Fatal("fixture did not lose the restore response")
	}
	f.mu.Lock()
	created := f.sandboxes["guest-2"]
	delete(f.sandboxes, created.ID) // Simulate a temporarily incomplete provider list.
	f.createStatus = 0
	f.mu.Unlock()
	err := d.Destroy(t.Context(), h.ID)
	var pending *driver.CleanupPendingError
	if err == nil || errors.As(err, &pending) && pending.WorkloadReleased {
		t.Fatalf("cancellation claimed an unknown allocation was released: %v", err)
	}
	r := recoveryRecord(t, d, h.ID)
	if r.Phase != "deleting" || r.Restore == nil || !r.Restore.Dispatched || len(r.RecoveryPoints) != 1 {
		t.Fatal("cancellation discarded pending restore or owned checkpoint cleanup")
	}
	if status, err := d.Status(t.Context(), h.ID); err != nil || status != driver.StatusPending {
		t.Fatalf("cancellation released capacity while creation remained uncertain: %s, %v", status, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = openLifecycleDriver(t, opts)
	if err := d.Destroy(t.Context(), h.ID); err == nil {
		t.Fatal("restart treated the incomplete provider list as proof of absence")
	}
	f.mu.Lock()
	f.sandboxes[created.ID] = created
	f.mu.Unlock()
	if err := d.Destroy(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.statePath(h.ID)); !os.IsNotExist(err) {
		t.Fatalf("completed cleanup retained its journal: %v", err)
	}
	if status, err := d.Status(t.Context(), h.ID); err != nil || status != driver.StatusNotFound {
		t.Fatalf("completed cancellation still owns capacity: %s, %v", status, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.created != 2 || f.deleted != 2 || len(f.sandboxes) != 0 || len(f.snapshots) != 0 || f.snapshotDeleted != 1 {
		t.Fatal("reconciled cancellation leaked a guest/checkpoint or replayed creation")
	}
}
