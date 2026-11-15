package e2b

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"sandbox-gateway/internal/driver"
)

func TestSnapshotDefaultUsesNativePauseAndSurvivesGatewayRestart(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	h, err := d.Create(t.Context(), lifecycleSpec("native-pause"))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	f.mu.Lock()
	f.beforeRequest = func(r *http.Request) { paths = append(paths, r.URL.Path) }
	f.mu.Unlock()
	for range 2 {
		if err := d.Stop(t.Context(), h.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Endpoints(t.Context(), h.ID); err == nil {
		t.Fatal("paused sandbox advertised endpoints")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = openLifecycleDriver(t, opts)
	if status, err := d.Status(t.Context(), h.ID); err != nil || status != driver.StatusStopped {
		t.Fatalf("lost native pause: %s, %v", status, err)
	}
	if err := d.Start(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := d.Get(t.Context(), h.ID)
	if err != nil || restored.ID != h.ID || restored.Name != h.Name || !reflect.DeepEqual(restored.Endpoints, h.Endpoints) {
		t.Fatalf("native identity/endpoints changed: %v, %v", restored, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.paused != 1 || f.created != 1 || f.connected != 1 || f.snapshotCreateRequests != 0 || f.snapshotListed != 0 || f.aliasRequests != 0 {
		t.Fatal("keep=1 did not use native pause/connect alone")
	}
	for _, path := range paths {
		if strings.Contains(path, "snapshot") {
			t.Fatalf("default lifecycle depends on snapshot endpoint: %s", path)
		}
	}
}

func TestSnapshotRetentionKeepsFivePointsThenReturnsToNativeOnly(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	h, err := d.Create(t.Context(), lifecycleSpec("retention"))
	if err != nil {
		t.Fatal(err)
	}
	for cycle := 1; cycle <= 6; cycle++ {
		if err := d.PauseSnapshot(t.Context(), h.ID, 5); err != nil {
			t.Fatal(err)
		}
		if err := d.PauseSnapshot(t.Context(), h.ID, 5); err != nil {
			t.Fatal(err)
		}
		r := recoveryRecord(t, d, h.ID)
		if r.CaptureName != "" || latestRecoveryPoint(&r) == nil || len(r.RecoveryPoints) != min(cycle, 5) {
			t.Fatal("retention lost latest checkpoint or failed to remove oldest")
		}
		if cycle < 6 {
			if err := d.Start(t.Context(), h.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.mu.Lock()
	if f.paused != 0 || f.created != 6 || f.deleted != 6 || f.snapshotCreated != 6 || f.snapshotCreateRequests != 6 || f.snapshotDeleted != 1 || len(f.snapshots) != 5 {
		t.Error("repeated stop captured again, native pause duplicated snapshots, or retention failed")
	}
	f.mu.Unlock()
	latest := recoveryRecord(t, d, h.ID).LatestSnapshotID
	if err := d.PauseSnapshot(t.Context(), h.ID, 1); err != nil {
		t.Fatal(err)
	}
	r := recoveryRecord(t, d, h.ID)
	if len(r.RecoveryPoints) != 1 || r.LatestSnapshotID != latest {
		t.Fatal("reducing retention deleted the only authoritative recovery point")
	}
	if err := d.Start(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	r = recoveryRecord(t, d, h.ID)
	if len(r.RecoveryPoints) != 1 || !r.RecoveryPoints[0].Deleting || r.LatestSnapshotID != "" || r.BackupKeep != 1 {
		t.Fatal("native pause did not retain only its required base template dependency")
	}
	f.mu.Lock()
	if f.snapshotDeleted != 5 || len(f.snapshots) != 1 || f.sandboxes[r.Provider.ID].State != "paused" || f.paused != 1 {
		t.Error("native-only mode deleted its base dependency or retained unrelated history")
	}
	f.mu.Unlock()
	if err := d.Start(t.Context(), h.ID); err != nil {
		t.Fatalf("native pause could not resume using its retained template: %v", err)
	}
	if err := d.Destroy(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshotDeleted != 6 || len(f.snapshots) != 0 {
		t.Fatal("final guest deletion failed to remove the retained base dependency")
	}
}

func TestSnapshotCommittedReleaseResumesAfterGatewayRestartWithoutRecapture(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	h, err := d.Create(t.Context(), lifecycleSpec("release-restart"))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.deleteStatus = http.StatusInternalServerError
	f.mu.Unlock()
	if err := d.PauseSnapshot(t.Context(), h.ID, 2); err == nil {
		t.Fatal("release failure was hidden")
	}
	saved := recoveryRecord(t, d, h.ID)
	if saved.Phase != "releasing" || saved.LatestSnapshotID == "" || saved.CaptureName != "" {
		t.Fatal("source release did not retain the committed checkpoint")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = openLifecycleDriver(t, opts)
	status, err := d.Get(t.Context(), h.ID)
	if err != nil || status.Status != driver.StatusRunning || len(status.Endpoints) != 0 {
		t.Fatalf("pending source release did not account for compute safely: %v %v", status, err)
	}
	f.mu.Lock()
	f.deleteStatus = 0
	f.mu.Unlock()
	if err := d.PauseSnapshot(t.Context(), h.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := d.Start(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := d.Get(t.Context(), h.ID)
	if err != nil || restored.ID != h.ID || restored.Name == h.Name || !reflect.DeepEqual(restored.Endpoints, h.Endpoints) {
		t.Fatalf("release restart lost stable gateway endpoints: %v %v", restored, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshotCreateRequests != 1 || f.snapshotCreated != 1 || f.paused != 0 || f.deleted != 1 {
		t.Fatal("release retry recaptured an advanced source instead of using the saved checkpoint")
	}
}

func TestSnapshotNativePauseSkipsRejectedCaptureAndPreservesStateOnPauseFailure(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			testNativePauseAfterRejectedCapture(t, status)
		})
	}
}

func testNativePauseAfterRejectedCapture(t *testing.T, status int) {
	t.Helper()
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	h, err := d.Create(t.Context(), lifecycleSpec("native-after-rejection"))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.snapshotCreateStatus = status
	f.mu.Unlock()
	if err := d.PauseSnapshot(t.Context(), h.ID, 2); err == nil {
		t.Fatal("capture rejection was hidden")
	}
	f.mu.Lock()
	f.pauseStatus = http.StatusInternalServerError
	f.mu.Unlock()
	if err := d.Stop(t.Context(), h.ID); err == nil {
		t.Fatal("native pause failure was hidden")
	}
	if r := recoveryRecord(t, d, h.ID); r.Phase != "running" || len(r.RecoveryPoints) != 1 {
		t.Fatal("failed native pause erased pending state")
	}
	f.mu.Lock()
	f.pauseStatus = 0
	f.mu.Unlock()
	if err := d.Stop(t.Context(), h.ID); err != nil {
		t.Fatalf("explicitly rejected capture prevented fallback to native pause: %v", err)
	}
	if r := recoveryRecord(t, d, h.ID); r.Phase != "paused" || len(r.RecoveryPoints) != 0 {
		t.Fatal("native pause kept an explicitly rejected capture as an uncertain backup")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshotCreateRequests != 1 || f.paused != 1 || f.sandboxes[h.Name].State != "paused" {
		t.Fatal("keep=1 retried an unwanted capture or destroyed the native recovery point")
	}
}

func TestSnapshotLater404CannotDisproveEarlierAmbiguousCapture(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	h, err := d.Create(t.Context(), lifecycleSpec("ambiguous-before-rejection"))
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.snapshotCreateStatus, f.snapshotCreateAmbiguous = http.StatusInternalServerError, true
	f.mu.Unlock()
	if err := d.PauseSnapshot(t.Context(), h.ID, 2); err == nil {
		t.Fatal("fixture did not lose the accepted snapshot response")
	}
	r := recoveryRecord(t, d, h.ID)
	if len(r.RecoveryPoints) != 1 || !r.RecoveryPoints[0].Dispatched || r.RecoveryPoints[0].ID != "" {
		t.Fatal("ambiguous capture was not durably retained")
	}
	name := r.CaptureName
	f.mu.Lock()
	id := f.snapshotAliases[name]
	// The alias is temporarily invisible although the first capture was accepted.
	delete(f.snapshotAliases, name)
	delete(f.snapshotAliases, "team/"+name)
	f.snapshotCreateStatus, f.snapshotCreateAmbiguous = http.StatusNotFound, false
	f.mu.Unlock()
	if err := d.PauseSnapshot(t.Context(), h.ID, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second capture did not report its explicit rejection: %v", err)
	}
	r = recoveryRecord(t, d, h.ID)
	if r.CaptureName != name || len(r.RecoveryPoints) != 1 || !r.RecoveryPoints[0].Dispatched {
		t.Fatal("a later 404 erased the earlier ambiguous request")
	}
	var pending *driver.CleanupPendingError
	if err := d.Destroy(t.Context(), h.ID); !errors.As(err, &pending) || !pending.WorkloadReleased {
		t.Fatalf("cancellation discarded unresolved checkpoint cleanup: %v", err)
	}
	r = recoveryRecord(t, d, h.ID)
	if len(r.RecoveryPoints) != 1 || !r.RecoveryPoints[0].Dispatched || !r.RecoveryPoints[0].Deleting {
		t.Fatal("cleanup lost the unresolved snapshot tombstone")
	}
	f.mu.Lock()
	f.snapshotAliases[name] = id
	f.mu.Unlock()
	if err := d.Destroy(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshotCreateRequests != 2 || f.snapshotCreated != 1 || !f.deletedSnapshots[id] {
		t.Fatal("late discovery failed to delete the original accepted checkpoint")
	}
}

func TestSnapshotFailuresPreservePreviousPointAndRetrySameCapture(t *testing.T) {
	for _, failure := range []string{"rejected", "ambiguous", "pending", "list", "release"} {
		t.Run(failure, func(t *testing.T) {
			d, f, _, h := checkpointedLifecycle(t, "retry-capture")
			previous := recoveryRecord(t, d, h.ID).LatestSnapshotID
			if err := d.Start(t.Context(), h.ID); err != nil {
				t.Fatal(err)
			}
			source := recoveryRecord(t, d, h.ID).Provider.ID
			f.mu.Lock()
			switch failure {
			case "rejected":
				f.snapshotCreateStatus = http.StatusBadRequest
			case "ambiguous":
				f.snapshotCreateStatus, f.snapshotCreateAmbiguous = http.StatusInternalServerError, true
			case "pending":
				f.snapshotPending = true
			case "list":
				f.snapshotListStatus = http.StatusInternalServerError
			case "release":
				f.deleteStatus = http.StatusInternalServerError
			}
			f.mu.Unlock()
			if err := d.PauseSnapshot(t.Context(), h.ID, 2); err == nil {
				t.Fatal("failed capture/release declared successful")
			}
			failed := recoveryRecord(t, d, h.ID)
			captureName := failed.RecoveryPoints[0].Name
			if len(failed.RecoveryPoints) != 2 || failure != "release" && (failed.CaptureName == "" || failed.Phase != "running") || failure == "release" && (failed.Phase != "releasing" || failed.LatestSnapshotID == "") {
				t.Fatal("failure discarded capture/release intent")
			}
			f.mu.Lock()
			if f.sandboxes[source].State != "running" || f.snapshots[previous] == nil || f.snapshotDeleted != 0 {
				t.Error("failure deleted original workload or prior checkpoint")
			}
			requests := f.snapshotCreateRequests
			f.snapshotCreateStatus, f.snapshotListStatus, f.deleteStatus = 0, 0, 0
			f.snapshotPending = false
			for _, point := range f.snapshots {
				point.Ready = true
			}
			f.mu.Unlock()
			if err := d.PauseSnapshot(t.Context(), h.ID, 2); err != nil {
				t.Fatal(err)
			}
			after := recoveryRecord(t, d, h.ID)
			if after.CaptureName != "" || after.Phase != "paused" || after.RecoveryPoints[0].Name != captureName || after.LatestSnapshotID != after.RecoveryPoints[0].ID {
				t.Fatal("retry replaced saved capture identity")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if failure == "rejected" {
				requests++
			}
			if f.snapshotCreateRequests != requests || f.snapshotCreated != 2 || f.paused != 0 || f.sandboxes[source] != nil {
				t.Fatal("retry recaptured an already complete checkpoint or failed to release source")
			}
		})
	}
}

func TestSnapshotDeletionRetriesAfterComputeReleaseAndGatewayRestart(t *testing.T) {
	d, f, opts, h := checkpointedLifecycle(t, "delete-snapshots")
	f.mu.Lock()
	f.snapshotDeleteStatus = http.StatusInternalServerError
	f.mu.Unlock()
	err := d.Destroy(t.Context(), h.ID)
	var pending *driver.CleanupPendingError
	if !errors.As(err, &pending) || !pending.WorkloadReleased {
		t.Fatalf("snapshot cleanup failed to distinguish compute release: %v", err)
	}
	r := recoveryRecord(t, d, h.ID)
	if r.Phase != "deleting" || len(r.RecoveryPoints) != 1 || !r.RecoveryPoints[0].Deleting {
		t.Fatal("pending snapshot deletion lost journal")
	}
	if status, err := d.Status(t.Context(), h.ID); err != nil || status != driver.StatusNotFound {
		t.Fatalf("absent workload retained quota: %s, %v", status, err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = openLifecycleDriver(t, opts)
	if err := d.Destroy(t.Context(), h.ID); !errors.As(err, &pending) || !pending.WorkloadReleased {
		t.Fatalf("restart lost pending cleanup: %v", err)
	}
	f.mu.Lock()
	f.snapshotDeleteStatus = 0
	f.mu.Unlock()
	if err := d.Destroy(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(d.statePath(h.ID)); !os.IsNotExist(err) {
		t.Fatalf("finished cleanup retained tombstone: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleted != 1 || f.snapshotDeleted != 1 || len(f.snapshots) != 0 {
		t.Fatal("owned checkpoint cleanup did not finish")
	}
}

func TestSnapshotDeleteFailuresNeverManufactureComputeAbsence(t *testing.T) {
	for _, failure := range []string{"delete-500", "get-500", "get-401", "delete-accepted-still-visible"} {
		t.Run(failure, func(t *testing.T) {
			f := newLifecycleProvider(t)
			d := openLifecycleDriver(t, lifecycleOptions(t, f))
			h, err := d.Create(t.Context(), lifecycleSpec("delete-failure"))
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			switch failure {
			case "delete-500":
				f.deleteStatus = http.StatusInternalServerError
			case "get-500":
				f.getStatus = http.StatusInternalServerError
			case "get-401":
				f.getStatus = http.StatusUnauthorized
			default:
				f.deleteStatus = http.StatusNoContent
			}
			f.mu.Unlock()
			err = d.Destroy(t.Context(), h.ID)
			var pending *driver.CleanupPendingError
			if err == nil || errors.As(err, &pending) && pending.WorkloadReleased {
				t.Fatalf("failure reported as compute release: %v", err)
			}
			status, statusErr := d.Status(t.Context(), h.ID)
			if statusErr == nil && status != driver.StatusRunning {
				t.Fatalf("deleting row hid allocated workload: %s", status)
			}
			f.mu.Lock()
			f.deleteStatus, f.getStatus = 0, 0
			f.mu.Unlock()
			if err := d.Destroy(t.Context(), h.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSnapshotCleanupPinsImmutableIDDespiteAliasMove(t *testing.T) {
	d, f, _, h := checkpointedLifecycle(t, "pinned-delete")
	p := recoveryRecord(t, d, h.ID).RecoveryPoints[0]
	f.mu.Lock()
	f.snapshots["other-snapshot"] = &lifecycleSnapshot{ID: "other-snapshot", Name: p.Name, SourceID: "other-guest", Ready: true}
	f.snapshotAliases[p.Name], f.snapshotAliases["team/"+p.Name] = "other-snapshot", "other-snapshot"
	aliases := f.aliasRequests
	f.mu.Unlock()
	if err := d.Destroy(t.Context(), h.ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deletedSnapshots[p.ID] || f.deletedSnapshots["other-snapshot"] || f.snapshots["other-snapshot"] == nil || f.aliasRequests != aliases {
		t.Fatal("cleanup followed mutable alias instead of owned immutable ID")
	}
}

func TestSnapshotCleanupDistinguishesUndispatchedAndUnknownCaptures(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(fmt.Sprint(dispatched), func(t *testing.T) {
			f := newLifecycleProvider(t)
			opts := lifecycleOptions(t, f)
			d := openLifecycleDriver(t, opts)
			h, err := d.Create(t.Context(), lifecycleSpec("pending-cleanup"))
			if err != nil {
				t.Fatal(err)
			}
			e, _ := d.lookup(h.ID)
			e.mu.Lock()
			e.record.CaptureName = "grasp-pending"
			e.record.RecoveryPoints = []recoveryPoint{{Name: "grasp-pending", SourceID: h.Name, Dispatched: dispatched}}
			err = d.save(&e.record)
			e.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			err = d.Destroy(t.Context(), h.ID)
			if !dispatched {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(d.statePath(h.ID)); !os.IsNotExist(err) {
					t.Fatal("never dispatched capture prevented cleanup")
				}
				return
			}
			var pending *driver.CleanupPendingError
			if !errors.As(err, &pending) || !pending.WorkloadReleased {
				t.Fatalf("unknown capture lost cleanup after native deletion: %v", err)
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
			d = openLifecycleDriver(t, opts)
			if r := recoveryRecord(t, d, h.ID); len(r.RecoveryPoints) != 1 || !r.RecoveryPoints[0].Dispatched || !r.RecoveryPoints[0].Deleting {
				t.Fatal("restart discarded uncertain capture")
			}
			f.mu.Lock()
			f.snapshots["late-capture"] = &lifecycleSnapshot{ID: "late-capture", Name: "grasp-pending", SourceID: h.Name}
			f.snapshotAliases["grasp-pending"] = "late-capture"
			f.mu.Unlock()
			if err := d.Destroy(t.Context(), h.ID); err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.deletedSnapshots["late-capture"] {
				t.Fatal("late capture not cleaned up")
			}
		})
	}
}

func TestSnapshotClientOfficialPayloadAndIdentityChecks(t *testing.T) {
	client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sandboxes/source/snapshots" {
			t.Error("unexpected capture route")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(body, map[string]any{"name": "grasp-name", "memory": true}) {
			t.Error("nonstandard capture payload")
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(snapshotWire{ID: "immutable:default", Names: []string{"team/grasp-name:default"}})
	})
	if err := client.CreateSnapshot(t.Context(), "source", "grasp-name"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ source, name, pinned string }{{"../source", "grasp-name", ""}, {"source", "../name", ""}, {"source", "grasp-name", "../id"}} {
		if _, _, err := client.ResolveSnapshot(t.Context(), tc.source, tc.name, tc.pinned); err == nil {
			t.Fatal("invalid snapshot identity accepted")
		}
	}
	for _, tc := range []struct{ source, name string }{{"../source", "grasp-name"}, {"source", "../name"}} {
		if err := client.CreateSnapshot(t.Context(), tc.source, tc.name); err == nil {
			t.Fatal("invalid capture identity accepted")
		}
	}
	if err := client.DeleteSnapshot(t.Context(), "../template"); err == nil {
		t.Fatal("invalid deletion accepted")
	}
	for _, response := range []snapshotWire{{ID: "wrong:default"}, {ID: "a/b/grasp-name:default"}, {ID: "team/grasp-name:older"}} {
		client, _ := newControlTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(response)
		})
		if err := client.CreateSnapshot(t.Context(), "source", "grasp-name"); err == nil {
			t.Fatal("foreign snapshot response accepted")
		}
	}
}

func TestSnapshotClientResolveAndDeleteFailures(t *testing.T) {
	for _, mode := range []string{"missing", "alias-error", "invalid-alias", "list-error", "pending", "ready-name", "ready-id", "ready-tagged-id"} {
		t.Run(mode, func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/templates/aliases/grasp-name" {
					switch mode {
					case "missing":
						w.WriteHeader(http.StatusNotFound)
					case "alias-error":
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = fmt.Fprint(w, testProviderSecret)
					case "invalid-alias":
						_, _ = fmt.Fprint(w, `{"templateID":"../foreign"}`)
					default:
						_, _ = fmt.Fprint(w, `{"templateID":"immutable"}`)
					}
					return
				}
				if r.URL.Path != "/snapshots" || r.URL.Query().Get("sandboxID") != "source" || r.URL.Query().Get("name") != "immutable:default" || r.URL.Query().Get("limit") != "100" {
					t.Error("resolve did not pin source/immutable ID")
				}
				if mode == "list-error" {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = fmt.Fprint(w, testProviderSecret)
					return
				}
				point := snapshotWire{ID: "unrelated"}
				switch mode {
				case "ready-name":
					point.Names = []string{"team/grasp-name:default"}
				case "ready-id":
					point.ID = "immutable"
				case "ready-tagged-id":
					point.ID = "immutable:default"
				}
				_ = json.NewEncoder(w).Encode([]snapshotWire{point})
			})
			id, ready, err := client.ResolveSnapshot(t.Context(), "source", "grasp-name", "")
			switch mode {
			case "alias-error", "invalid-alias", "list-error":
				assertSafeControlError(t, err)
				if ready || mode == "list-error" && id != "immutable" {
					t.Fatal("failed resolution lost ID or claimed completion")
				}
			case "missing":
				if err != nil || id != "" || ready {
					t.Fatal("alias absence confused with failure")
				}
			default:
				if err != nil || id != "immutable" || ready != strings.HasPrefix(mode, "ready") {
					t.Fatalf("incorrect state: %s %t %v", id, ready, err)
				}
			}
		})
	}
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("delete-%d", status), func(t *testing.T) {
			client, _ := newControlTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != "/templates/immutable" {
					t.Error("deletion did not use immutable ID")
				}
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, testProviderSecret)
			})
			err := client.DeleteSnapshot(t.Context(), "immutable")
			if status == http.StatusInternalServerError {
				assertSafeControlError(t, err)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
