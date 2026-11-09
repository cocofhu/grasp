package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/workspacesnapshot"
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

func TestWorkspaceGatewayIdentityStartsAuthenticatedStandby(t *testing.T) {
	for _, resource := range []string{"", "ebe8cbce-a0f"} {
		t.Run("resource="+resource, func(t *testing.T) {
			env := workspaceStartupFixture(t)
			control, restore := env["GRASP_WORKSPACE_CONTROL_ROOT"], env["GRASP_WORKSPACE_RESTORE_ROOT"]
			delete(env, "GRASP_WORKSPACE_CONTROL_ROOT")
			delete(env, "GRASP_WORKSPACE_RESTORE_ROOT")
			env["GRASP_SANDBOX_RESOURCE_ID"] = resource
			limits, _ := json.Marshal(workspacesnapshot.DefaultLimits())
			env["GRASP_WORKSPACE_FILE_LIMITS"] = string(limits)
			controller, err := newWorkspaceOperations(func(key string) string { return env[key] }, true)
			if controller != nil || !errors.Is(err, errWorkspaceStandby) {
				t.Fatal("trusted pre-configuration startup did not enter blocked standby", err)
			}
			if env["GRASP_SANDBOX_RESOURCE_ID"] != resource {
				t.Fatal("standby changed its expected gateway resource identity")
			}
			for _, root := range []string{control, restore} {
				if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("standby initialized private roots before platform configuration", root, err)
				}
			}
		})
	}
}

func TestWorkspaceGatewayStandbyRejectsPartialOrUnauthenticatedConfiguration(t *testing.T) {
	for _, bad := range []string{"authentication", "lineage", "invalid_lineage", "invalid_resource", "short_resource", "control", "restore", "limits", "invalid_limits", "workspace"} {
		t.Run(bad, func(t *testing.T) {
			env := workspaceStartupFixture(t)
			delete(env, "GRASP_WORKSPACE_CONTROL_ROOT")
			delete(env, "GRASP_WORKSPACE_RESTORE_ROOT")
			limits, _ := json.Marshal(workspacesnapshot.DefaultLimits())
			env["GRASP_WORKSPACE_FILE_LIMITS"] = string(limits)
			switch bad {
			case "lineage":
				delete(env, "GRASP_EXECUTION_LINEAGE")
			case "invalid_lineage":
				env["GRASP_EXECUTION_LINEAGE"] = "foreign/lineage"
			case "invalid_resource":
				env["GRASP_SANDBOX_RESOURCE_ID"] = "foreign/resource"
			case "short_resource":
				env["GRASP_SANDBOX_RESOURCE_ID"] = "short"
			case "control":
				env["GRASP_WORKSPACE_CONTROL_ROOT"] = filepath.Join(t.TempDir(), "control")
			case "restore":
				env["GRASP_WORKSPACE_RESTORE_ROOT"] = filepath.Join(t.TempDir(), "restores")
			case "limits":
				delete(env, "GRASP_WORKSPACE_FILE_LIMITS")
			case "invalid_limits":
				env["GRASP_WORKSPACE_FILE_LIMITS"] = "{}"
			case "workspace":
				env["WORKSPACE_DIR"] = "relative-workspace"
			}
			controller, err := newWorkspaceOperations(func(key string) string { return env[key] }, bad != "authentication")
			if controller != nil || err == nil || errors.Is(err, errWorkspaceStandby) {
				t.Fatal("invalid startup was accepted as a configured or standby runtime", err)
			}
		})
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
