package service

import (
	"errors"
	"reflect"
	"testing"

	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"

	"gorm.io/gorm"
)

func TestColdResumeRefreshesCurrentToolInputsWithoutChangingWorkloadLayout(t *testing.T) {
	for _, hostConfig := range []bool{false, true} {
		name := "bundle"
		if hostConfig {
			name = "host-bind"
		}
		t.Run(name, func(t *testing.T) {
			svc, drv, _ := testService(t)
			config := &driver.ConfigInject{ConfigRoot: "/root/.old-tool", BundleURL: "http://expired.invalid/config", Headers: "expired"}
			if hostConfig {
				config = &driver.ConfigInject{ConfigRoot: "/root/.shared", HostPath: "/host/shared-config"}
			}
			sb, err := svc.Create(t.Context(), CreateRequest{
				Image: "retained-image:version", WorkspaceDir: "/data/original-workspace", Ports: []int{5173},
				Mounts: []string{"/host/shared:/shared:ro"}, Config: config,
				CPUCores: 4, MemoryMB: 8192, DiskGi: 50, Labels: map[string]string{"owner": "restore-test"},
				Env: map[string]string{"SSH_KEY": "retained-key", "WORKSPACE_DIR": "/data/original-workspace",
					"AGENT_PROVIDER": "old-tool", "OLD_TOOL_TOKEN": "old-secret", "REMOVED_CUSTOM": "old-value",
					"GRASP_RUNTIME_URL": "http://expired.invalid/runtime"},
			})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			original := *drv.LastCreate()
			if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
				t.Fatal(err)
			}
			svc.cfg.Image, svc.cfg.WorkspaceDir = "new-default", "/different/default"
			opts := driver.StartOptions{
				RuntimeURL: "http://fallback.invalid/runtime", RuntimeHeaders: "fallback",
				ReplaceEnvKeys: []string{"AGENT_PROVIDER", "OLD_TOOL_TOKEN", "REMOVED_CUSTOM", "GRASP_RUNTIME_URL", "SANDBOX_INJECT", "SANDBOX_INJECT_HEADERS"},
				RestoreEnv: map[string]string{"AGENT_PROVIDER": "current-tool", "NEW_TOOL_TOKEN": "new-secret", "SKIP_INNER_DOCKER": "1",
					"GRASP_RUNTIME_URL": "http://fresh.invalid/runtime", "SANDBOX_INJECT_HEADERS": "Authorization: Bearer fresh",
					"SANDBOX_INJECT": "http://fresh.invalid/config|/root/.current-tool;http://fresh.invalid/git|/root/.git",
					"WORKSPACE_DIR":  "/data/original-workspace"},
			}
			if err := svc.StartPrepared(t.Context(), sb.ID, opts); err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			got := drv.LastCreate()
			if got.Image != original.Image || got.ID != original.ID || got.WorkspaceDir != original.WorkspaceDir || got.Resources != original.Resources ||
				!reflect.DeepEqual(got.Mounts, original.Mounts) || !reflect.DeepEqual(got.Ports, original.Ports) || !reflect.DeepEqual(got.Labels, original.Labels) {
				t.Fatalf("tool refresh changed the saved workload: before=%+v after=%+v", original, got)
			}
			if hostConfig && !reflect.DeepEqual(got.Config, original.Config) || !hostConfig && got.Config != nil {
				t.Fatalf("stale bundle or changed host binding: %+v", got.Config)
			}
			for key, value := range opts.RestoreEnv {
				if got.Env[key] != value {
					t.Errorf("fresh input %s was lost", key)
				}
			}
			for _, key := range []string{"OLD_TOOL_TOKEN", "REMOVED_CUSTOM"} {
				if _, exists := got.Env[key]; exists {
					t.Errorf("removed input %s survived tool switch", key)
				}
			}
			if got.Env["SSH_KEY"] != original.Env["SSH_KEY"] {
				t.Fatal("refresh lost the existing SSH grant")
			}
			row, err := svc.Get(sb.ID)
			if err != nil || !reflect.DeepEqual(row.Env(), got.Env) || svc.WorkspaceDir(row) != original.WorkspaceDir {
				t.Fatalf("restore did not persist its effective inputs: %+v %v", row, err)
			}
		})
	}
}

func TestColdResumeInputPatchIsAtomicWithAcceptedCreation(t *testing.T) {
	svc, drv, db := provisioningService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{Config: &driver.ConfigInject{BundleURL: "http://old.invalid/config"},
		Env: map[string]string{"OLD_TOOL_TOKEN": "old", "SSH_KEY": "stable"}})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
		t.Fatal(err)
	}
	before, err := svc.Get(sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("cannot persist restore intent")
	if err := db.Callback().Update().Before("gorm:update").Register("test:fail-input-patch", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*models.Sandbox); ok && row.Status == models.StatusCreating {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	opts := driver.StartOptions{ReplaceEnvKeys: []string{"OLD_TOOL_TOKEN"}, RestoreEnv: map[string]string{"NEW_TOOL_TOKEN": "fresh", "SANDBOX_INJECT": "http://fresh.invalid/config"}}
	if err := svc.StartPrepared(t.Context(), sb.ID, opts); !errors.Is(err, failure) {
		t.Fatalf("patch failure was not returned: %v", err)
	}
	assertProvisioningFinished(t, svc, sb.ID)
	after, err := svc.Get(sb.ID)
	if err != nil || after.Status != models.StatusStopped || !after.WorkloadReleased || after.SpecJSON != before.SpecJSON || !reflect.DeepEqual(after.Env(), before.Env()) {
		t.Fatalf("rejected refresh changed recovery inputs: %+v %v", after, err)
	}
	if status, _ := drv.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
		t.Fatal("created a workload before its restore inputs were committed")
	}
	if err := db.Callback().Update().Remove("test:fail-input-patch"); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartPrepared(t.Context(), sb.ID, opts); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	got := drv.LastCreate()
	if got.Env["NEW_TOOL_TOKEN"] != "fresh" || got.Env["OLD_TOOL_TOKEN"] != "" || got.Config != nil {
		t.Fatal("retry did not use the accepted fresh inputs")
	}
}

func TestColdResumeRejectsWorkspaceMoveAndWarmResumeIgnoresEnvironmentPatch(t *testing.T) {
	for _, cold := range []bool{false, true} {
		t.Run(map[bool]string{false: "warm", true: "cold"}[cold], func(t *testing.T) {
			svc, drv, _ := testService(t)
			sb, err := svc.Create(t.Context(), CreateRequest{WorkspaceDir: "/original", Env: map[string]string{"TOKEN": "original"}})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			if cold {
				err = svc.Stop(t.Context(), sb.ID, 1)
			} else {
				err = drv.Stop(t.Context(), sb.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := svc.Get(sb.ID)
			if err != nil {
				t.Fatal(err)
			}
			err = svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{ReplaceEnvKeys: []string{"TOKEN"},
				RestoreEnv: map[string]string{"TOKEN": "different", "WORKSPACE_DIR": "/wrong-workspace"}})
			if cold && err == nil || !cold && err != nil {
				t.Fatalf("unexpected start result: %v", err)
			}
			if !cold {
				waitSvcRunning(t, svc, sb.ID)
			}
			after, err := svc.Get(sb.ID)
			if err != nil || after.SpecJSON != before.SpecJSON || !reflect.DeepEqual(after.Env(), before.Env()) {
				t.Fatalf("patch unexpectedly changed recovery data: %+v %v", after, err)
			}
			if cold && (after.Status != models.StatusStopped || !after.WorkloadReleased) {
				t.Fatal("rejected workspace move lost the restore point")
			}
		})
	}
}

func TestWorkspaceDirUsesSavedWorkloadBeforeChangedDefaults(t *testing.T) {
	svc, _, _ := testService(t)
	svc.cfg.WorkspaceDir = "/changed/default"
	for _, tc := range []struct{ name, spec, env, want string }{
		{"saved", `{"WorkspaceDir":"/original"}`, "/different-env", "/original"},
		{"old-record", "", "/legacy", "/legacy"},
		{"default", "", "", "/changed/default"},
		{"old-spec", `{}`, "/legacy", "/legacy"},
		{"invalid-spec", `{`, "/legacy", "/legacy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sb := &models.Sandbox{SpecJSON: tc.spec}
			sb.SetEnv(map[string]string{"WORKSPACE_DIR": tc.env})
			if got := svc.WorkspaceDir(sb); got != tc.want {
				t.Fatalf("workspace=%q want %q", got, tc.want)
			}
		})
	}
}
