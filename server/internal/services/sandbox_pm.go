package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// Platform MCP names.
const (
	MemoryStoreMCP     = "memory-store"
	ContextStoreMCP    = "context-store"
	TaskSchedulerMCP   = "task-scheduler"
	PmProgressMCP      = "pm-progress"
	PmWorkflowReadMCP  = "pm-workflow-read"
	PmWorkflowWriteMCP = "pm-workflow-write"
	PmAgentFSMCP       = "pm-agent-fs"
	PmPrdManagerMCP    = "pm-prd-manager"
)

// OpenForPM opens (or reuses) a consult sandbox for the project PM Leader agent.
// Implementation is the generic OpenAgentSandbox with reuse enabled.
func (s *SandboxService) OpenForPM(ctx context.Context, profile, projectID, threadID, sharedToken string, platformSpecs []sandbox.MCPServerSpec) (*models.Sandbox, bool, error) {
	return s.OpenAgentSandbox(ctx, AgentSandboxOpenOpts{
		Profile:       profile,
		ProjectID:     projectID,
		ThreadID:      threadID,
		SharedToken:   sharedToken,
		PlatformSpecs: platformSpecs,
		Reuse:         true,
		RunIDPrefix:   "agent",
	})
}
