package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func workspaceStartupFixture(t *testing.T) map[string]string {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"WORKSPACE_DIR":                workspace,
		"GRASP_EXECUTION_LINEAGE":      "retained-lineage-original",
		"GRASP_SANDBOX_RESOURCE_ID":    "resource-original",
		"GRASP_WORKSPACE_CONTROL_ROOT": filepath.Join(root, "private-control"),
		"GRASP_WORKSPACE_RESTORE_ROOT": filepath.Join(root, "isolated-restores"),
	}
}

func TestWorkspaceOperationsRequireCompleteAuthenticatedStartup(t *testing.T) {
	for _, missing := range []string{"GRASP_EXECUTION_LINEAGE", "GRASP_SANDBOX_RESOURCE_ID", "WORKSPACE_DIR", "GRASP_WORKSPACE_CONTROL_ROOT", "GRASP_WORKSPACE_RESTORE_ROOT", "authentication"} {
		t.Run(missing, func(t *testing.T) {
			env := workspaceStartupFixture(t)
			authenticated := missing != "authentication"
			delete(env, missing)
			controller, err := newWorkspaceOperations(func(key string) string { return env[key] }, authenticated)
			if err == nil || controller != nil {
				t.Fatal("partial or unauthenticated startup advertised snapshot capability")
			}
		})
	}
	controller, err := newWorkspaceOperations(func(key string) string {
		if key == "GRASP_SANDBOX_RESOURCE_ID" {
			return "gateway-provided-resource"
		}
		return ""
	}, true)
	if err != nil || controller != nil {
		t.Fatal("ordinary bridge unexpectedly initialized workspace operations", err)
	}
}

func TestWorkspaceOperationsRetainIdentityAcrossRuntimeRestart(t *testing.T) {
	env := workspaceStartupFixture(t)
	read := func(key string) string { return env[key] }
	controller, err := newWorkspaceOperations(read, true)
	if err != nil {
		t.Fatal(err)
	}
	caps := controller.Capabilities()
	if !caps.Capture || !caps.Read || !caps.Restore || caps.ProcessMemory || caps.Jobs.Resource != env["GRASP_SANDBOX_RESOURCE_ID"] || caps.Jobs.Lineage != env["GRASP_EXECUTION_LINEAGE"] {
		t.Fatal("runtime advertised capabilities without complete file controller")
	}
	if err := controller.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	controller, err = newWorkspaceOperations(read, true)
	if err != nil {
		t.Fatal("same identity failed to reopen its retained journal", err)
	}
	if err := controller.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	env["GRASP_EXECUTION_LINEAGE"] = "different-lineage-after-restore"
	controller, err = newWorkspaceOperations(read, true)
	if err == nil || controller != nil {
		t.Fatal("runtime rebound an old journal to a different external lineage")
	}
}

func TestWorkspaceOperationsRejectOverlappingOrSymlinkRoots(t *testing.T) {
	for _, bad := range []string{"source", "control/restore", "symlink", "public"} {
		t.Run(bad, func(t *testing.T) {
			env := workspaceStartupFixture(t)
			control := env["GRASP_WORKSPACE_CONTROL_ROOT"]
			switch bad {
			case "source":
				env["GRASP_WORKSPACE_CONTROL_ROOT"] = env["WORKSPACE_DIR"]
			case "control/restore":
				env["GRASP_WORKSPACE_RESTORE_ROOT"] = filepath.Join(control, "restored")
			case "symlink":
				if err := os.Symlink(t.TempDir(), control); err != nil {
					t.Fatal(err)
				}
			case "public":
				if err := os.Mkdir(control, 0755); err != nil {
					t.Fatal(err)
				}
			}
			controller, err := newWorkspaceOperations(func(key string) string { return env[key] }, true)
			if err == nil || controller != nil {
				t.Fatal("unsafe startup roots were accepted")
			}
		})
	}
}
