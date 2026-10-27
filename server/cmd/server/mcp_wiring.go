package main

import (
	"errors"
	"strings"

	"github.com/cocofhu/grasp/internal/channels"
	"github.com/cocofhu/grasp/internal/contextmcp"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/memorymcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/schedulermcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// wireArtifactMCPPersistence restores authorization and active-node capabilities
// when a sandbox calls another replica or the server restarts. Solo namespaces
// have no Run row: validate their current resident sandbox and session instead.
func wireArtifactMCPPersistence(host *mcp.Host, db *gorm.DB, runs *services.RunService, preview *services.PreviewService) {
	host.SetRunTokenSource(func(runID string) (string, bool, bool) {
		var run models.Run
		if err := db.Select("mcp_token").First(&run, "id = ?", runID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if sb, _, ok := preview.SoloMCPBinding(runID); ok {
					return sb.Token, true, true
				}
			}
			return "", false, false
		}
		var live int64
		db.Model(&models.Sandbox{}).Where("run_id = ? AND purpose = ?", runID, "run").Count(&live)
		return run.McpToken, live > 0, true
	})
	host.SetActiveNodeSource(func(runID string) (string, *models.AgentCapabilities, bool) {
		var run models.Run
		if err := db.Select("id", "status", "graph").First(&run, "id = ?", runID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if _, caps, ok := preview.SoloMCPBinding(runID); ok {
					return "solo", caps, true
				}
			}
			return "", nil, false
		}
		// Prefer the active visit; retained workflow sandboxes keep the same
		// token window after completion, so fall back to their owning node.
		nodeID := runs.CurrentNodeIDs([]models.Run{run})[runID]
		if nodeID == "" {
			var sb models.Sandbox
			if db.Where("run_id = ? AND purpose = ?", runID, "run").
				Order("updated_at desc").First(&sb).Error != nil || strings.TrimSpace(sb.NodeID) == "" {
				return "", nil, false
			}
			nodeID = sb.NodeID
		}
		var caps *models.AgentCapabilities
		if n := services.EffectiveNode(db, run, nodeID); n != nil {
			caps = n.Caps
		}
		return nodeID, caps, true
	})
}

// platformMCPWire owns the shared register/restore/unregister paths for the
// four platform MCP hosts so cron, channel, and sandbox-destroy hooks stay in
// sync without duplicating the same four Unregister calls in main.
type platformMCPWire struct {
	pm        *pmmcp.Host
	memory    *memorymcp.Host
	context   *contextmcp.Host
	scheduler *schedulermcp.Host
	pmSvc     *services.PmService
	skills    *services.AgentService
}

// agentExists gates platform MCP tokens on a live Agent.
func (w *platformMCPWire) agentExists(agent string) bool {
	agent = strings.TrimSpace(agent)
	if agent == "" || w.skills == nil {
		return false
	}
	return w.skills.Exists(agent)
}

func (w *platformMCPWire) unregister(token string) {
	if token == "" {
		return
	}
	if w.pm != nil {
		w.pm.Unregister(token)
	}
	if w.memory != nil {
		w.memory.Unregister(token)
	}
	if w.context != nil {
		w.context.Unregister(token)
	}
	if w.scheduler != nil {
		w.scheduler.Unregister(token)
	}
}

func (w *platformMCPWire) clearSandboxRef(threadID string) {
	if w.pmSvc == nil || threadID == "" {
		return
	}
	if err := w.pmSvc.ClearSandboxRef(threadID); err != nil {
		log.Warn().Err(err).Str("thread", threadID).Msg("clear sandbox ref failed")
	}
}

// registerCron mints a shared token for a cron execution. Memory writes are
// allowed; scheduler write tools stay off. PM role MCPs are attached only when
// the agent is the bound PM Leader.
func (w *platformMCPWire) registerCron(threadID, agent string) (string, []sandbox.MCPServerSpec) {
	if !w.agentExists(agent) {
		log.Warn().Str("agent", agent).
			Msg("cron platform MCP skipped: agent not found")
		return "", nil
	}
	tok := platformmcp.NewToken()
	if w.memory != nil {
		w.memory.Restore(tok, agent, threadID, "cron", true)
	}
	if w.context != nil {
		w.context.Restore(tok, agent, threadID, "cron")
	}
	if w.scheduler != nil {
		w.scheduler.Restore(tok, agent, threadID, "cron", false)
	}
	specs := services.BuildAgentPlatformMCPSpecs(agent, tok)
	if w.pmSvc != nil {
		if binding, err := w.pmSvc.GetBinding(); err == nil &&
			binding.Enabled && binding.AgentConfigRef == agent {
			if w.pm != nil {
				w.pm.Restore(threadID, "cron", agent, tok)
			}
			specs = append(specs, services.BuildPmRoleMCPSpecs(tok, binding.EnabledMcps)...)
		}
	}
	return tok, specs
}

// registerChannel mints a shared token for an IM channel turn. Memory/scheduler
// write gates follow caps from ChannelConfig (default off for unauthenticated
// channel senders). Platform PM role MCP specs + session AllowedMcps come from
// the channel's EnabledMcps (not Workspace.PmEnabledMcps).
func (w *platformMCPWire) registerChannel(threadID, userID, agent string, enabledMcps []string, caps channels.SessionCaps) (string, []sandbox.MCPServerSpec) {
	if w.pm == nil {
		return "", nil
	}
	if !w.agentExists(agent) {
		log.Warn().Str("agent", agent).
			Msg("channel platform MCP skipped: agent not found")
		return "", nil
	}
	tok := platformmcp.NewToken()
	w.pm.RestoreWithMcps(threadID, userID, agent, tok, enabledMcps)
	if w.memory != nil {
		w.memory.Restore(tok, agent, threadID, userID, caps.AllowMemoryWrite)
	}
	if w.context != nil {
		w.context.Restore(tok, agent, threadID, userID)
	}
	if w.scheduler != nil {
		w.scheduler.Restore(tok, agent, threadID, userID, caps.AllowSchedulerWrite)
	}
	// Inject the full default PM role set so expanding Channel.EnabledMcps on a
	// reused sandbox takes effect via session AllowedMcps without recreating
	// the container; ServeRPC still gates by the channel list.
	specs := append(
		services.BuildAgentPlatformMCPSpecs(agent, tok),
		services.BuildPmRoleMCPSpecs(tok, nil)...,
	)
	return tok, specs
}

func (w *platformMCPWire) restoreChannel(threadID, userID, agent, token string, enabledMcps []string, caps channels.SessionCaps) {
	if !w.agentExists(agent) {
		log.Warn().Str("agent", agent).
			Msg("channel platform MCP restore skipped: agent not found")
		return
	}
	if w.pm != nil {
		// Re-bind AllowedMcps from the latest Channel.EnabledMcps so permission
		// changes apply on the next reused-sandbox turn.
		w.pm.RestoreWithMcps(threadID, userID, agent, token, enabledMcps)
	}
	if w.memory != nil {
		w.memory.Restore(token, agent, threadID, userID, caps.AllowMemoryWrite)
	}
	if w.context != nil {
		w.context.Restore(token, agent, threadID, userID)
	}
	if w.scheduler != nil {
		w.scheduler.Restore(token, agent, threadID, userID, caps.AllowSchedulerWrite)
	}
}

// registerPMPolicy authorizes an ordinary per-workflow PM policy explicitly.
func (w *platformMCPWire) registerPMPolicy(threadID, agent string, enabledMcps []string) (string, []sandbox.MCPServerSpec) {
	if !w.agentExists(agent) {
		return "", nil
	}
	token := platformmcp.NewToken()
	if w.memory != nil {
		w.memory.Restore(token, agent, threadID, "system", true)
	}
	if w.context != nil {
		w.context.Restore(token, agent, threadID, "system")
	}
	if w.scheduler != nil {
		w.scheduler.Restore(token, agent, threadID, "system", false)
	}
	if w.pm != nil {
		w.pm.RestoreWithMcps(threadID, "system", agent, token, services.EffectivePmEnabledMcps(enabledMcps))
	}
	specs := services.BuildAgentPlatformMCPSpecs(agent, token)
	return token, append(specs, services.BuildPmRoleMCPSpecs(token, enabledMcps)...)
}
