package config

import (
	"math"
	"testing"
)

func TestWorkspaceOperationsDefaultsKeepRuntimeAndStorageWithinOneBudget(t *testing.T) {
	limits, err := (WorkspaceOperationsConfig{}).Resolved()
	if err != nil || !*limits.Enabled || limits.MaxFileBytes != 512<<20 || limits.MaxUncompressedBytes != 2<<30 || limits.MaxGitBytes != 512<<20 || limits.PrivateRoot != "/tmp/grasp-private-workspace" {
		t.Fatal(limits, err)
	}
	for _, bad := range []WorkspaceOperationsConfig{{PrivateRoot: "/"}, {PrivateRoot: "relative"}, {MaxFileBytes: math.MaxInt64}, {MaxUncompressedBytes: 1}, {MaxGitBytes: 2 << 30}, {MaxScopeReservedBytes: 1}, {MaxDuration: "0s"}, {MaxWorkers: -1}} {
		if _, err := bad.Resolved(); err == nil {
			t.Fatal("accepted unbounded or inconsistent operation settings", bad)
		}
	}
}

func TestWorkspaceOperationsExplicitEnvironmentCannotSilentlyReenableOnParseFailure(t *testing.T) {
	t.Setenv("GRASP_WORKSPACE_OPERATIONS_ENABLED", "false")
	t.Setenv("GRASP_WORKSPACE_MAX_DURATION", "10m")
	cfg := &Config{}
	applyWorkspaceOperationsEnv(cfg)
	resolved, err := cfg.WorkspaceOperations.Resolved()
	if err != nil || *resolved.Enabled || resolved.MaxDuration != "10m" {
		t.Fatal(err)
	}
	t.Setenv("GRASP_WORKSPACE_OPERATIONS_ENABLED", "invalid")
	applyWorkspaceOperationsEnv(cfg)
	if _, err := cfg.WorkspaceOperations.Resolved(); err == nil {
		t.Fatal("invalid enable setting silently enabled execution")
	}
}
