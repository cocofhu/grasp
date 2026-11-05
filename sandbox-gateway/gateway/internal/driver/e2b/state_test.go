package e2b

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateStateDriver(t *testing.T) *Driver {
	t.Helper()
	d := &Driver{opts: Options{OwnerID: "state-owner", StateDir: t.TempDir()}, entries: map[string]*entry{}}
	t.Cleanup(func() {
		if d.lockFile != nil {
			_ = d.lockFile.Close()
		}
	})
	return d
}

func privateStateRecord() record {
	return record{ID: "state-id", Phase: "running", RelayKey: "private-relay-secret", Provider: Sandbox{ID: "provider-id", EnvdAccessToken: "private-envd-token", TrafficAccessToken: "private-traffic-token", Metadata: map[string]string{ownerLabel: "state-owner", idLabel: "state-id", driverLabel: "e2b"}}, Ports: map[int]int{22: 34567}, Targets: map[int]int{22: 22}, Secrets: []string{"private-user-value"}, Profile: TemplateProfile{Image: "image", TemplateID: "template", CPUCores: 2, MemoryMB: 3072, DiskGi: 20}}
}

func TestStatePrivateAtomicRoundTrip(t *testing.T) {
	d := privateStateDriver(t)
	r := privateStateRecord()
	if err := d.save(&r); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(d.statePath(r.ID))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private record permission %v %v", info, err)
	}
	data, err := os.ReadFile(d.statePath(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "private-envd-token") || !strings.Contains(string(data), "private-traffic-token") {
		t.Fatal("private recovery credentials were not durably stored")
	}
	r.Phase = "paused"
	if err := d.save(&r); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.opts.StateDir, ".pending-ignored"), []byte("not a committed record"), 0600); err != nil {
		t.Fatal(err)
	}
	loaded := &Driver{opts: d.opts, entries: map[string]*entry{}}
	t.Cleanup(func() {
		if loaded.lockFile != nil {
			_ = loaded.lockFile.Close()
		}
	})
	if err := loaded.load(); err != nil {
		t.Fatal(err)
	}
	got := loaded.entries[r.ID].record
	if got.Phase != "paused" || got.Provider.EnvdAccessToken != r.Provider.EnvdAccessToken || got.Provider.TrafficAccessToken != r.Provider.TrafficAccessToken || got.Ports[22] != 34567 || got.Targets[22] != 22 {
		t.Fatal("recovery lost private credentials, phase or stable endpoints")
	}
	dir, err := os.Stat(d.opts.StateDir)
	if err != nil || dir.Mode().Perm() != 0700 {
		t.Fatal("state directory is not private")
	}
}

func TestStateRejectsCorruptForeignAndInsecureRecords(t *testing.T) {
	for name, change := range map[string]func(*record){"version": func(r *record) { r.Version = 2 }, "owner": func(r *record) { r.Owner = "other" }, "id": func(r *record) { r.ID = "../escape" }, "provider-owner": func(r *record) { r.Provider.Metadata[ownerLabel] = "other" },
		"phase":          func(r *record) { r.Phase = "unknown" },
		"target":         func(r *record) { r.Targets[22] = 49983 },
		"local-port":     func(r *record) { r.Ports[22] = 0 },
		"missing-target": func(r *record) { delete(r.Targets, 22) },
		"relay-key":      func(r *record) { r.RelayKey = "" },
		"traffic-token":  func(r *record) { r.TrafficToken = "" },
		"envd-token":     func(r *record) { r.EnvdToken = "" },
	} {
		t.Run(name, func(t *testing.T) {
			d := privateStateDriver(t)
			r := privateStateRecord()
			if err := d.save(&r); err != nil {
				t.Fatal(err)
			}
			change(&r)
			b, _ := json.Marshal(r)
			if err := os.WriteFile(d.statePath("state-id"), b, 0600); err != nil {
				t.Fatal(err)
			}
			if err := d.load(); err == nil {
				t.Fatal("unsafe recovery record accepted")
			}
		})
	}
	for _, name := range []string{"malformed", "oversized", "public-mode", "symlink", "directory", "wrong-name"} {
		t.Run(name, func(t *testing.T) {
			d := privateStateDriver(t)
			r := privateStateRecord()
			if err := d.save(&r); err != nil {
				t.Fatal(err)
			}
			p := d.statePath(r.ID)
			switch name {
			case "malformed":
				if err := os.WriteFile(p, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(p, []byte(strings.Repeat("x", (8<<20)+1)), 0600); err != nil {
					t.Fatal(err)
				}
			case "public-mode":
				if err := os.Chmod(p, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(p, p+".target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+".target", p); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(p, 0700); err != nil {
					t.Fatal(err)
				}
			case "wrong-name":
				if err := os.Rename(p, d.statePath("other-id")); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.load(); err == nil {
				t.Fatal("unsafe recovery filesystem accepted")
			}
		})
	}
}

func TestStateExclusiveOwnerLockAndSymlinkRefusal(t *testing.T) {
	d := privateStateDriver(t)
	if err := d.load(); err != nil {
		t.Fatal(err)
	}
	second := privateStateDriver(t)
	second.opts = d.opts
	if err := second.load(); err == nil {
		t.Fatal("second gateway acquired the same private recovery journal")
	}
	if err := d.lockFile.Close(); err != nil {
		t.Fatal(err)
	}
	d.lockFile = nil
	if err := second.load(); err != nil {
		t.Fatalf("journal lock was not released: %v", err)
	}
	third := privateStateDriver(t)
	target := filepath.Join(t.TempDir(), "owned-file")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(third.opts.StateDir, ".lock")); err != nil {
		t.Fatal(err)
	}
	if err := third.load(); err == nil {
		t.Fatal("state lock followed a symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatal("lock operation modified the symlink destination")
	}
}

func TestStateDirectoryFailuresAndInterruptedCreate(t *testing.T) {
	d := privateStateDriver(t)
	d.opts.StateDir = "relative"
	if err := d.load(); err == nil {
		t.Fatal("relative private state directory accepted")
	}
	d = privateStateDriver(t)
	file := filepath.Join(d.opts.StateDir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	d.opts.StateDir = file
	if err := d.load(); err == nil {
		t.Fatal("regular file accepted as state directory")
	}
	d = privateStateDriver(t)
	target := d.opts.StateDir
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	d.opts.StateDir = link
	if err := d.load(); err == nil {
		t.Fatal("symlink state directory accepted")
	}
	d = privateStateDriver(t)
	r := privateStateRecord()
	r.Phase = "creating"
	r.Ports = nil
	r.Targets = nil
	if err := d.save(&r); err != nil {
		t.Fatal(err)
	}
	if err := d.load(); err != nil {
		t.Fatal(err)
	}
	loaded := d.entries[r.ID].record
	if loaded.Phase != "deleting" || loaded.Ports == nil || loaded.Targets == nil {
		t.Fatal("interrupted create was published or retained nil maps")
	}
	if h := handle(&r, nil, "pending"); h.ID != r.ID || h.Name != r.Provider.ID || h.Status != "pending" {
		t.Fatal("recovered handle identity changed")
	}
}

func TestStateSaveFailuresLeaveNoCommittedOrPendingSecrets(t *testing.T) {
	d := privateStateDriver(t)
	r := privateStateRecord()
	r.Profile.CPUCores = math.NaN()
	if err := d.save(&r); err == nil {
		t.Fatal("invalid record encoded")
	}
	r = privateStateRecord()
	original := d.opts.StateDir
	d.opts.StateDir = filepath.Join(original, "absent")
	if err := d.save(&r); err == nil {
		t.Fatal("save succeeded without a state directory")
	}
	d.opts.StateDir = original
	if err := os.Mkdir(d.statePath(r.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := d.save(&r); err == nil {
		t.Fatal("save replaced an existing directory")
	}
	files, err := os.ReadDir(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".pending-") {
			t.Fatal("failed save retained a pending secret file")
		}
	}
}
