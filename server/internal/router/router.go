// Package router wires HTTP routes to handlers.
package router

import (
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/auth/apikey"
	"github.com/cocofhu/grasp/internal/handlers"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// New builds the gin engine with all routes registered.
func New(h *handlers.Handlers) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(errorLogger())
	r.Use(cors())
	// Trust common private-network proxies so ClientIP reads X-Forwarded-For
	// correctly behind K8s Ingress.
	_ = r.SetTrustedProxies([]string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.1",
	})

	// Vite-built SPA assets and self-hosted fonts, baked into the image at
	// ./web/dist (see server/Dockerfile). Served from the filesystem, not
	// embedded. /fonts must be mounted (and excluded from SPA NoRoute below)
	// so woff2 is not swallowed as index.html (OTS / Failed to decode).
	r.Static("/assets", "./web/dist/assets")
	r.Static("/fonts", "./web/dist/fonts")
	r.StaticFile("/favicon.ico", "./web/dist/favicon.ico")
	r.StaticFile("/", "./web/dist/index.html")

	api := r.Group("/api")
	api.Use(drainMiddleware(h))
	if h.Auth != nil {
		api.Use(h.Auth.APIMiddleware())
	}
	{
		if h.Auth != nil {
			api.POST("/auth/login", h.Auth.LoginHandler)
			api.POST("/auth/logout", h.Auth.LogoutHandler)
			api.GET("/auth/me", h.Auth.MeHandler)
		}

		api.GET("/health", h.Health)
		api.GET("/live", h.Live)
		api.GET("/node-registry", h.NodeRegistry)
		api.GET("/stats/dashboard", h.DashboardStats)
		api.GET("/stats/token", h.GetGlobalTokenStats)
		api.GET("/stats/token/events", h.ListTokenUsageEvents)
		api.GET("/stats/token/pricing", h.GetTokenPricing)
		api.PUT("/stats/token/pricing", h.UpdateTokenPricing)
		api.GET("/stats/platform-status", h.PlatformStatus)

		api.GET("/settings", h.GetSettings)
		api.PUT("/settings", h.UpdateSettings)

		api.GET("/notifications", h.ListNotifications)
		api.POST("/notifications/read", h.MarkNotificationRead)
		api.POST("/notifications/read-all", h.MarkAllNotificationsRead)

		api.GET("/opencode/providers", h.ListOpenCodeProviders)
		api.GET("/opencode/providers/:provider/models", h.ListOpenCodeProviderModels)
		api.GET("/acp-backends/:backend/models", h.ListCliBackendModels)

		api.GET("/workspace/maintenance", h.WorkspaceMaintenance)
		api.POST("/workspace/maintenance", h.WorkspaceMaintenance)
		api.GET("/workspace", h.GetWorkspace)
		api.GET("/workspace/run-tags", h.ListRunTags)
		api.GET("/workspace/audit", h.ListAudit)
		api.GET("/workspace/audit/facets", h.ListAuditFacets)
		api.GET("/workspace/audit/export", h.ExportAudit)
		api.PUT("/workspace", h.UpdateWorkspace)
		api.PATCH("/workspace", h.UpdateWorkspace)
		api.POST("/workspace/bootstrap-onboarding", h.BootstrapOnboarding)
		api.GET("/workspace/external-mcp", h.GetExternalMcp)
		api.PUT("/workspace/external-mcp", h.UpdateExternalMcp)
		api.GET("/workspace/external-mcp/keys", h.ListMcpKeys)
		api.POST("/workspace/external-mcp/keys", h.CreateMcpKey)
		api.DELETE("/workspace/external-mcp/keys/:keyId", h.RevokeMcpKey)
		api.GET("/workspace/credentials", h.ListCredentials)
		api.POST("/workspace/credentials", h.CreateCredential)
		api.PUT("/workspace/credentials/:credentialId", h.UpdateCredential)
		api.PATCH("/workspace/credentials/:credentialId", h.UpdateCredential)
		api.DELETE("/workspace/credentials/:credentialId", h.ClearCredential)
		api.DELETE("/workspace/credentials/:credentialId/account", h.RemoveModelAPICredential)
		api.POST("/workspace/credentials/:credentialId/revoke", h.RevokeCredential)
		modelConnections := &handlers.ModelConnectionHandlers{Service: h.ModelConnections}
		api.GET("/workspace/model-connections", modelConnections.List)
		api.POST("/workspace/model-connections", modelConnections.Create)
		api.GET("/workspace/model-connections/:connectionId", modelConnections.Get)
		api.GET("/workspace/model-connections/:connectionId/models", modelConnections.Models)
		api.PUT("/workspace/model-connections/:connectionId", modelConnections.Update)
		api.PATCH("/workspace/model-connections/:connectionId", modelConnections.Update)
		api.DELETE("/workspace/model-connections/:connectionId", modelConnections.Delete)
		modelConfigurations := &handlers.ModelConfigurationHandlers{Service: h.ModelConfigurations}
		api.GET("/workspace/model-configurations", modelConfigurations.List)
		api.POST("/workspace/model-configurations", modelConfigurations.Create)
		api.GET("/workspace/model-configurations/:configurationId", modelConfigurations.Get)
		api.PUT("/workspace/model-configurations/:configurationId", modelConfigurations.Update)
		api.DELETE("/workspace/model-configurations/:configurationId", modelConfigurations.Delete)
		api.GET("/workspace/tool-capabilities", modelConnections.Capabilities)
		api.GET("/workspace/agents/export", h.ExportWorkspaceAgents)
		api.POST("/workspace/agents/import", h.ImportWorkspaceAgents)
		api.GET("/workspace/shared-agent-config", h.GetSharedAgent)
		api.PUT("/workspace/shared-agent-config", h.PutSharedAgent)
		api.POST("/workspace/shared-agent-config/test", h.CreateSharedAgentTest)

		api.GET("/workspace/requirement-drafts", h.ListRequirementDrafts)
		api.POST("/workspace/requirement-drafts", h.CreateRequirementDraft)
		api.GET("/workspace/requirement-drafts/:draftId", h.GetRequirementDraft)
		api.PUT("/workspace/requirement-drafts/:draftId", h.UpdateRequirementDraft)
		api.PATCH("/workspace/requirement-drafts/:draftId", h.UpdateRequirementDraft)
		api.PATCH("/workspace/requirement-drafts/:draftId/status", h.PatchRequirementDraftStatus)
		api.PATCH("/workspace/requirement-drafts/:draftId/schedule", h.PatchRequirementDraftSchedule)
		api.DELETE("/workspace/requirement-drafts/:draftId", h.DeleteRequirementDraft)

		api.GET("/agent-teams/templates", h.ListAgentTeamTemplates)
		api.POST("/agent-teams/bootstrap", h.BootstrapAgentTeam)
		api.GET("/agent-teams/bootstrap/:id", h.GetAgentTeamBootstrap)
		api.POST("/agent-teams/bootstrap/:id/retry", h.RetryAgentTeamBootstrap)

		api.GET("/workspace/pm-leader", h.GetPmLeader)
		api.PUT("/workspace/pm-leader", h.UpdatePmLeader)
		api.GET("/workspace/cron-jobs", h.ListCronJobs)
		api.GET("/workspace/notify-receipts", h.ListNotifyReceipts)
		api.PATCH("/workspace/cron-jobs/:jobId", h.PatchCronJob)
		api.DELETE("/workspace/cron-jobs/:jobId", h.DeleteCronJob)
		// Plural multi-channel APIs (primary + secondary).
		api.GET("/workspace/channels", h.ListChannels)
		api.POST("/workspace/channels", h.CreateChannel)
		api.PUT("/workspace/channels/:channelId", h.UpdateChannel)
		api.DELETE("/workspace/channels/:channelId", h.DeleteChannel)
		api.GET("/workspace/pm/memories", h.ListPmMemories)
		api.GET("/workspace/pm/threads", h.ListPmThreads)
		api.POST("/workspace/pm/threads", h.CreatePmThread)
		api.GET("/workspace/pm/threads/:tid", h.GetPmThread)
		api.DELETE("/workspace/pm/threads/:tid", h.DeletePmThread)
		api.GET("/workspace/pm/threads/:tid/messages", h.ListPmMessages)
		api.POST("/workspace/pm/threads/:tid/messages", h.AppendPmMessage)
		api.PATCH("/workspace/pm/threads/:tid/messages/:mid", h.PatchPmMessage)
		api.POST("/workspace/pm/threads/:tid/sandbox", h.EnsurePmSandbox)
		api.POST("/workspace/pm/threads/:tid/turns", h.StartPmTurn)
		api.POST("/workspace/pm/threads/:tid/turns/cancel", h.CancelPmTurn)
		api.GET("/workspace/pm/threads/:tid/chat", h.PmThreadChat)

		api.GET("/projects", h.ListProjects)
		api.POST("/projects", h.CreateProject)
		api.GET("/projects/:id", h.GetProject)
		api.PUT("/projects/:id", h.UpdateProject)
		api.DELETE("/projects/:id", h.DeleteProject)
		api.GET("/solo/sessions", h.ListSoloSessions)
		api.POST("/solo/sessions", h.CreateSoloSession)
		api.GET("/solo/sessions/:id", h.GetSoloSession)
		api.PATCH("/solo/sessions/:id", h.PatchSoloSession)
		api.DELETE("/solo/sessions/:id", h.DeleteSoloSession)
		api.POST("/solo/sessions/:id/close", h.CloseSoloSession)
		api.POST("/solo/sessions/:id/restart", h.RestartSoloSession)
		api.GET("/solo/sessions/:id/messages", h.ListSoloMessages)
		api.GET("/solo/sessions/:id/env", h.GetSoloEnv)
		api.POST("/solo/sessions/:id/model", h.SetSoloSessionModel)
		api.POST("/solo/sessions/:id/turns", h.StartSoloTurn)
		api.POST("/solo/sessions/:id/turns/cancel", h.CancelSoloTurn)
		api.PUT("/solo/sessions/:id/queue", h.ReorderSoloQueue)
		api.DELETE("/solo/sessions/:id/queue/:turnId", h.RemoveSoloQueued)
		api.GET("/solo/sessions/:id/chat", h.SoloChat)

		api.GET("/workflows/catalog", h.ListWorkflowCatalog)
		api.GET("/workflows", h.ListWorkflows)
		api.POST("/workflows", h.SaveWorkflow)
		api.POST("/workflows/import", h.ImportWorkflow)
		api.POST("/workflows/from-baseline", h.CreateWorkflowFromBaseline)
		api.GET("/workflows/:id", h.GetWorkflow)
		api.PUT("/workflows/:id", h.SaveWorkflow)
		api.PATCH("/workflows/:id/notify-policy", h.PatchWorkflowNotifyPolicy)
		api.PATCH("/workflows/:id/home-visibility", h.PatchWorkflowHomeVisibility)
		api.DELETE("/workflows/:id", h.DeleteWorkflow)
		api.GET("/workflows/:id/copy-preview", h.CopyWorkflowPreview)
		api.POST("/workflows/:id/copy", h.CopyWorkflow)
		api.POST("/workflows/:id/publish", h.PublishWorkflow)
		api.GET("/workflows/:id/versions", h.WorkflowVersions)
		api.GET("/workflows/:id/versions/:version/graph", h.WorkflowVersionGraph)
		api.POST("/workflows/:id/versions/:version/restore", h.RestoreWorkflowVersion)
		api.POST("/workflows/:id/runs", h.StartRun)
		api.GET("/workflows/:id/api-keys", h.ListAPIKeys)
		api.POST("/workflows/:id/api-keys", h.CreateAPIKey)
		api.DELETE("/workflows/:id/api-keys/:keyId", h.RevokeAPIKey)

		api.GET("/runs", h.ListRuns)
		api.GET("/runs/:id", h.GetRun)
		api.GET("/runs/:id/conversation-chain", h.ConversationChain)
		api.DELETE("/runs/:id", h.DeleteRun)
		api.POST("/runs/:id/cancel", h.CancelRun)
		api.POST("/runs/:id/resume", h.ResumeRun)
		api.PATCH("/runs/:id/priority", h.UpdateRunPriority)
		api.PATCH("/runs/:id/title", h.UpdateRunTitle)
		api.GET("/runs/:id/variables", h.RunVariables)
		api.GET("/runs/:id/artifacts", h.RunArtifacts)
		api.GET("/runs/:id/artifacts/pack", h.PackRunArtifacts)
		api.GET("/runs/:id/logs/export", h.ExportRunLogs)
		api.GET("/runs/:id/llm-transcript", h.RunLlmTranscript)
		api.GET("/runs/:id/nodes/:nodeId/events", h.NodeEvents)
		api.GET("/runs/:id/nodes/:nodeId/sandbox-log", h.NodeSandboxLog)
		api.GET("/runs/:id/nodes/:nodeId/sandbox", h.RunNodeSandbox)
		api.POST("/runs/:id/nodes/:nodeId/takeover", h.TakeoverNode)
		api.GET("/runs/:id/nodes/:nodeId/previews", h.ListNodePreviews)
		api.POST("/runs/:id/nodes/:nodeId/embed-ticket", h.CreateEmbedTicket)
		api.GET("/runs/:id/nodes/:nodeId/live-sessions", h.LiveSessions)
		api.POST("/runs/:id/nodes/:nodeId/live-sessions/discard-all", h.LiveDiscardAll)
		api.GET("/runs/:id/nodes/:nodeId/preview-issues", h.ListPreviewIssues)
		api.POST("/runs/:id/nodes/:nodeId/preview-issues", h.CreatePreviewIssue)
		api.DELETE("/runs/:id/nodes/:nodeId/preview-issues/:issueId", h.DeletePreviewIssue)
		api.POST("/runs/:id/nodes/:nodeId/decision", h.ResumeGate)
		api.POST("/runs/:id/nodes/:nodeId/share-link", h.NodeCreateShareLink)
		api.GET("/runs/:id/nodes/:nodeId/share-link", h.NodeGetShareLink)
		api.POST("/runs/:id/nodes/:nodeId/share-link/regen", h.NodeRegenShareLink)
		api.POST("/runs/:id/nodes/:nodeId/share-link/revoke", h.NodeRevokeShareLink)
		api.POST("/runs/:id/nodes/:nodeId/reply", h.NodeReply)
		api.POST("/runs/:id/nodes/:nodeId/cancel", h.NodeCancel)
		api.POST("/runs/:id/nodes/:nodeId/queue/remove", h.NodeQueueRemove)
		api.POST("/runs/:id/nodes/:nodeId/queue/reorder", h.NodeQueueReorder)
		api.GET("/runs/:id/nodes/:nodeId/primary-artifacts", h.ListGatePrimaryArtifacts)
		api.PUT("/runs/:id/nodes/:nodeId/artifacts/:name", h.SaveGateArtifact)
		api.PUT("/runs/:id/nodes/:nodeId/annotation-artifact", h.SaveAnnotationArtifact)
		api.GET("/runs/:id/nodes/:nodeId/context", h.RunInboxContext)
		api.GET("/runs/:id/events", h.RunEvents)

		api.GET("/pending-nodes", h.ListGates)
		api.GET("/artifacts", h.ListArtifacts)
		api.GET("/artifacts/tree", h.ArtifactTree)
		api.GET("/artifacts/:id/content", h.ArtifactContent)
		api.GET("/artifacts/:id/versions", h.ArtifactVersions)
		api.GET("/artifacts/:id/versions/:rev/content", h.ArtifactVersionContent)
		api.GET("/artifacts/:id/download", h.DownloadArtifact)
		api.DELETE("/artifacts/:id", h.DeleteArtifact)
		api.GET("/blobs/:id", h.GetBlob)

		api.GET("/agents", h.ListAgents)
		api.POST("/agents", h.CreateAgent)
		api.GET("/agents/:name/export", h.ExportAgent)
		api.POST("/agents/import", h.ImportAgent)
		api.GET("/agents/:name/workspace/revisions", h.ListAgentWorkspaceRevisions)
		api.GET("/agents/:name/workspace/revisions/:sha/diff", h.GetAgentWorkspaceRevisionDiff)
		api.POST("/agents/:name/workspace/revisions/:sha/restore", h.RestoreAgentWorkspaceRevision)
		api.GET("/agents/:name", h.GetAgent)
		api.PUT("/agents/:name", h.SaveAgent)
		api.POST("/agents/:name/rename", h.RenameAgent)
		api.DELETE("/agents/:name", h.DeleteAgent)
		// Agent-scoped data (Studio).
		api.GET("/agents/:name/memories", h.ListAgentMemories)
		api.POST("/agents/:name/memories", h.UpsertAgentMemory)
		api.DELETE("/agents/:name/memories", h.ClearAgentMemories)
		api.PUT("/agents/:name/memories/:mid", h.UpdateAgentMemory)
		api.DELETE("/agents/:name/memories/:mid", h.DeleteAgentMemory)
		api.GET("/agents/:name/threads", h.ListAgentThreads)
		api.GET("/agents/:name/threads/:tid/messages", h.GetAgentThreadMessages)
		api.DELETE("/agents/:name/threads/:tid", h.DeleteAgentThread)
		api.GET("/agents/:name/cron-jobs", h.ListAgentCronJobs)
		api.PATCH("/agents/:name/cron-jobs/:jobId", h.PatchAgentCronJob)
		api.DELETE("/agents/:name/cron-jobs/:jobId", h.DeleteAgentCronJob)

		api.GET("/sandboxes", h.ListSandboxes)
		api.POST("/sandboxes/cleanup", h.CleanupSandboxes)
		api.GET("/sandboxes/:id", h.GetSandbox)
		api.POST("/sandboxes/:id/stop", h.StopSandbox)
		api.POST("/sandboxes/:id/leases", h.ExtendSandboxLease)
		api.DELETE("/sandboxes/:id", h.DestroySandbox)
		api.GET("/sandboxes/:id/chat", h.SandboxChat)
		api.GET("/sandboxes/:id/terminal", h.SandboxTerminal)
		api.GET("/sandboxes/:id/events", h.SandboxEvents)
		api.GET("/sandboxes/:id/eventlog", h.SandboxEventLog)
		api.GET("/sandboxes/:id/log", h.SandboxLog)
	}

	// Run-scoped artifact-store MCP (outside /api): the in-container
	// cursor-agent connects here via host.docker.internal with a per-run
	// Bearer token. Streamable-HTTP over POST; GET/DELETE are no-ops.
	r.POST("/mcp/runs/:runId", h.MCPRPC)
	r.GET("/mcp/runs/:runId", h.MCPRPC)
	r.DELETE("/mcp/runs/:runId", h.MCPRPC)
	r.GET("/mcp/runs/:runId/embed-origin", h.MCPEmbedOrigin)

	// Preview-page chat drawer, artifact modal, and image preview frame (outside /api:
	// no cf_session, which a cross-site iframe never carries). A ticket is redeemed for
	// a run/node-bound bearer that the /public/approvals chat endpoints accept.
	r.GET("/embed/runs/:runId/nodes/:nodeId/chat", h.EmbedChatPage)
	r.GET("/embed/runs/:runId/nodes/:nodeId/artifacts", h.EmbedChatPage)
	r.GET("/embed/runs/:runId/nodes/:nodeId/image", h.EmbedChatPage)
	r.GET("/embed/runs/:runId/nodes/:nodeId/demo", h.EmbedChatPage)
	r.POST("/embed-api/session", h.RedeemEmbedSession)

	// PM MCP hosts (outside /api).
	r.POST("/mcp/pm/:mcpId", h.PMMCPRPC)
	r.GET("/mcp/pm/:mcpId", h.PMMCPRPC)
	r.DELETE("/mcp/pm/:mcpId", h.PMMCPRPC)

	r.POST("/mcp/workspace", h.ExternalMCPRPC)
	r.GET("/mcp/workspace", h.ExternalMCPRPC)
	r.DELETE("/mcp/workspace", h.ExternalMCPRPC)
	r.POST("/mcp/workspace/:mcpId", h.ExternalMCPRPC)
	r.GET("/mcp/workspace/:mcpId", h.ExternalMCPRPC)
	r.DELETE("/mcp/workspace/:mcpId", h.ExternalMCPRPC)

	r.POST("/mcp/memory-store", h.MemoryMCPRPC)
	r.GET("/mcp/memory-store", h.MemoryMCPRPC)
	r.DELETE("/mcp/memory-store", h.MemoryMCPRPC)

	r.POST("/mcp/context-store", h.ContextMCPRPC)
	r.GET("/mcp/context-store", h.ContextMCPRPC)
	r.DELETE("/mcp/context-store", h.ContextMCPRPC)

	r.POST("/mcp/task-scheduler/:agentName", h.SchedulerMCPRPC)
	r.GET("/mcp/task-scheduler/:agentName", h.SchedulerMCPRPC)
	r.DELETE("/mcp/task-scheduler/:agentName", h.SchedulerMCPRPC)

	// Loopback-only, token-protected sessions used by `approving doctor
	// --run-demo` to exercise the live MCP HTTP control plane.
	r.POST("/_internal/doctor/artifact-sessions", h.DoctorArtifactSession)
	r.DELETE("/_internal/doctor/artifact-sessions/:id", h.DoctorArtifactSession)

	// Pre-start ConfigHome inject for sandbox-gateway (SANDBOX_INJECT). Bearer
	// token is minted at Create; sandboxes fetch before agent/acp-bridge start.
	r.GET("/sandbox-inject/:id", h.SandboxInject)
	r.HEAD("/sandbox-inject/:id", h.SandboxInject)

	// External /v1 API (Bearer workflow API Key, separate from /api Session auth).
	if h.APIKeys != nil {
		v1 := r.Group("/v1")
		v1.Use(apikey.Middleware(h.APIKeys))
		{
			v1.POST("/workflows/:id/runs", h.V1StartRun)
			v1.GET("/runs/:id", h.V1GetRun)
			v1.GET("/runs/:id/artifacts", h.V1RunArtifacts)
			v1.GET("/artifacts/:id/download", h.V1DownloadArtifact)
			v1.POST("/runs/:id/cancel", h.V1CancelRun)
		}
	}

	// Browser reverse-proxy to sandbox consoles (require session when auth enabled).
	sandboxRoutes := r.Group("")
	if h.Auth != nil {
		sandboxRoutes.Use(h.Auth.SandboxRedirectMiddleware())
	}
	sandboxRoutes.Any("/sandbox/:id/*path", h.SandboxProxy)
	sandboxRoutes.Any("/sandbox-bridge/:id/*path", h.SandboxACPProxy)

	// Preview proxy is intentionally outside SandboxRedirectMiddleware: iframe
	// requests cannot carry cf_session; runId+nodeId+port acts as the credential.
	r.Any("/preview/:runId/:nodeId/:port/*path", h.PreviewProxy)

	// Cooperative pick.js for IP-direct iframe preview (public static).
	r.GET("/preview-pick.js", h.PreviewPickScript)
	r.GET("/page-control.js", h.PageControlScript)
	r.GET("/live-overlay.js", h.LiveOverlayScript)

	// The one VNC address of a sandbox desktop (WebSocket): noVNC RFB proxy +
	// CDP Pick/navigate control. App ports are pages on this desktop.
	r.GET("/sandbox-vnc/:sandboxId/ws", h.SandboxVNC)

	// Public approval page + API (no session). Security headers + no ACAO.
	pub := r.Group("/public/approvals")
	pub.Use(publicGateMiddleware())
	{
		pub.GET("", h.PublicGateApprovalPage)
		pub.GET("/", h.PublicGateApprovalPage)
		pub.GET("/preview", h.PublicGatePreview)
		pub.GET("/images/:index", h.PublicGateImage)
		pub.GET("/artifacts", h.PublicGateArtifacts)
		pub.GET("/artifacts/:name/content", h.PublicGateArtifactContent)
		pub.GET("/upstream", h.PublicGateUpstream)
		pub.GET("/events", h.PublicGateEvents)
		pub.POST("/preview-ticket", h.PublicPreviewTicket)
		pub.POST("/embed-ticket", h.PublicEmbedTicket)
		pub.GET("/preview-vnc/ws", h.PublicPreviewVNC)
		pub.Any("/preview-api/:ticket/*path", h.PublicPreviewAPIProxy)
		pub.POST("/decide", h.PublicGateDecide)
		pub.POST("/reply", h.PublicGateReply)
		pub.GET("/live-sessions", h.PublicLiveSessions)
		pub.POST("/live-discard-all", h.PublicLiveDiscardAll)
		pub.POST("/cancel", h.PublicGateCancel)
		pub.POST("/queue/remove", h.PublicGateQueueRemove)
		pub.POST("/queue/reorder", h.PublicGateQueueReorder)
		pub.OPTIONS("/preview", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/images/:index", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/artifacts", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/artifacts/:name/content", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/upstream", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/preview-ticket", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/embed-ticket", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/decide", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/reply", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/live-sessions", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/live-discard-all", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/cancel", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/queue/remove", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/queue/reorder", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	}

	// SPA fallback: serve index.html for unknown non-API paths so vue-router
	// deep links work; real API/asset/mcp misses still 404.
	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/assets/") ||
			strings.HasPrefix(p, "/fonts/") ||
			strings.HasPrefix(p, "/api/") ||
			strings.HasPrefix(p, "/v1/") ||
			strings.HasPrefix(p, "/mcp/") ||
			strings.HasPrefix(p, "/sandbox/") ||
			strings.HasPrefix(p, "/sandbox-bridge/") ||
			strings.HasPrefix(p, "/sandbox-vnc/") ||
			strings.HasPrefix(p, "/preview/") ||
			strings.HasPrefix(p, "/preview-vnc/") ||
			strings.HasPrefix(p, "/embed-api/") ||
			isPublicApprovalPath(p) || isRetiredPublicGatePath(p) {
			// /mcp/ covers both /mcp/runs/ and /mcp/pm/
			c.String(http.StatusNotFound, "not found: %s", p)
			return
		}
		c.File("./web/dist/index.html")
	})

	return r
}

// errorLogger centralizes server-side visibility for failed requests. Handlers
// return errors to the client as JSON but historically logged nothing, so 5xx
// responses left no server trace (there is also no access-log middleware). This
// logs every 5xx as an error and any handler-attached c.Errors, so unexpected
// failures across all handlers are diagnosable in one place without sprinkling
// log calls through every handler. 4xx are intentionally not logged as errors
// (they are client faults) unless a handler explicitly attached a c.Error.
func errorLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		if status < http.StatusInternalServerError && len(c.Errors) == 0 {
			return
		}
		ev := log.Error()
		if status < http.StatusInternalServerError {
			ev = log.Warn()
		}
		ev = ev.
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", status).
			Str("client_ip", c.ClientIP()).
			Dur("latency", time.Since(start))
		if errs := c.Errors.Errors(); len(errs) > 0 {
			ev = ev.Strs("errors", errs)
		}
		ev.Msg("request failed")
	}
}

func isPublicApprovalPath(p string) bool {
	return strings.HasPrefix(p, "/public/approvals")
}

// Retired share prefix stays out of the SPA shell and out of wildcard CORS,
// so every method including OPTIONS is a plain 404.
func isRetiredPublicGatePath(p string) bool {
	return strings.HasPrefix(p, "/public/gate-approvals")
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Request.URL.Path
		if isPublicApprovalPath(p) || isRetiredPublicGatePath(p) || strings.HasPrefix(p, "/embed-api/") || strings.HasPrefix(p, "/embed/") {
			c.Next()
			return
		}
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

func publicGateMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Pragma", "no-cache")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("X-Content-Type-Options", "nosniff")
		// Opaque preview-api reverse-proxy is framed by the public page (same origin).
		// Do not apply DENY / frame-ancestors 'none' on that path.
		if !strings.Contains(c.Request.URL.Path, "/preview-api/") {
			c.Header("X-Frame-Options", "DENY")
			c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		}
		c.Writer.Header().Del("Access-Control-Allow-Origin")
		c.Writer.Header().Del("Access-Control-Allow-Methods")
		c.Writer.Header().Del("Access-Control-Allow-Headers")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func drainMiddleware(h *handlers.Handlers) gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.Shutdown == nil || !h.Shutdown.IsDraining() {
			c.Next()
			return
		}
		if !isDrainBlocked(c.Request.Method, c.Request.URL.Path) {
			c.Next()
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":                  "shutting_down",
			"error":                   "服务正在关闭，不接受新请求",
			"grace_remaining_seconds": h.Shutdown.GraceRemainingSeconds(),
		})
		c.Abort()
	}
}

func isDrainBlocked(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	if path == "/api/workspace/maintenance" || path == "/api/health" || path == "/api/live" {
		return false
	}
	return strings.HasPrefix(path, "/api/")
}
