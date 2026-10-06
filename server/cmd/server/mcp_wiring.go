package main

import (
	"strings"

	"github.com/cocofhu/grasp/internal/channels"
	"github.com/cocofhu/grasp/internal/contextmcp"
	"github.com/cocofhu/grasp/internal/memorymcp"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/schedulermcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/rs/zerolog/log"
)

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

// agentBoundToProject is true when the Agent exists and its home project equals
// projectID. Agents of other projects must not receive memory/context/
// scheduler tokens for this project.
func (w *platformMCPWire) agentBoundToProject(projectID, agent string) bool {
	projectID = strings.TrimSpace(projectID)
	agent = strings.TrimSpace(agent)
	if projectID == "" || agent == "" || w.skills == nil {
		return false
	}
	ag, ok := w.skills.Get(agent)
	if !ok {
		return false
	}
	return services.AgentProjectMatches(ag, projectID)
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
// the agent is the project's bound PM Leader.
func (w *platformMCPWire) registerCron(projectID, threadID, agent string) (string, []sandbox.MCPServerSpec) {
	if !w.agentBoundToProject(projectID, agent) {
		log.Warn().Str("project", projectID).Str("agent", agent).
			Msg("cron platform MCP skipped: agent home project mismatch")
		return "", nil
	}
	tok := platformmcp.NewToken()
	if w.memory != nil {
		w.memory.Restore(tok, projectID, agent, threadID, "cron", true)
	}
	if w.context != nil {
		w.context.Restore(tok, projectID, agent, threadID, "cron")
	}
	if w.scheduler != nil {
		w.scheduler.Restore(tok, projectID, agent, threadID, "cron", false)
	}
	specs := services.BuildAgentPlatformMCPSpecs(projectID, agent, tok)
	if w.pmSvc != nil {
		if binding, err := w.pmSvc.GetBinding(projectID); err == nil &&
			binding.Enabled && binding.AgentConfigRef == agent {
			if w.pm != nil {
				w.pm.Restore(projectID, threadID, "cron", agent, tok)
			}
			specs = append(specs, services.BuildPmRoleMCPSpecs(projectID, tok, binding.EnabledMcps)...)
		}
	}
	return tok, specs
}

// registerChannel mints a shared token for an IM channel turn. Memory/scheduler
// write gates follow caps from ChannelConfig (default off for unauthenticated
// channel senders). Platform PM role MCP specs + session AllowedMcps come from
// the channel's EnabledMcps (not Project.PmEnabledMcps).
func (w *platformMCPWire) registerChannel(projectID, threadID, userID, agent string, enabledMcps []string, caps channels.SessionCaps) (string, []sandbox.MCPServerSpec) {
	if w.pm == nil {
		return "", nil
	}
	if !w.agentBoundToProject(projectID, agent) {
		log.Warn().Str("project", projectID).Str("agent", agent).
			Msg("channel platform MCP skipped: agent home project mismatch")
		return "", nil
	}
	tok := platformmcp.NewToken()
	w.pm.RestoreWithMcps(projectID, threadID, userID, agent, tok, enabledMcps)
	if w.memory != nil {
		w.memory.Restore(tok, projectID, agent, threadID, userID, caps.AllowMemoryWrite)
	}
	if w.context != nil {
		w.context.Restore(tok, projectID, agent, threadID, userID)
	}
	if w.scheduler != nil {
		w.scheduler.Restore(tok, projectID, agent, threadID, userID, caps.AllowSchedulerWrite)
	}
	// Inject the full default PM role set so expanding Channel.EnabledMcps on a
	// reused sandbox takes effect via session AllowedMcps without recreating
	// the container; ServeRPC still gates by the channel list.
	specs := append(
		services.BuildAgentPlatformMCPSpecs(projectID, agent, tok),
		services.BuildPmRoleMCPSpecs(projectID, tok, nil)...,
	)
	return tok, specs
}

func (w *platformMCPWire) restoreChannel(projectID, threadID, userID, agent, token string, enabledMcps []string, caps channels.SessionCaps) {
	if !w.agentBoundToProject(projectID, agent) {
		log.Warn().Str("project", projectID).Str("agent", agent).
			Msg("channel platform MCP restore skipped: agent home project mismatch")
		return
	}
	if w.pm != nil {
		// Re-bind AllowedMcps from the latest Channel.EnabledMcps so permission
		// changes apply on the next reused-sandbox turn.
		w.pm.RestoreWithMcps(projectID, threadID, userID, agent, token, enabledMcps)
	}
	if w.memory != nil {
		w.memory.Restore(token, projectID, agent, threadID, userID, caps.AllowMemoryWrite)
	}
	if w.context != nil {
		w.context.Restore(token, projectID, agent, threadID, userID)
	}
	if w.scheduler != nil {
		w.scheduler.Restore(token, projectID, agent, threadID, userID, caps.AllowSchedulerWrite)
	}
}
