package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"backend/internal/workspacebinding"
	"backend/internal/workspacesnapshot"
)

func configureFixture(t *testing.T) (runtimeWorkspaceConfigurer, runtimeWorkspaceConfiguration, map[string]string, *int, *bool) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "private ' configuration")
	root := filepath.Join(base, "resource-original")
	cfg := runtimeWorkspaceConfiguration{1, "resource-original", "lineage-original", filepath.Join(root, "control"), filepath.Join(root, "restores"), workspacesnapshot.DefaultLimits()}
	run := filepath.Join(base, "run")
	if err := os.MkdirAll(run, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "backend.pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "env"), []byte("export SAFE_CONFIG='kept'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(base, "original")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"GRASP_EXECUTION_LINEAGE": cfg.Lineage, "ACP_BRIDGE_PASSWORD": "synthetic-password", "WORKSPACE_DIR": workspace}
	calls, ready := new(int), new(bool)
	c := runtimeWorkspaceConfigurer{runDir: run, getenv: func(k string) string { return env[k] }, restart: func(context.Context) error { *calls++; *ready = true; return nil }, verify: func(context.Context, runtimeWorkspaceConfiguration) error {
		if !*ready {
			return workspacebinding.ErrUnknown
		}
		return nil
	}}
	return c, cfg, env, calls, ready
}
func configBody(cfg runtimeWorkspaceConfiguration) *bytes.Reader {
	data, _ := json.Marshal(cfg)
	return bytes.NewReader(data)
}
func TestWorkspaceConfigurePersistsQuotedPlatformTupleAndDoesNotRepeatRestart(t *testing.T) {
	c, cfg, env, calls, _ := configureFixture(t)
	if err := c.configure(t.Context(), configBody(cfg)); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatal("configuration did not restart exactly once", *calls)
	}
	cmd := exec.Command("bash", "-c", "source \"$1\"; printf '%s\\n' \"$SAFE_CONFIG\" \"$GRASP_SANDBOX_RESOURCE_ID\" \"$GRASP_WORKSPACE_CONTROL_ROOT\" \"$GRASP_WORKSPACE_RESTORE_ROOT\"", "configure-test", filepath.Join(c.runDir, "env"))
	output, err := cmd.Output()
	if err != nil || string(output) != "kept\n"+cfg.Resource+"\n"+cfg.ControlRoot+"\n"+cfg.RestoreRoot+"\n" {
		t.Fatal("tuple was not correctly shell quoted", err)
	}
	env["GRASP_SANDBOX_RESOURCE_ID"] = cfg.Resource
	if err := c.configure(t.Context(), configBody(cfg)); err != nil || *calls != 1 {
		t.Fatal("receipt check replayed backend restart", err, *calls)
	}
	data, err := os.ReadFile(filepath.Join(cfg.ControlRoot, "configuration.json"))
	var receipt runtimeWorkspaceConfigurationReceipt
	if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.State != "confirmed" {
		t.Fatal("no durable configuration proof", err)
	}
}
func TestWorkspaceConfigureUnknownACKOnlyObservesOriginalReceipt(t *testing.T) {
	c, cfg, env, calls, ready := configureFixture(t)
	c.restart = func(context.Context) error { *calls++; return errors.New("lost SSH acknowledgement") }
	if err := c.configure(t.Context(), configBody(cfg)); !errors.Is(err, workspacebinding.ErrUnknown) {
		t.Fatal(err)
	}
	if err := c.configure(t.Context(), configBody(cfg)); !errors.Is(err, workspacebinding.ErrUnknown) || *calls != 1 {
		t.Fatal("unknown original configuration restarted again", err, *calls)
	}
	env["GRASP_SANDBOX_RESOURCE_ID"] = cfg.Resource
	*ready = true
	if err := c.configure(t.Context(), configBody(cfg)); err != nil || *calls != 1 {
		t.Fatal("verified original configuration was not observed", err, *calls)
	}
}
func TestWorkspaceConfigureNewStoppedProcessRebuildsEnvWithoutChangingPersistentHead(t *testing.T) {
	c, cfg, env, calls, ready := configureFixture(t)
	if err := c.configure(t.Context(), configBody(cfg)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.ControlRoot, "configuration.json")
	data, _ := os.ReadFile(path)
	var receipt runtimeWorkspaceConfigurationReceipt
	_ = json.Unmarshal(data, &receipt)
	receipt.Incarnation = "previous-pod-process"
	data, _ = json.Marshal(receipt)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(cfg.ControlRoot, "retained-marker")
	if err := os.WriteFile(marker, []byte("persistent Return and journal bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	*ready = false
	delete(env, "GRASP_SANDBOX_RESOURCE_ID")
	if err := c.configure(t.Context(), configBody(cfg)); err != nil || *calls != 2 {
		t.Fatal("new standby could not rebuild ephemeral env", err, *calls)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "persistent Return and journal bytes" {
		t.Fatal("startup changed retained control bytes", err)
	}
}
func TestWorkspaceConfigureRejectsChangedTupleAndUnsafeRootsBeforeRestart(t *testing.T) {
	for _, kind := range []string{"lineage", "resource", "overlap", "symlink", "limits", "unknown_json"} {
		t.Run(kind, func(t *testing.T) {
			c, cfg, env, calls, _ := configureFixture(t)
			switch kind {
			case "lineage":
				env["GRASP_EXECUTION_LINEAGE"] = "different-lineage"
			case "resource":
				env["GRASP_SANDBOX_RESOURCE_ID"] = "different-resource"
			case "overlap":
				env["WORKSPACE_DIR"] = filepath.Dir(cfg.ControlRoot)
			case "symlink":
				if err := os.MkdirAll(filepath.Dir(cfg.ControlRoot), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(t.TempDir(), cfg.ControlRoot); err != nil {
					t.Fatal(err)
				}
			case "limits":
				cfg.Limits.MaxBytes = 0
			}
			var input *bytes.Reader
			if kind == "unknown_json" {
				data, _ := json.Marshal(cfg)
				input = bytes.NewReader([]byte(strings.TrimSuffix(string(data), "}") + `,"path":"/untrusted"}`))
			} else {
				input = configBody(cfg)
			}
			if err := c.configure(t.Context(), input); err == nil || *calls != 0 {
				t.Fatal("unsafe configuration reached restart", err, *calls)
			}
		})
	}
}
