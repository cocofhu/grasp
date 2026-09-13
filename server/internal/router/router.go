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

// New builds the gin engine with all approving routes registered.
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
		api.GET("/stats/platform-status", h.PlatformStatus)

		api.GET("/settings", h.GetSettings)
		api.PUT("/settings", h.UpdateSettings)

		api.GET("/notifications", h.ListNotifications)
		api.POST("/notifications/read", h.MarkNotificationRead)
		api.POST("/notifications/read-all", h.MarkAllNotificationsRead)

		api.GET("/opencode/providers", h.ListOpenCodeProviders)
		api.GET("/opencode/providers/:provider/models", h.ListOpenCodeProviderModels)

		api.GET("/platform-rules", h.ListPlatformRules)
		api.GET("/platform-rules/:file/embed", h.GetPlatformRuleEmbed)
		api.GET("/platform-rules/:file", h.GetPlatformRule)
		api.PUT("/platform-rules/:file", h.SavePlatformRule)
		api.DELETE("/platform-rules/:file", h.DeletePlatformRule)
		api.POST("/platform-rules/:file/reset", h.ResetPlatformRule)

		api.GET("/projects", h.ListProjects)
		api.POST("/projects", h.CreateProject)
		api.GET("/projects/:id", h.GetProject)
		api.GET("/projects/:id/run-tags", h.ListProjectRunTags)
		api.GET("/projects/:id/token-stats", h.GetProjectTokenStats)
		api.GET("/projects/:id/audit", h.ListProjectAudit)
		api.GET("/projects/:id/audit/facets", h.ListProjectAuditFacets)
		api.GET("/projects/:id/audit/export", h.ExportProjectAudit)
		api.PUT("/projects/:id", h.UpdateProject)
		api.PATCH("/projects/:id", h.UpdateProject)
		api.DELETE("/projects/:id", h.DeleteProject)
		api.POST("/projects/:id/bootstrap-onboarding", h.BootstrapProjectOnboarding)
		api.GET("/projects/:id/external-mcp", h.GetProjectExternalMcp)
		api.PUT("/projects/:id/external-mcp", h.UpdateProjectExternalMcp)
		api.GET("/projects/:id/external-mcp/keys", h.ListProjectMcpKeys)
		api.POST("/projects/:id/external-mcp/keys", h.CreateProjectMcpKey)
		api.DELETE("/projects/:id/external-mcp/keys/:keyId", h.RevokeProjectMcpKey)
		api.GET("/projects/:id/shared-agent-config", h.GetProjectSharedAgent)
		api.PUT("/projects/:id/shared-agent-config", h.PutProjectSharedAgent)
		api.POST("/projects/:id/shared-agent-config/test", h.CreateProjectSharedAgentTest)

		api.GET("/projects/:id/requirement-drafts", h.ListRequirementDrafts)
		api.POST("/projects/:id/requirement-drafts", h.CreateRequirementDraft)
		api.GET("/projects/:id/requirement-drafts/:draftId", h.GetRequirementDraft)
		api.PUT("/projects/:id/requirement-drafts/:draftId", h.UpdateRequirementDraft)
		api.PATCH("/projects/:id/requirement-drafts/:draftId", h.UpdateRequirementDraft)
		api.PATCH("/projects/:id/requirement-drafts/:draftId/status", h.PatchRequirementDraftStatus)
		api.PATCH("/projects/:id/requirement-drafts/:draftId/schedule", h.PatchRequirementDraftSchedule)
		api.DELETE("/projects/:id/requirement-drafts/:draftId", h.DeleteRequirementDraft)

		api.GET("/agent-teams/templates", h.ListAgentTeamTemplates)
		api.POST("/agent-teams/bootstrap", h.BootstrapAgentTeam)
		api.GET("/agent-teams/bootstrap/:id", h.GetAgentTeamBootstrap)
		api.POST("/agent-teams/bootstrap/:id/retry", h.RetryAgentTeamBootstrap)

		api.GET("/projects/:id/pm-leader", h.GetPmLeader)
		api.PUT("/projects/:id/pm-leader", h.UpdatePmLeader)
		api.GET("/projects/:id/cron-jobs", h.ListProjectCronJobs)
		api.GET("/projects/:id/notify-receipts", h.ListProjectNotifyReceipts)
		api.PATCH("/projects/:id/cron-jobs/:jobId", h.PatchProjectCronJob)
		api.DELETE("/projects/:id/cron-jobs/:jobId", h.DeleteProjectCronJob)
		// Plural multi-channel APIs (primary + secondary).
		api.GET("/projects/:id/channels", h.ListProjectChannels)
		api.POST("/projects/:id/channels", h.CreateProjectChannel)
		api.PUT("/projects/:id/channels/:channelId", h.UpdateProjectChannel)
		api.DELETE("/projects/:id/channels/:channelId", h.DeleteProjectChannelByID)
		// Legacy singular aliases → primary channel.
		api.GET("/projects/:id/channel", h.GetProjectChannel)
		api.PUT("/projects/:id/channel", h.PutProjectChannel)
		api.DELETE("/projects/:id/channel", h.DeleteProjectChannel)
		api.GET("/projects/:id/pm/memories", h.ListPmMemories)
		api.POST("/projects/:id/pm/memories", h.UpsertPmMemory)
		api.DELETE("/projects/:id/pm/memories", h.ClearPmMemories)
		api.PUT("/projects/:id/pm/memories/:mid", h.UpdatePmMemory)
		api.DELETE("/projects/:id/pm/memories/:mid", h.DeletePmMemory)
		api.GET("/projects/:id/pm/threads", h.ListPmThreads)
		api.POST("/projects/:id/pm/threads", h.CreatePmThread)
		api.GET("/projects/:id/pm/threads/:tid", h.GetPmThread)
		api.DELETE("/projects/:id/pm/threads/:tid", h.DeletePmThread)
		api.GET("/projects/:id/pm/threads/:tid/messages", h.ListPmMessages)
		api.POST("/projects/:id/pm/threads/:tid/messages", h.AppendPmMessage)
		api.PATCH("/projects/:id/pm/threads/:tid/messages/:mid", h.PatchPmMessage)
		api.POST("/projects/:id/pm/threads/:tid/sandbox", h.EnsurePmSandbox)
		api.GET("/projects/:id/pm/threads/:tid/draft", h.GetPmDraft)
		api.GET("/projects/:id/pm/threads/:tid/chat", h.PmThreadChat)

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
		api.DELETE("/runs/:id", h.DeleteRun)
		api.POST("/runs/:id/cancel", h.CancelRun)
		api.POST("/runs/:id/resume", h.ResumeRun)
		api.PATCH("/runs/:id/priority", h.UpdateRunPriority)
		api.GET("/runs/:id/variables", h.RunVariables)
		api.GET("/runs/:id/artifacts", h.RunArtifacts)
		api.GET("/runs/:id/artifacts/pack", h.PackRunArtifacts)
		api.GET("/runs/:id/logs/export", h.ExportRunLogs)
		api.GET("/runs/:id/inbox-context", h.RunInboxContext)
		api.GET("/runs/:id/nodes/:nodeId/events", h.NodeEvents)
		api.GET("/runs/:id/nodes/:nodeId/sandbox-log", h.NodeSandboxLog)
		api.GET("/runs/:id/nodes/:nodeId/sandbox", h.RunNodeSandbox)
		api.GET("/runs/:id/nodes/:nodeId/previews", h.ListNodePreviews)
		api.GET("/runs/:id/nodes/:nodeId/preview-issues", h.ListPreviewIssues)
		api.POST("/runs/:id/nodes/:nodeId/preview-issues", h.CreatePreviewIssue)
		api.DELETE("/runs/:id/nodes/:nodeId/preview-issues/:issueId", h.DeletePreviewIssue)
		api.POST("/runs/:id/gates/:nodeId/resume", h.ResumeGate)
		api.POST("/runs/:id/gates/:nodeId/share-link", h.CreateGateShareLink)
		api.GET("/runs/:id/gates/:nodeId/share-link", h.GetGateShareLink)
		api.POST("/runs/:id/gates/:nodeId/share-link/regen", h.RegenGateShareLink)
		api.POST("/runs/:id/gates/:nodeId/share-link/revoke", h.RevokeGateShareLink)
		api.POST("/runs/:id/reviews/:nodeId/share-link", h.CreateReviewShareLink)
		api.GET("/runs/:id/reviews/:nodeId/share-link", h.GetReviewShareLink)
		api.POST("/runs/:id/reviews/:nodeId/share-link/regen", h.RegenReviewShareLink)
		api.POST("/runs/:id/reviews/:nodeId/share-link/revoke", h.RevokeReviewShareLink)
		api.POST("/runs/:id/gates/:nodeId/react-revise", h.GateReactRevise)
		api.POST("/runs/:id/gates/:nodeId/react-cancel", h.GateReactCancel)
		api.POST("/runs/:id/gates/:nodeId/react-queue/remove", h.GateReactQueueRemove)
		api.POST("/runs/:id/gates/:nodeId/react-queue/reorder", h.GateReactQueueReorder)
		api.GET("/runs/:id/gates/:nodeId/primary-artifacts", h.ListGatePrimaryArtifacts)
		api.PUT("/runs/:id/gates/:nodeId/artifacts/:name", h.SaveGateArtifact)
		api.PUT("/runs/:id/gates/:nodeId/annotation-artifact", h.SaveAnnotationArtifact)
		api.POST("/runs/:id/react/:nodeId/reply", h.ReactReply)
		api.POST("/runs/:id/react/:nodeId/cancel", h.ReactCancel)
		api.POST("/runs/:id/react/:nodeId/queue/remove", h.ReactQueueRemove)
		api.POST("/runs/:id/react/:nodeId/queue/reorder", h.ReactQueueReorder)
		api.GET("/runs/:id/events", h.RunEvents)

		api.GET("/gates", h.ListGates)
		api.GET("/artifacts", h.ListArtifacts)
		api.GET("/artifacts/:id/content", h.ArtifactContent)
		api.GET("/artifacts/:id/download", h.DownloadArtifact)
		api.DELETE("/artifacts/:id", h.DeleteArtifact)
		api.GET("/blobs/:id", h.GetBlob)

		api.GET("/agents", h.ListAgents)
		api.POST("/agents", h.CreateAgent)
		// /agents/org must be registered before /agents/:name so "org" is not captured as a name.
		api.GET("/agents/org", h.GetAgentsOrg)
		api.PUT("/agents/org", h.PutAgentsOrg)
		api.GET("/agents/org/export", h.ExportOrgFolder)
		api.POST("/agents/org/import", h.ImportOrgFolder)
		api.GET("/agents/org/sensitive-keys", h.ScanOrgSensitiveKeys)
		api.POST("/agents/org/strip-sensitive-keys", h.StripOrgSensitiveKeys)
		api.GET("/agents/:name/export", h.ExportAgent)
		api.POST("/agents/import", h.ImportAgent)
		api.GET("/agents/:name/platform-rules", h.ListAgentPlatformRules)
		api.GET("/agents/:name/platform-rules/:file", h.GetAgentPlatformRule)
		api.PUT("/agents/:name/platform-rules/:file", h.SaveAgentPlatformRule)
		api.DELETE("/agents/:name/platform-rules/:file", h.DeleteAgentPlatformRule)
		api.GET("/agents/:name/workspace/revisions", h.ListAgentWorkspaceRevisions)
		api.GET("/agents/:name/workspace/revisions/:sha/diff", h.GetAgentWorkspaceRevisionDiff)
		api.POST("/agents/:name/workspace/revisions/:sha/restore", h.RestoreAgentWorkspaceRevision)
		api.GET("/agents/:name", h.GetAgent)
		api.PUT("/agents/:name", h.SaveAgent)
		api.PATCH("/agents/:name/project", h.PatchAgentProject)
		api.POST("/agents/:name/rename", h.RenameAgent)
		api.DELETE("/agents/:name", h.DeleteAgent)
		// Agent-scoped data (Studio). Project resolved from agent.projectId.
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

	// Project-scoped PM MCP hosts (outside /api).
	r.POST("/mcp/pm/:projectId", h.PMMCPRPC)
	r.GET("/mcp/pm/:projectId", h.PMMCPRPC)
	r.DELETE("/mcp/pm/:projectId", h.PMMCPRPC)
	r.POST("/mcp/pm/:projectId/:mcpId", h.PMMCPRPC)
	r.GET("/mcp/pm/:projectId/:mcpId", h.PMMCPRPC)
	r.DELETE("/mcp/pm/:projectId/:mcpId", h.PMMCPRPC)

	r.POST("/mcp/external/:projectId", h.ExternalMCPRPC)
	r.GET("/mcp/external/:projectId", h.ExternalMCPRPC)
	r.DELETE("/mcp/external/:projectId", h.ExternalMCPRPC)
	r.POST("/mcp/external/:projectId/:mcpId", h.ExternalMCPRPC)
	r.GET("/mcp/external/:projectId/:mcpId", h.ExternalMCPRPC)
	r.DELETE("/mcp/external/:projectId/:mcpId", h.ExternalMCPRPC)

	r.POST("/mcp/memory-store/:projectId", h.MemoryMCPRPC)
	r.GET("/mcp/memory-store/:projectId", h.MemoryMCPRPC)
	r.DELETE("/mcp/memory-store/:projectId", h.MemoryMCPRPC)

	r.POST("/mcp/context-store/:projectId", h.ContextMCPRPC)
	r.GET("/mcp/context-store/:projectId", h.ContextMCPRPC)
	r.DELETE("/mcp/context-store/:projectId", h.ContextMCPRPC)

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
	sandboxRoutes.Any("/sandbox-acp/:id/*path", h.SandboxACPProxy)

	// Preview proxy is intentionally outside SandboxRedirectMiddleware: iframe
	// requests cannot carry cf_session; runId+nodeId+port acts as the credential.
	r.Any("/preview/:runId/:nodeId/:port/*path", h.PreviewProxy)

	// Cooperative pick.js for IP-direct iframe preview (public static).
	r.GET("/preview-pick.js", h.PreviewPickScript)

	// VNC preview (WebSocket): noVNC RFB proxy + CDP Pick/navigate control.
	r.GET("/preview-vnc/:runId/:nodeId/:port/ws", h.PreviewVNC)

	// Console VNC (WebSocket): sandbox-scoped noVNC proxy (no preview port triple).
	r.GET("/sandbox-vnc/:sandboxId/ws", h.SandboxVNC)

	// Public gate-approval page + API (no session). Security headers + no ACAO.
	pub := r.Group("/public/gate-approvals")
	pub.Use(publicGateMiddleware())
	{
		pub.GET("", h.PublicGateApprovalPage)
		pub.GET("/", h.PublicGateApprovalPage)
		pub.GET("/preview", h.PublicGatePreview)
		pub.GET("/artifacts", h.PublicGateArtifacts)
		pub.GET("/artifacts/:name/content", h.PublicGateArtifactContent)
		pub.GET("/upstream", h.PublicGateUpstream)
		pub.GET("/events", h.PublicGateEvents)
		pub.POST("/preview-ticket", h.PublicPreviewTicket)
		pub.GET("/preview-vnc/ws", h.PublicPreviewVNC)
		pub.Any("/preview-api/:ticket/*path", h.PublicPreviewAPIProxy)
		pub.POST("/decide", h.PublicGateDecide)
		pub.POST("/reply", h.PublicGateReply)
		pub.POST("/cancel", h.PublicGateCancel)
		pub.POST("/queue/remove", h.PublicGateQueueRemove)
		pub.POST("/queue/reorder", h.PublicGateQueueReorder)
		pub.OPTIONS("/preview", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/artifacts", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/artifacts/:name/content", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/upstream", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/preview-ticket", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/decide", func(c *gin.Context) { c.Status(http.StatusNoContent) })
		pub.OPTIONS("/reply", func(c *gin.Context) { c.Status(http.StatusNoContent) })
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
			strings.HasPrefix(p, "/sandbox-acp/") ||
			strings.HasPrefix(p, "/sandbox-vnc/") ||
			strings.HasPrefix(p, "/preview/") ||
			strings.HasPrefix(p, "/preview-vnc/") ||
			strings.HasPrefix(p, "/public/gate-approvals") {
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

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/public/gate-approvals") {
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
	if path == "/api/health" || path == "/api/live" {
		return false
	}
	return strings.HasPrefix(path, "/api/")
}
