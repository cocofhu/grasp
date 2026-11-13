package services

import (
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// SandboxScopes authorizes artifact MCP namespaces owned by a sandbox rather
// than a workflow run: resident Solo sandboxes and Agent test sandboxes. The
// token is the one stored on the sandbox row. A test sandbox grants access
// until it errors or is deleted. A Solo sandbox additionally needs its Agent
// and must still be the one its session points at; starting and stopping
// count as live because the agent initializes MCP before the session is
// running and may still write while a backup runs.
func SandboxScopes(db *gorm.DB, agents AgentGetter) mcp.ScopeSource {
	return func(runID string) (string, bool, bool) {
		var row models.Sandbox
		if db.Select("id", "token", "purpose", "status", "thread_id", "profile").
			Where("run_id = ? AND purpose IN ?", runID, []string{SandboxPurposeSolo, "test"}).
			Order("id desc").First(&row).Error != nil {
			return "", false, false
		}
		if row.Status == "error" {
			return row.Token, false, true
		}
		if row.Purpose == "test" {
			return row.Token, true, true
		}
		if agents != nil {
			if _, ok := agents.Get(row.Profile); !ok {
				return row.Token, false, true
			}
		}
		var sessions int64
		if db.Model(&models.SoloSession{}).
			Where("id = ? AND sandbox_id = ? AND agent_name = ? AND status IN ?", row.ThreadID, row.ID, row.Profile,
				[]string{models.SoloStatusStarting, models.SoloStatusRunning, models.SoloStatusStopping}).
			Count(&sessions).Error != nil {
			return row.Token, false, true
		}
		return row.Token, sessions > 0, true
	}
}
