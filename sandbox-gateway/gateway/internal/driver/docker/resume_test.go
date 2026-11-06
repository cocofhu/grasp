package docker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
)

func TestStatusDoesNotTreatDockerOutageAsAbsence(t *testing.T) {
	for _, failure := range []error{errors.New("Cannot connect to the Docker daemon"), context.DeadlineExceeded, context.Canceled} {
		m := newMock()
		m.on("inspect", "", failure)
		d := New(Options{})
		d.run = m.run
		status, err := d.Status(context.Background(), "resident")
		if err == nil || status == driver.StatusNotFound {
			t.Fatalf("outage became absence: %q %v", status, err)
		}
	}
}

func TestPrepareStartRepairsStoppedContainerInPlace(t *testing.T) {
	d := New(Options{})
	var content string
	var copiedPath string
	d.run = func(_ context.Context, _ time.Duration, args ...string) (string, error) {
		switch args[0] {
		case "inspect":
			return "exited", nil
		case "cp":
			copiedPath = args[1]
			data, err := os.ReadFile(args[1])
			if err != nil {
				return "", err
			}
			content = string(data)
			info, _ := os.Stat(args[1])
			if info.Mode().Perm() != 0o700 {
				t.Errorf("bootstrap permissions: %v", info.Mode())
			}
			if args[2] != "sbx-original:/grasp-bootstrap.sh" {
				t.Errorf("wrong destination: %v", args)
			}
			cmd := exec.Command("bash", "-n", args[1])
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("invalid script: %v %s", err, out)
			}
			return "", nil
		default:
			t.Fatalf("prepare must not create/start/remove a container: %v", args)
			return "", nil
		}
	}
	if err := d.PrepareStart(context.Background(), "original", driver.StartOptions{BootstrapScript: "#!/bin/bash\nexit 0\n", RuntimeURL: "http://server/new", RuntimeHeaders: "Authorization: Bearer a'b$(false)"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "export SANDBOX_INJECT=''") || !strings.Contains(content, "http://server/new") || !strings.Contains(content, "a'\\''b$(false)") {
		t.Fatalf("bootstrap inputs not safely repaired: %s", content)
	}
	if _, err := os.Stat(copiedPath); !os.IsNotExist(err) {
		t.Fatal("temporary credential file was retained")
	}
}

func TestPrepareStartFailureDoesNotRemoveContainer(t *testing.T) {
	for _, tc := range []struct {
		status, script string
		copyErr        bool
	}{
		{"running", "#!/bin/bash\n", false},
		{"exited", "invalid", false},
		{"exited", "#!/bin/bash\n", true},
	} {
		m := newMock()
		m.on("inspect", tc.status, nil)
		if tc.copyErr {
			m.on("cp", "", errors.New("copy failed"))
		}
		d := New(Options{})
		d.run = m.run
		if err := d.PrepareStart(context.Background(), "resident", driver.StartOptions{BootstrapScript: tc.script, RuntimeURL: "http://runtime"}); err == nil {
			t.Fatal("expected prepare failure")
		}
		if len(m.callsWith("rm")) != 0 || len(m.callsWith("run")) != 0 {
			t.Fatal("prepare failure destroyed the original")
		}
	}
}
