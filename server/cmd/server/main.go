// Command server boots the approving backend: config, logging, database +
// seed, the artifact-store MCP host, the execution provider, the FSM
// engine, and the HTTP/WS API.
package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/channels"
	"github.com/cocofhu/grasp/internal/channels/dingtalk"
	"github.com/cocofhu/grasp/internal/channels/feishu"
	"github.com/cocofhu/grasp/internal/channels/qq"
	"github.com/cocofhu/grasp/internal/channels/wecom"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/contextmcp"
	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/logging"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/memorymcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/opencodecatalog"
	"github.com/cocofhu/grasp/internal/pagebridge"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/router"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/schedulermcp"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/shutdown"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func main() {
	logging.Setup()

	// Config: single YAML file (CONFIG_PATH, default "config.yaml"); on K8s
	// it is typically mounted from a ConfigMap at deploy time.
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "config.yaml"
	}
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	if err := config.Load(cfgPath); err != nil {
		log.Fatal().Err(err).Str("path", cfgPath).Msg("load config failed")
	}
	cfg := config.GetConfig()

	// At-rest secret encryption reads its key from the live config (security.
	// secrets_key, with GRASP_SECRETS_KEY env override) so channel credentials
	// stay encrypted in the DB and the key is managed like any other config value.
	crypto.SetKeySource(func() string {
		c := config.GetConfig()
		if c == nil {
			return ""
		}
		return c.SecretsKey()
	})

	// Reload on ConfigMap writes. Values captured below at boot
	// (port, db, provider options) need a restart; the watcher logs a warning
	// when those change so ops know to roll the pod.
	if err := config.WatchAndReload(context.Background(), cfgPath); err != nil {
		log.Warn().Err(err).Msg("config watcher failed to start (hot-reload disabled)")
	}

	gin.SetMode(gin.ReleaseMode)

	db, err := database.Open(cfg.Database)
	if err != nil {
		log.Fatal().Err(err).Msg("open database failed")
	}
	if err := database.Seed(db); err != nil {
		log.Error().Err(err).Msg("seed failed")
	}

	artifactSvc := services.NewArtifactService(db)
	host := mcp.NewHost(artifactSvc)
	// Outcome validation chain is DefaultThenRPC (see mcp.ChainedOutcomeValidator).
	// NewHost installs DefaultOutcomeValidator; wire business RPC later via
	// host.SetRPCOutcomeValidator(...). Default checks always run first.
	// Expose read-only run history to the list_run_history / get_history_detail
	// MCP tools so agent nodes can recall past executions and human feedback.
	runSvc := services.NewRunService(db)
	host.SetHistoryProvider(runSvc)
	// Token lifetime tracks sandbox lifetime: when the in-memory registration is
	// gone (run finished / restart), MCP calls still authorize with the run's
	// persisted token for as long as the run has a live sandbox row. Once the
	// last sandbox is torn down (row deleted), the token stops authorizing.
	host.SetRunTokenSource(func(runID string) (string, bool, bool) {
		var run models.Run
		if db.Select("mcp_token").First(&run, "id = ?", runID).Error != nil {
			return "", false, false
		}
		var live int64
		db.Model(&models.Sandbox{}).Where("run_id = ? AND purpose = ?", runID, "run").Count(&live)
		return run.McpToken, live > 0, true
	})
	// Node-type gate fallback: when the in-memory SetActiveNode registration is
	// gone (server restarted mid-run, or the MCP call is served by a replica
	// that never executed the node — e.g. an app_preview sandbox kept alive
	// during waiting_human), resolve the run's current node + type from the DB
	// so set_preview / set_plan / set_* keep passing their node-type gate
	// instead of being wrongly rejected.
	//
	// Prefer CurrentNodeIDs (running / waiting_human). When that misses but a
	// purpose=run sandbox row still exists — matching authorize's sandbox-alive
	// token window after cancel/finish — fall back to Sandbox.NodeID so an
	// in-sandbox agent can still call set_preview / set_* while the container
	// lives (otherwise ActiveNodeType is "" and node-scoped tools are wrongly
	// rejected after UnregisterRun).
	host.SetActiveNodeSource(func(runID string) (string, string, bool) {
		var run models.Run
		if db.Select("id", "status", "graph").First(&run, "id = ?", runID).Error != nil {
			return "", "", false
		}
		nodeID := runSvc.CurrentNodeIDs([]models.Run{run})[runID]
		if nodeID == "" {
			var sb models.Sandbox
			if db.Where("run_id = ? AND purpose = ?", runID, "run").
				Order("updated_at desc").First(&sb).Error != nil || strings.TrimSpace(sb.NodeID) == "" {
				return "", "", false
			}
			nodeID = sb.NodeID
		}
		nodeType := ""
		if n := run.Graph.FindNode(nodeID); n != nil {
			nodeType = n.Type
		}
		return nodeID, nodeType, true
	})
	// Shared ConfigHome .tgz registry for gateway config.bundleUrl inject
	// (startup.sh extracts before agent start). Served at /sandbox-inject/:id.
	injectStore := sandbox.NewBundleStore()

	blobStore, err := blob.NewFromConfig(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("blob store init failed")
	}

	projectSvc := services.NewProjectService(db)
	auditSvc := services.NewProjectAuditService(db)
	externalMcpSvc := services.NewProjectExternalMcpService(db, cfg.Server.MCPAdvertise)
	projectMcpKeySvc := services.NewProjectMcpApiKeyService(db)
	services.BackfillAuditElevatedFields(db)
	sharedAgentSvc := services.NewSharedAgentService(services.DefaultSharedAgentRoot(cfg.Engine.ProfilesRoot))
	services.MigrateProjectSandboxEnvOnce(db, projectSvc, sharedAgentSvc)
	// One snapshot of OpenCode's provider catalog, shared by the pickers and by
	// every path that generates opencode.json, so both agree on which vendor ids
	// OpenCode can resolve without an adapter of our own.
	openCodeCatalog := opencodecatalog.New(cfg.Sandbox.OpenCodeCatalogURL)
	provider := runtime.NewProvider(cfg.Engine.ExecProvider, host, runtime.Options{
		SandboxImage:         cfg.Sandbox.Image,
		SandboxImages:        cfg.Sandbox.Images,
		GatewayURL:           cfg.Sandbox.GatewayURL,
		GatewayAPIKey:        cfg.Sandbox.GatewayAPIKey,
		Env:                  cfg.Sandbox.Env,
		CursorAuthPath:       cfg.Sandbox.CursorAuthPath,
		ChatTimeout:          cfg.AgentChatTimeout(),
		ChatIdleTimeout:      cfg.ChatIdleTimeout(),
		SandboxMaxAttempts:   cfg.Sandbox.MaxAttempts,
		SandboxRetryBackoff:  cfg.SandboxRetryBackoff(),
		SandboxCreateTimeout: cfg.SandboxCreateTimeout(),
		MCPEndpoint:          cfg.Server.MCPAdvertise,
		InjectStore:          injectStore,
		Blobs:                blobStore,
		ProfilesRoot:         cfg.Engine.ProfilesRoot,
		PlatformRulesRoot:    cfg.Engine.PlatformRulesRoot,
		ProjectIDForWorkflow: func(workflowID string) string {
			var wf models.WorkflowDef
			if err := db.Select("project_id").First(&wf, "id = ?", workflowID).Error; err != nil {
				return ""
			}
			return wf.ProjectID
		},
		SharedAgentForProject: func(projectID string) runtime.SharedAgentView {
			cfg := sharedAgentSvc.Get(projectID)
			mcp := make([]runtime.SharedMCPView, 0, len(cfg.MCP))
			for _, m := range cfg.MCP {
				mcp = append(mcp, runtime.SharedMCPView{
					Name: m.Name, URL: m.URL, Headers: m.Headers,
					Command: m.Command, Args: m.Args, Env: m.Env,
				})
			}
			return runtime.SharedAgentView{
				AcpBackend:       cfg.AcpBackend,
				GitSshKnownHosts: cfg.GitSshKnownHosts,
				GitSshPrivateKey: cfg.GitSshPrivateKey,
				MCP:              mcp,
				Env:              cfg.Env,
				Layout: runtime.SharedLayoutView{
					ConfigRoot: cfg.Layout.ConfigRoot, WorkspaceDir: cfg.Layout.WorkspaceDir,
				},
				Prompts:   cfg.Prompts,
				WorkDir:   sharedAgentSvc.WorkDir(projectID),
				ProjectID: pickSharedProjectID(cfg),
			}
		},
		RunSandboxEnvForRun: func(runID string) []models.EnvEntry {
			var run models.Run
			if err := db.Select("sandbox_env").First(&run, "id = ?", runID).Error; err != nil {
				return nil
			}
			return run.SandboxEnv
		},
		PublicAdvertise: cfg.Server.PublicAdvertise,
		OpenCodeCatalog: openCodeCatalog,
	})
	eng := engine.New(db, provider, host, artifactSvc, cfg.Engine.MaxConcurrentRuns)
	eng.SetBlobStore(blobStore)
	eng.SetProjectVarsLookup(func(workflowID string) []models.ProjectVariable {
		return projectSvc.VariablesForWorkflow(workflowID)
	})
	eng.SetAuditRecorder(func(rec services.AuditRecord) {
		auditSvc.Record(rec)
	})
	pageHub := pagebridge.NewHub()
	host.SetPageBridge(&pagebridge.Router{Hub: pageHub, Turns: eng})
	gateShareSvc := gateshare.NewService(db, auditSvc)
	gateShareTickets := gateshare.NewTicketStore(db)
	gateShareSessions := gateshare.NewPreviewSessionHub()
	embedStore := embed.NewStore(db)
	gateShareSvc.SetEmbedLookup(func(token string) (gateshare.EmbedRef, bool) {
		c, ok := embedStore.LookupSession(token)
		if !ok {
			return gateshare.EmbedRef{}, false
		}
		ref := gateshare.EmbedRef{RunID: c.RunID, NodeID: c.NodeID, ExpiresAt: c.ExpiresAt}
		if c.Kind == models.EmbedKindShare {
			ref.ShareTokenHash = c.ShareTokenHash
		}
		return ref, true
	})
	gateShareSvc.SetInvalidationHook(func(tokenHashes []string) {
		for _, th := range tokenHashes {
			gateShareTickets.InvalidateByTokenHash(th)
		}
		embedStore.InvalidateShare(tokenHashes...)
		gateShareSessions.KickMany(tokenHashes)
	})
	eng.SetShareRevoker(gateShareSvc)
	host.SetProjectAuditHook(func(runID, nodeID, tool string, args map[string]any, resultText string, isError bool) {
		projectID := services.ResolveProjectIDForRun(db, runID)
		if projectID == "" {
			return
		}
		outcome := models.AuditOutcomeOK
		if isError {
			outcome = models.AuditOutcomeFail
		}
		// Structured payload; SecretMask applied inside Record.
		resultPayload := any(resultText)
		if len(resultText) > 2000 {
			resultPayload = resultText[:2000] + "…"
		}
		node := strings.TrimSpace(nodeID)
		if node == "mcp" {
			node = ""
		}
		auditSvc.Record(services.AuditRecord{
			ProjectID:    projectID,
			Actor:        services.SystemActor(), // MCP host has no Session
			CallerKind:   models.CallerKindSystem,
			Action:       models.AuditActionMCPCall,
			ResourceType: "mcp",
			ResourceID:   tool,
			RunID:        runID,
			NodeID:       node,
			Outcome:      outcome,
			// Semantic summary from original args (before Record masks payload).
			// History is not backfilled; export emits the stored Summary as-is.
			Summary: mcp.FormatMCPAuditSummary(tool, args, resultText, isError),
			Payload: map[string]any{
				"tool":      tool,
				"runId":     runID,
				"nodeId":    node,
				"arguments": args,
				"result":    resultPayload,
				"isError":   isError,
			},
		})
	})

	// Where per-sandbox ConfigHome trees are staged before gateway bundleUrl /
	// SSH inject. Empty → OS temp dir (see config.Sandbox.WorkDir).
	sandbox.HomeBaseDir = cfg.Sandbox.WorkDir

	agentSvc := services.NewAgentService(cfg.Engine.ProfilesRoot)
	eng.SetAgents(agentSvc)
	orgSvc := services.NewOrgService(cfg.Engine.ProfilesRoot, agentSvc)
	platformRuleSvc, err := services.NewPlatformRuleService(cfg.Engine.PlatformRulesRoot, cfg.Engine.ProfilesRoot)
	if err != nil {
		log.Fatal().Err(err).Msg("platform rules init failed")
	}
	sbxGateway := sandbox.NewGatewayClient(cfg.Sandbox.GatewayURL, cfg.Sandbox.GatewayAPIKey)
	sbxMgr := sandbox.NewManager(sbxGateway, sandbox.ManagerOptions{
		Image:           cfg.Sandbox.Image,
		WorkspaceDir:    "/root/workspace",
		InstallHelpers:  true,
		InjectStore:     injectStore,
		InjectAdvertise: cfg.Server.MCPAdvertise,
		CreateTimeout:   cfg.SandboxCreateTimeout(),
		Blobs:           blobStore,
	})
	log.Info().Str("gateway", cfg.Sandbox.GatewayURL).Msg("sandbox control plane: sandbox-gateway")
	sbxSvc := services.NewSandboxService(db, sbxMgr, agentSvc, host, services.SandboxOptions{
		ProfilesRoot:      cfg.Engine.ProfilesRoot,
		PlatformRulesRoot: cfg.Engine.PlatformRulesRoot,
		MCPEndpoint:       cfg.Server.MCPAdvertise,
		Env:               cfg.Sandbox.Env,
		ChatTimeout:       cfg.AgentChatTimeout(),
		TTL:               cfg.TestSandboxTTL(),
		RunTTL:            cfg.RunSandboxTTL(),
		Max:               cfg.Sandbox.MaxTestSandboxes,
		SharedAgent:       sharedAgentSvc,
		OpenCodeCatalog:   openCodeCatalog,
	})
	// Let the exec provider record per-run node sandboxes in the same store so
	// they show up in the sandbox UI alongside interactive test sandboxes.
	if rr, ok := provider.(runtime.SandboxRegistrar); ok {
		rr.SetSandboxRegistry(sbxSvc)
	}
	// Reconcile DB ↔ gateway on boot and start the idle-TTL sweeper. The
	// gateway owns image lifecycle now, so there is no local image pre-pull.
	sbxSvc.ReconcileOnStartup(context.Background())
	sweeperCtx, stopSweeper := context.WithCancel(context.Background())
	go sbxSvc.RunSweeper(sweeperCtx)

	// In-sandbox VNC preview: dials each app_preview sandbox's CDP/websockify.
	browserSvc := browser.New(sbxMgr, browser.Config{
		MaxTabs:             cfg.Browser.MaxTabs,
		MaxTabsPerContainer: cfg.Browser.MaxTabsPerContainer,
		TabIdleTTL:          cfg.TabIdleTTL(),
		ContainerIdleTTL:    cfg.ContainerIdleTTL(),
	})
	browserSvc.Start()
	log.Info().Int("max_tabs", cfg.Browser.MaxTabs).Msg("in-sandbox vnc preview enabled")

	// Platform settings: DB override layer over the read-only config file for
	// runtime-tunable scheduling params. ApplyOnBoot re-applies persisted UI
	// values so they survive restarts.
	settingsSvc := services.NewSettingsService(db, eng, sbxSvc)
	settingsSvc.ApplyOnBoot()

	previewSvc := services.NewPreviewService(db, sbxMgr)
	previewSvc.SetBrowser(browserSvc)
	host.SetPreviewStore(previewSvc)
	host.SetPreviewSandboxOps(previewSvc)
	host.SetPreviewBaseURL(cfg.Server.PublicAdvertise)

	// Preview feedback: humans report problems from the app_preview UI (one-way).
	// The engine snapshots them into the preview_issues run variable at gate
	// resume so a downstream node consumes them via {{vars.preview_issues}}.
	issueSvc := services.NewIssueService(db)
	issueSvc.SetBlobStore(blobStore)
	eng.SetIssueService(issueSvc)

	requirementDraftSvc := services.NewRequirementDraftService(db)
	notificationSvc := services.NewNotificationService(db, runSvc)

	coord := shutdown.New(cfg.AgentChatTimeout())
	authSvc := auth.NewService(db, config.GetConfig)

	pmSvc := services.NewPmService(db, agentSvc)
	pmSvc.SetBlobStore(blobStore)
	pmProgress := services.NewPmProgress(pmSvc, runSvc, artifactSvc)
	wfSvc := services.NewWorkflowService(db)
	wfSvc.SetAgents(agentSvc)
	pmMCP := pmmcp.NewHost(pmSvc, pmProgress, wfSvc, runSvc, artifactSvc, eng)
	pmMCP.SetOrgAndAgent(orgSvc, agentSvc)
	pmMCP.SetRequirementDrafts(requirementDraftSvc)
	memoryMCP := memorymcp.NewHost(pmSvc)
	contextMCP := contextmcp.NewHost(pmSvc)
	schedulerMCP := schedulermcp.NewHost(db, pmSvc)
	recordMCPAudit := func(rec services.AuditRecord) { auditSvc.Record(rec) }
	pmMCP.SetAuditRecorder(recordMCPAudit)
	memoryMCP.SetAuditRecorder(recordMCPAudit)
	contextMCP.SetAuditRecorder(recordMCPAudit)
	schedulerMCP.SetAuditRecorder(recordMCPAudit)
	mcpWire := &platformMCPWire{
		pm: pmMCP, memory: memoryMCP, context: contextMCP,
		scheduler: schedulerMCP, pmSvc: pmSvc, skills: agentSvc,
	}
	pmTurns := services.NewPmTurnRunner(pmSvc, sbxSvc)
	pmTurns.SetCitationDeps(runSvc, artifactSvc, wfSvc)
	// Raise the per-turn deadline well above the legacy 90s so channel/cron and
	// interactive PM turns are not truncated (aligns with the sandbox chat cap).
	pmTurns.SetTurnDeadline(cfg.AgentChatTimeout() + 30*time.Second)
	sbxSvc.SetAgentSandboxDestroyHook(func(projectID, threadID, token string) {
		mcpWire.unregister(token)
		mcpWire.clearSandboxRef(threadID)
	})
	sbxSvc.SetTestSchedulerHooks(services.TestSchedulerHooks{
		Register: func(projectID, profile, runID, token string) {
			schedulerMCP.Restore(token, projectID, profile, runID, "test", false)
		},
		Unregister: schedulerMCP.Unregister,
	})

	cronSched := services.NewCronScheduler(db, pmSvc, sbxSvc, pmTurns, services.CronTokenHooks{
		Register:   mcpWire.registerCron,
		Unregister: mcpWire.unregister,
	})
	gateAutoSvc := services.NewGateAutoInvokeService(db, pmSvc, sbxSvc, pmTurns, services.CronTokenHooks{
		Register:   mcpWire.registerCron,
		Unregister: mcpWire.unregister,
	})
	eng.SetGateAutoInvoker(gateAutoEngineAdapter{svc: gateAutoSvc})
	runNotifySvc := services.NewRunNotifyService(db, nil, cfg.Server.PublicAdvertise)
	eng.SetRunNotifier(runNotifyEngineAdapter{svc: runNotifySvc})
	// External IM channels (QQ today; extensible). One bot binds one project +
	// its PM Leader. Configs are DB-managed via the admin WebUI and hot-reloaded.
	// Memory/scheduler writes follow ChannelConfig session caps (default off).
	// pm-workflow-write is controlled by project EnabledMcps — enable it only
	// when channel-side mutations are desired.
	channelHooks := channels.MCPTokenHooks{
		Register:       mcpWire.registerChannel,
		RestoreOnReuse: mcpWire.restoreChannel,
		Unregister:     mcpWire.unregister,
	}
	channelBridge := channels.NewChannelBridge(pmSvc, sbxSvc, pmTurns, channelHooks)
	channelSvc := services.NewChannelConfigService(db)
	channelSvc.SetAgentService(agentSvc)
	channelMgr := channels.NewManager(channelBridge, map[string]channels.AdapterFactory{
		models.ChannelTypeQQ:       qq.New,
		models.ChannelTypeWeCom:    wecom.New,
		models.ChannelTypeFeishu:   feishu.New,
		models.ChannelTypeDingTalk: dingtalk.New,
	}, crypto.Decrypt)
	channelMgr.SetLoader(channelSvc.ListRaw)
	channelSvc.SetRuntimeLookup(channelMgr.RuntimeState)
	channelSvc.SetOnChange(channelMgr.Reload)
	channelSvc.SetOnlineLookup(channelMgr.IsOnline)
	channelMgr.ApplyOnBoot()
	cronSched.SetChannelDeliverer(channelMgr)
	runNotifySvc.SetDeliverer(channelMgr)
	cronSched.Start(sweeperCtx)

	h := &handlers.Handlers{
		WF:                wfSvc,
		Projects:          projectSvc,
		Runs:              runSvc,
		Arts:              artifactSvc,
		APIKeys:           services.NewAPIKeyService(db),
		Agents:            agentSvc,
		SharedAgent:       sharedAgentSvc,
		Org:               orgSvc,
		Dash:              services.NewDashboardService(db, projectSvc),
		Sbx:               sbxSvc,
		Preview:           previewSvc,
		Issues:            issueSvc,
		RequirementDrafts: requirementDraftSvc,
		Notifications:     notificationSvc,
		Eng:               eng,
		MCP:               host,
		Pm:                pmSvc,
		PmProgress:        pmProgress,
		PmTurns:           pmTurns,
		PMMCP:             pmMCP,
		MemoryMCP:         memoryMCP,
		ContextMCP:        contextMCP,
		SchedulerMCP:      schedulerMCP,
		Settings:          settingsSvc,
		Shutdown:          coord,
		Auth:              authSvc,
		PlatformRules:     platformRuleSvc,
		Channels:          channelSvc,
		RunNotify:         runNotifySvc,
		Browser:           browserSvc,
		Audit:             auditSvc,
		ExternalMcp:       externalMcpSvc,
		ProjectMcpKeys:    projectMcpKeySvc,
		GateShare:         gateShareSvc,
		GateShareNonces:   gateshare.NewNonceStore(db),
		GateShareTickets:  gateShareTickets,
		Embed:             embedStore,
		PageBridge:        pageHub,
		GateShareSessions: gateShareSessions,
		GateShareLimiter:  gateshare.NewIPLimiter(),
		PublicAdvertise:   cfg.Server.PublicAdvertise,
		InjectBundles:     injectStore,
		Blobs:             blobStore,
		Onboarding:        services.NewOnboardingService(projectSvc, agentSvc, sharedAgentSvc, wfSvc, orgSvc),
		Team:              services.NewTeamService(projectSvc, agentSvc, orgSvc, pmSvc, sbxSvc),
		OpenCodeCatalog:   openCodeCatalog,
	}
	if h.Team != nil && h.PMMCP != nil {
		h.PMMCP.SetTeam(h.Team)
	}

	r := router.New(h)
	port := strconv.Itoa(cfg.Server.Port)
	addr := ":" + port
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal().Err(err).Str("addr", addr).Msg("listen failed")
	}
	srv := &http.Server{Handler: r}

	log.Info().Str("port", port).Str("exec_provider", provider.Name()).
		Str("config_path", cfgPath).Msg("approving server starting")

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server exited")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit
	log.Info().Str("signal", sig.String()).Msg("signal received")

	coord.BeginDraining()
	log.Info().Msg("drain started")

	// Stop admitting new work and drop anything still queued. The drain
	// middleware already 503s new mutating /api requests; halting the scheduler
	// prevents the dispatcher from promoting queued runs while we drain.
	eng.Halt()
	eng.CancelQueuedRuns()

	// Keep the HTTP server up while waiting for active agent/react nodes to
	// finish: their in-container sandboxes still call back to /mcp/runs/:id on
	// this same server, and the frontend keeps polling /api/health to render the
	// draining banner. Waiting first (bounded by the grace deadline) is what
	// lets rolling updates finish in-flight agent work instead of severing it.
	timedOut := eng.WaitAgentReact(context.Background(), coord.Deadline())

	// Now tear everything down: idle sweeper, interactive test sandboxes, then
	// the HTTP server (which closes /api and /mcp for good).
	stopSweeper()
	channelMgr.StopAll()
	browserSvc.Stop()
	sbxSvc.ShutdownAllTestSandboxes(context.Background(), true)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("http server shutdown")
	}

	if timedOut {
		log.Info().Msg("exit")
		os.Exit(1)
	}
	log.Info().Msg("exit")
	os.Exit(0)
}

func pickSharedProjectID(cfg services.SharedAgentConfig) string {
	if v := strings.TrimSpace(cfg.DefaultProjectID); v != "" {
		return v
	}
	return strings.TrimSpace(cfg.ProjectID)
}
