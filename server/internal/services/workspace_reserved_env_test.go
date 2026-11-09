package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestWorkflowLaunchCannotSupplyWorkspaceExecutionIdentity(t *testing.T) {
	for _, key := range []string{"GRASP_SANDBOX_RESOURCE_ID", "GRASP_EXECUTION_LINEAGE", "GRASP_WORKSPACE_CONTROL_ROOT", "GRASP_WORKSPACE_RESTORE_ROOT"} {
		t.Run(key, func(t *testing.T) {
			if _, err := ValidateRunSandboxEnv([]models.EnvEntry{{Key: key, Value: "caller-controlled"}}); err == nil {
				t.Fatal("workflow launch could select its execution identity or private native storage")
			}
		})
	}
	if _, err := ValidateRunSandboxEnv([]models.EnvEntry{{Key: "PROJECT_FEATURE", Value: "enabled"}}); err != nil {
		t.Fatal("ordinary workspace configuration was rejected", err)
	}
}
