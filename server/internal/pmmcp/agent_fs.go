package pmmcp

import (
	"errors"
	"strings"

	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"
)

func (h *Host) callAgentFS(projectID, token, name string, args map[string]any) (any, bool) {
	sess, ok := h.SessionFor(projectID, token)
	if !ok {
		return map[string]any{"error": "unauthorized"}, true
	}
	h.mu.RLock()
	skill := h.skill
	h.mu.RUnlock()
	if skill == nil {
		return map[string]any{"error": "agent service unavailable"}, true
	}

	switch name {
	case "pm_list_project_agents":
		return services.ListProjectAgents(skill.List(), sess.ProjectID, sess.AgentName), false

	case "pm_list_agent_templates", "pm_create_agent_from_template":
		return h.callTeamTools(sess, skill, name, args)

	case "pm_fs_list", "pm_fs_read", "pm_fs_write", "pm_fs_delete", "pm_fs_mkdir", "pm_fs_rename",
		"pm_fs_history", "pm_fs_diff", "pm_fs_restore":
		agentName := strings.TrimSpace(platformmcp.StrArg(args, "agentName"))
		if agentName == "" {
			return map[string]any{"error": "agentName is required"}, true
		}
		if errMsg, deny := h.authorizeAgentFSTarget(sess, skill, agentName); deny {
			return map[string]any{"error": errMsg}, true
		}
		path := platformmcp.StrArg(args, "path")
		switch name {
		case "pm_fs_list":
			entries, err := skill.ListWorkspace(agentName, path)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{"agentName": agentName, "path": path, "entries": entries}, false
		case "pm_fs_read":
			content, err := skill.ReadWorkspaceFile(agentName, path)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{"agentName": agentName, "path": path, "content": content}, false
		case "pm_fs_history":
			revs, err := skill.Vcs.ListRevisions(agentName, 100)
			if err != nil {
				return map[string]any{"error": err.Error()}, true
			}
			return map[string]any{"agentName": agentName, "revisions": revs}, false
		case "pm_fs_diff":
			sha := strings.TrimSpace(platformmcp.StrArg(args, "sha"))
			if sha == "" {
				return map[string]any{"error": "sha is required"}, true
			}
			diff, err := skill.Vcs.DiffRevision(agentName, sha)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{"agentName": agentName, "sha": sha, "diff": diff}, false
		case "pm_fs_restore":
			sha := strings.TrimSpace(platformmcp.StrArg(args, "sha"))
			if sha == "" {
				return map[string]any{"error": "sha is required"}, true
			}
			reason := strings.TrimSpace(platformmcp.StrArg(args, "reason"))
			opts := h.workspaceWriteOpts(sess)
			newSha, err := skill.RestoreWorkspaceVcs(agentName, sha, opts.Author, reason)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{"ok": true, "agentName": agentName, "sha": newSha, "restoredTo": sha}, false
		case "pm_fs_write":
			content, _ := args["content"].(string)
			if _, has := args["content"]; !has {
				return map[string]any{"error": "content is required"}, true
			}
			opts := h.workspaceWriteOpts(sess)
			opts.Reason = strings.TrimSpace(platformmcp.StrArg(args, "reason"))
			commitSha, err := skill.WriteWorkspaceFileVcs(agentName, path, content, opts)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{
				"ok":        true,
				"agentName": agentName,
				"path":      path,
				"bytes":     len(content),
				"sha":       commitSha,
				"note":      "persisted to host workspace; refresh or reopen Agent Studio to see changes; unsaved Studio drafts may overwrite on Save",
			}, false
		case "pm_fs_delete":
			opts := h.workspaceWriteOpts(sess)
			opts.Reason = strings.TrimSpace(platformmcp.StrArg(args, "reason"))
			commitSha, err := skill.DeleteWorkspacePathVcs(agentName, path, opts)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{"ok": true, "agentName": agentName, "path": path, "sha": commitSha}, false
		case "pm_fs_mkdir":
			opts := h.workspaceWriteOpts(sess)
			opts.Reason = strings.TrimSpace(platformmcp.StrArg(args, "reason"))
			commitSha, err := skill.MkdirWorkspaceVcs(agentName, path, opts)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{"ok": true, "agentName": agentName, "path": path, "sha": commitSha}, false
		case "pm_fs_rename":
			toPath := platformmcp.StrArg(args, "toPath")
			if strings.TrimSpace(toPath) == "" {
				return map[string]any{"error": "toPath is required"}, true
			}
			opts := h.workspaceWriteOpts(sess)
			opts.Reason = strings.TrimSpace(platformmcp.StrArg(args, "reason"))
			commitSha, err := skill.RenameWorkspaceVcs(agentName, path, toPath, opts)
			if err != nil {
				return map[string]any{"error": workspaceFSError(err)}, true
			}
			return map[string]any{"ok": true, "agentName": agentName, "path": path, "toPath": toPath, "sha": commitSha}, false
		}
	}
	return map[string]any{"error": "unknown tool: " + name}, true
}

func (h *Host) workspaceWriteOpts(sess *Session) services.WorkspaceWriteOpts {
	author := strings.TrimSpace(sess.AgentName)
	source := services.VcsSourcePmMCP
	if sess.External {
		source = services.VcsSourceExternalMCP
		if name := strings.TrimSpace(sess.ApiKeyName); name != "" {
			author = name
		} else {
			author = "external-mcp"
		}
	}
	if author == "" {
		author = "pm-mcp"
	}
	return services.WorkspaceWriteOpts{Author: author, Source: source}
}

// authorizeAgentFSTarget allows PM to manage any agent bound to the same project (incl. self).
func (h *Host) authorizeAgentFSTarget(sess *Session, skill *services.AgentService, target string) (errMsg string, deny bool) {
	if strings.TrimSpace(sess.AgentName) == "" {
		return "pm leader not bound", true
	}
	ag, ok := skill.Get(target)
	if !ok {
		return "agent not found: " + target, true
	}
	if !services.AgentProjectMatches(ag, sess.ProjectID) {
		return "agent not in project: " + target, true
	}
	return "", false
}

func workspaceFSError(err error) string {
	switch {
	case errors.Is(err, services.ErrVcsReasonRequired):
		return "reason is required and must be non-empty"
	case errors.Is(err, services.ErrVcsRevisionMiss):
		return "revision not found"
	case errors.Is(err, services.ErrWorkspacePathInvalid):
		return "invalid workspace path (escape/symlink rejected): " + err.Error()
	case errors.Is(err, services.ErrWorkspaceFileTooLarge):
		return "file exceeds 1MiB limit"
	case errors.Is(err, services.ErrWorkspaceNotFound):
		return "path not found"
	case errors.Is(err, services.ErrWorkspaceAgentMissing):
		return "agent not found"
	case errors.Is(err, services.ErrWorkspaceIsDir):
		return "path is a directory"
	case errors.Is(err, services.ErrWorkspaceNotDir):
		return "path is not a directory"
	case errors.Is(err, services.ErrWorkspaceExists):
		return "destination already exists"
	default:
		if strings.Contains(err.Error(), "版本记录失败") {
			return err.Error()
		}
		return err.Error()
	}
}
