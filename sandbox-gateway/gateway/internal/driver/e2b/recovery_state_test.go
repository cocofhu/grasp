package e2b

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func checkpointStateRecord() record {
	r := privateStateRecord()
	r.Phase = "paused"
	r.BackupKeep = 2
	r.RecoveryPoints = []recoveryPoint{{Name: "grasp-checkpoint", ID: "snapshot-id", SourceID: r.Provider.ID, CreatedAt: time.Now().UTC(), Dispatched: true, Ready: true}}
	r.LatestSnapshotID = "snapshot-id"
	r.Restore = &snapshotRestore{Token: "restore-attempt", SnapshotID: "snapshot-id", SourceID: r.Provider.ID, Dispatched: true}
	return r
}

func TestRecoveryStateReadsLegacyV1AndPersistsV2(t *testing.T) {
	d := privateStateDriver(t)
	r := privateStateRecord()
	r.Phase = "paused"
	if err := d.save(&r); err != nil {
		t.Fatal(err)
	}
	r.Version = 1
	legacy, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.statePath(r.ID), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.load(); err != nil {
		t.Fatalf("existing native-pause journal no longer loads: %v", err)
	}
	loaded := &d.entries[r.ID].record
	if loaded.Version != 1 || loaded.Phase != "paused" || loaded.Provider.ID != r.Provider.ID || loaded.Provider.EnvdAccessToken != r.Provider.EnvdAccessToken || loaded.Provider.TrafficAccessToken != r.Provider.TrafficAccessToken || !reflect.DeepEqual(loaded.Ports, r.Ports) || len(loaded.RecoveryPoints) != 0 {
		t.Fatal("legacy load changed the existing sandbox or its credentials/endpoints")
	}
	if err := d.save(loaded); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(d.statePath(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	var persisted record
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Version != 2 || persisted.ID != r.ID || persisted.Provider.ID != r.Provider.ID || persisted.EnvdToken != r.EnvdToken || persisted.TrafficToken != r.TrafficToken {
		t.Fatal("next journal update did not preserve identity while writing v2")
	}
}

func TestRecoveryStatePreservesCaptureRestoreAndCleanupAcrossRestart(t *testing.T) {
	for name, prepare := range map[string]func(*record){
		"ambiguous-restore": func(r *record) {},
		"known-restored-guest": func(r *record) {
			r.Restore.SandboxID = "restored-guest"
		},
		"committed-release": func(r *record) {
			r.Restore, r.Phase = nil, "releasing"
		},
		"pending-capture": func(r *record) {
			r.Restore, r.LatestSnapshotID = nil, ""
			r.CaptureName = "grasp-capture"
			r.RecoveryPoints = append(r.RecoveryPoints, recoveryPoint{Name: r.CaptureName, SourceID: r.Provider.ID, Dispatched: true})
		},
		"pending-deletion": func(r *record) {
			r.Restore, r.LatestSnapshotID, r.Phase = nil, "", "deleting"
			r.RecoveryPoints[0].Deleting = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := privateStateDriver(t)
			r := checkpointStateRecord()
			prepare(&r)
			if err := d.save(&r); err != nil {
				t.Fatal(err)
			}
			if err := d.load(); err != nil {
				t.Fatal(err)
			}
			got := d.entries[r.ID].record
			if got.Version != 2 || got.BackupKeep != r.BackupKeep || got.CaptureName != r.CaptureName || got.LatestSnapshotID != r.LatestSnapshotID || !reflect.DeepEqual(got.Restore, r.Restore) || !reflect.DeepEqual(got.RecoveryPoints, r.RecoveryPoints) {
				t.Fatal("journal reload lost the pending capture/restore/cleanup intent")
			}
		})
	}
}

func TestRecoveryStateRejectsMalformedReferencesBeforeProviderCalls(t *testing.T) {
	for name, change := range map[string]func(*record){
		"snapshot-name":    func(r *record) { r.RecoveryPoints[0].Name = "foreign-name" },
		"snapshot-source":  func(r *record) { r.RecoveryPoints[0].SourceID = "../foreign" },
		"snapshot-id":      func(r *record) { r.RecoveryPoints[0].ID = "../foreign" },
		"ready-without-id": func(r *record) { r.RecoveryPoints[0].ID = "" },
		"duplicate-name":   func(r *record) { r.RecoveryPoints = append(r.RecoveryPoints, r.RecoveryPoints[0]) },
		"duplicate-immutable-id": func(r *record) {
			p := r.RecoveryPoints[0]
			p.Name = "grasp-other"
			r.RecoveryPoints = append(r.RecoveryPoints, p)
		},
		"missing-capture": func(r *record) { r.CaptureName = "grasp-absent" },
		"deleting-capture": func(r *record) {
			r.Restore, r.LatestSnapshotID = nil, ""
			r.CaptureName = r.RecoveryPoints[0].Name
			r.RecoveryPoints[0].Deleting = true
		},
		"missing-latest":     func(r *record) { r.LatestSnapshotID = "absent" },
		"unfinished-latest":  func(r *record) { r.RecoveryPoints[0].Ready = false },
		"deleting-latest":    func(r *record) { r.RecoveryPoints[0].Deleting = true },
		"negative-retention": func(r *record) { r.BackupKeep = -1 },
		"release-without-checkpoint": func(r *record) {
			r.Restore, r.LatestSnapshotID, r.Phase = nil, "", "releasing"
		},
		"restore-token":          func(r *record) { r.Restore.Token = "../token" },
		"restore-source":         func(r *record) { r.Restore.SourceID = "../source" },
		"restore-from-older":     func(r *record) { r.Restore.SnapshotID = "older-id" },
		"restore-without-latest": func(r *record) { r.Restore.SnapshotID, r.LatestSnapshotID = "", "" },
		"restore-guest":          func(r *record) { r.Restore.SandboxID = "../guest" },
		"undispatched-guest": func(r *record) {
			r.Restore.Dispatched = false
			r.Restore.SandboxID = "restored-guest"
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := privateStateDriver(t)
			r := checkpointStateRecord()
			if err := d.save(&r); err != nil {
				t.Fatal(err)
			}
			change(&r)
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(d.statePath(r.ID), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := d.load(); err == nil {
				t.Fatal("unsafe checkpoint/restore reference was accepted")
			}
			if len(d.entries) != 0 {
				t.Fatal("invalid state became available to provider cleanup or restore")
			}
		})
	}
}
