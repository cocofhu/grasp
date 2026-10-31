// Command server boots the approving backend: config, logging, database +
// seed, the artifact-store MCP host, the execution provider, the FSM
// engine, and the HTTP/WS API.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	"github.com/cocofhu/grasp/internal/modelrouting"
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

const (
	// shutdownHTTPTimeout bounds how long shutdown waits for in-flight HTTP
	// requests. It is also the grace shown on the draining banner.
	shutdownHTTPTimeout = 10 * time.Second
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
	// Without one, non-production servers fall back to a key file beside the DB.
	fallbackKey := secretsKeyFallback(cfg)
	crypto.SetKeySource(func() string {
		c := config.GetConfig()
		if c != nil && c.SecretsKey() != "" {
			return c.SecretsKey()
		}
		return fallbackKey
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

	artifactSvc := services.NewArtifactService(db)
	host := mcp.NewHost(artifactSvc)
	// Outcome validation chain is DefaultThenRPC (see mcp.ChainedOutcomeValidator).
	// NewHost installs DefaultOutcomeValidator; wire business RPC later via
	// host.SetRPCOutcomeValidator(...). Default checks always run first.
	// Expose read-only run history to the list_run_history / get_history_detail
	// MCP tools so agent nodes can recall past executions and human feedback.
	runSvc := services.NewRunService(db)
	host.SetHistoryProvider(runSvc)
	host.SetReviewPhaseSource(func(runID string) bool {
		nodeID := host.ActiveNode(runID)
		if !host.ActiveCaps(runID).ReviewEnabled() {
			return false
		}
		var conv models.ReactConversation
		if db.Select("done").Where("run_id = ? AND node_id = ?", runID, nodeID).
			Order("iteration desc, id desc").First(&conv).Error != nil {
			return false
		}
		return !conv.Done
	})
	// Shared ConfigHome .tgz registry for gateway config.bundleUrl inject
	// (startup.sh extracts before agent start). Served at /sandbox-inject/:id.
	injectStore := sandbox.NewBundleStore()
	// Grasp's in-sandbox runtime (scripts + backend + preview-inject), served
	// to new sandboxes at start and pushed to running ones (EnsureRuntime).
	runtimeBundle := sandbox.NewRuntimeBundle(cfg.SandboxRuntimeBundle())
	if _, _, err := runtimeBundle.Current(); err != nil {
		log.Warn().Err(err).Msg("sandbox runtime bundle unavailable; sandbox creation will fail until it exists")
	}

	blobStore, err := blob.NewFromConfig(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("blob store init failed")
	}

	workspaceSvc := services.NewWorkspaceService(db)
	auditSvc := services.NewAuditService(db)
	externalMcpSvc := services.NewExternalMcpService(db)
	mcpKeySvc := services.NewWorkspaceMcpApiKeyService(db)
	credentialSvc := services.NewCredentialService(db)
	sharedAgentSvc := services.NewSharedAgentService(services.DefaultSharedAgentRoot(cfg.Engine.ProfilesRoot))
	modelConnectionSvc := services.NewModelConnectionService(db)
	modelConfigurationSvc := services.NewModelConfigurationService(db)
	resolveModelBinding := func(backend string, binding *modelrouting.Binding) (modelrouting.Resolved, error) {
		if err := modelrouting.ValidateConfigurationReference(binding); err != nil {
			return modelrouting.Resolved{}, err
		}
		return modelConnectionSvc.Resolve(backend, *binding)
	}
	resolveModelConfiguration := func(binding *modelrouting.Binding) (string, error) {
		if err := modelrouting.ValidateConfigurationReference(binding); err != nil {
			return "", err
		}
		configuration, err := modelConfigurationSvc.Get(binding.ConfigurationID)
		if err != nil {
			return "", err
		}
		_, err = resolveModelBinding(configuration.Backend, binding)
		return configuration.Backend, err
	}
	sharedAgentSvc.SetBindingResolvers(resolveModelConfiguration, func(bindings *models.GitBindings) error {
		_, err := runtime.ResolveGitBindings(bindings, credentialSvc.ResolveGitValue)
		return err
	})
	// The catalog provides model choices; saved model configurations determine execution.
	openCodeCatalog := opencodecatalog.New(cfg.Sandbox.OpenCodeCatalogURL)
	sshKeyPath := sandbox.DataPlaneKeyPath(cfg.SecretsKeyFile())
	sandbox.SetDataPlaneKeyFile(sshKeyPath)
	log.Info().Str("path", sshKeyPath).Msg("data-plane ssh key file")
	sandboxToken := func(id string) string {
		id = strings.TrimSpace(id)
		if id == "" {
			return ""
		}
		var row models.Sandbox
		if err := db.Select("token").Where("name = ?", id).First(&row).Error; err != nil {
			return ""
		}
		return row.Token
	}
	provider := runtime.NewProvider(host, runtime.Options{
		SandboxImage:         cfg.Sandbox.Image,
		SandboxImages:        cfg.Sandbox.Images,
		GatewayURL:           cfg.Sandbox.GatewayURL,
		GatewayAPIKey:        cfg.Sandbox.GatewayAPIKey,
		Env:                  cfg.Sandbox.Env,
		ChatIdleTimeout:      cfg.ChatIdleTimeout(),
		NodeHardCap:          cfg.AgentNodeHardCap(),
		SandboxMaxAttempts:   cfg.Sandbox.MaxAttempts,
		SandboxRetryBackoff:  cfg.SandboxRetryBackoff(),
		SandboxCreateTimeout: cfg.SandboxCreateTimeout(),
		MCPEndpoint:          cfg.Server.MCPAdvertise,
		InjectStore:          injectStore,
		Runtime:              runtimeBundle,
		Blobs:                blobStore,
		SSHKeyFile:           sshKeyPath,
		DataPlanePassword:    sandboxToken,
		ProfilesRoot:         cfg.Engine.ProfilesRoot,
		SharedAgent: func() runtime.SharedAgentView {
			cfg := sharedAgentSvc.Get()
			mcp := make([]runtime.SharedMCPView, 0, len(cfg.MCP))
			for _, m := range cfg.MCP {
				mcp = append(mcp, runtime.SharedMCPView{
					Name: m.Name, URL: m.URL, Headers: m.Headers,
					Command: m.Command, Args: m.Args, Env: m.Env,
				})
			}
			return runtime.SharedAgentView{
				AcpBackend: cfg.AcpBackend, ModelBinding: cfg.ModelBinding, GitBindings: cfg.GitBindings,
				MCP: mcp,
				Env: cfg.Env,
				Layout: runtime.SharedLayoutView{
					ConfigRoot: cfg.Layout.ConfigRoot, WorkspaceDir: cfg.Layout.WorkspaceDir,
				},
				WorkDir: sharedAgentSvc.WorkDir(),
			}
		},
		CredentialKeys:       credentialSvc.CredentialEnvKeys,
		CredentialReferences: credentialSvc.ResolveReferences,
		ModelBinding:         resolveModelBinding,
		SelectedCredential:   credentialSvc.ResolveSelectedValue,
		GitCredential:        credentialSvc.ResolveGitValue,
		RunSandboxEnvForRun: func(runID string) []models.EnvEntry {
			var run models.Run
			if err := db.Select("sandbox_env").First(&run, "id = ?", runID).Error; err != nil {
				return nil
			}
			return run.SandboxEnv
		},
		PublicAdvertise:         cfg.Server.PublicAdvertise,
		WriteBackCodexLoginFile: credentialSvc.WriteBackCodexLoginCredential,
	})
	eng := engine.New(db, provider, host, artifactSvc, cfg.Engine.MaxConcurrentRuns)
	if err := eng.InitDialogueEpoch(); err != nil {
		log.Fatal().Err(err).Msg("init dialogue epoch")
	}
	eng.SetBlobStore(blobStore)
	eng.SetWorkspaceVarsLookup(func(workflowID string) []models.WorkspaceVariable {
		return workspaceSvc.VariablesForWorkflow(workflowID)
	})
	eng.SetAuditRecorder(func(rec services.AuditRecord) {
		auditSvc.Record(rec)
	})
	pageHub := pagebridge.NewHub()
	eng.SetPageHub(pageHub)
	host.SetPageBridge(&pagebridge.Router{Hub: pageHub, Turns: eng})
	host.SetLiveUpdater(eng)
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
		go eng.RetireVisitorLanesForTokenHashes(tokenHashes)
	})
	eng.SetShareRevoker(gateShareSvc)
	host.SetAuditHook(func(runID, nodeID, tool string, args map[string]any, resultText string, isError bool) {
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
	agentSvc.SetSharedAgent(sharedAgentSvc)
	agentSvc.SetModelConfigurationResolver(resolveModelConfiguration)
	agentSvc.SetGitBindingResolver(func(bindings *models.GitBindings) error {
		_, err := runtime.ResolveGitBindings(bindings, credentialSvc.ResolveGitValue)
		return err
	})
	agentSvc.SetModelBindingResolver(func(backend string, binding *modelrouting.Binding) error {
		_, err := resolveModelBinding(backend, binding)
		return err
	})
	eng.SetAgents(agentSvc)
	sbxGateway := sandbox.NewGatewayClient(cfg.Sandbox.GatewayURL, cfg.Sandbox.GatewayAPIKey)
	sbxMgr := sandbox.NewManager(sbxGateway, sandbox.ManagerOptions{
		Image:             cfg.Sandbox.Image,
		WorkspaceDir:      "/root/workspace",
		InstallHelpers:    true,
		InjectStore:       injectStore,
		InjectAdvertise:   cfg.Server.MCPAdvertise,
		CreateTimeout:     cfg.SandboxCreateTimeout(),
		Blobs:             blobStore,
		Runtime:           runtimeBundle,
		SSHKeyFile:        sshKeyPath,
		DataPlanePassword: sandboxToken,
	})
	log.Info().Str("gateway", cfg.Sandbox.GatewayURL).Msg("sandbox control plane: sandbox-gateway")
	sbxSvc := services.NewSandboxService(db, sbxMgr, agentSvc, host, services.SandboxOptions{
		ProfilesRoot:         cfg.Engine.ProfilesRoot,
		MCPEndpoint:          cfg.Server.MCPAdvertise,
		Env:                  cfg.Sandbox.Env,
		TTL:                  cfg.TestSandboxTTL(),
		RunTTL:               cfg.RunSandboxTTL(),
		MaxRetention:         cfg.RunSandboxMaxRetention(),
		Max:                  cfg.Sandbox.MaxTestSandboxes,
		MaxSolo:              cfg.Sandbox.MaxSoloSandboxes,
		SharedAgent:          sharedAgentSvc,
		CredentialReferences: credentialSvc.ResolveReferences,
		ModelBinding:         resolveModelBinding,
		SelectedCredential:   credentialSvc.ResolveSelectedValue,
		GitCredential:        credentialSvc.ResolveGitValue,
	})
	sbxSvc.SetCodexLoginWriteBack(credentialSvc.WriteBackCodexLoginCredential)
	// Let the exec provider record per-run node sandboxes in the same store so
	// they show up in the sandbox UI alongside interactive test sandboxes.
	if rr, ok := provider.(runtime.SandboxRegistrar); ok {
		rr.SetSandboxRegistry(sbxSvc)
	}
	// Reconcile DB ↔ gateway on boot and start the idle-TTL sweeper. The
	// gateway owns image lifecycle now, so there is no local image pre-pull.
	sbxSvc.ReconcileOnStartup(context.Background(), eng.AdoptedSandboxes())
	sweeperCtx, stopSweeper := context.WithCancel(context.Background())
	go sbxSvc.RunSweeper(sweeperCtx)

	// In-sandbox VNC preview: dials each preview-capable sandbox's CDP/websockify.
	browserSvc := browser.New(sbxMgr, browser.Config{
		MaxTabs:             cfg.Browser.MaxTabs,
		MaxTabsPerContainer: cfg.Browser.MaxTabsPerContainer,
		TabIdleTTL:          cfg.TabIdleTTL(),
		ContainerIdleTTL:    cfg.ContainerIdleTTL(),
		DesktopIdleTTL:      cfg.DesktopIdleTTL(),
	})
	browserSvc.Start()
	log.Info().Int("max_tabs", cfg.Browser.MaxTabs).Msg("in-sandbox vnc preview enabled")

	// Platform settings: DB override layer over the read-only config file for
	// runtime-tunable scheduling params. ApplyOnBoot re-applies persisted UI
	// values so they survive restarts.
	settingsSvc := services.NewSettingsService(db, eng, sbxSvc)
	settingsSvc.ApplyOnBoot()

	previewSvc := services.NewPreviewService(db, sbxMgr)
	previewSvc.SetAgents(agentSvc)
	host.SetConversationHistoryProvider(services.NewConversationHistoryService(db, func(runID string) (models.Sandbox, bool) {
		row, _, ok := previewSvc.SoloMCPBinding(runID)
		return row, ok
	}))
	wireArtifactMCPPersistence(host, db, runSvc, previewSvc)
	previewSvc.SetBrowser(browserSvc)
	previewSvc.SetSandboxLeases(sbxSvc)
	host.SetPreviewStore(previewSvc)
	host.SetPreviewSandboxOps(previewSvc)
	host.SetPreviewBaseURL(cfg.Server.PublicAdvertise)

	// Preview feedback: humans report problems from the preview UI (one-way).
	// The engine snapshots them into the preview_issues run variable at gate
	// resume so a downstream node consumes them via {{vars.preview_issues}}.
	issueSvc := services.NewIssueService(db)
	issueSvc.SetBlobStore(blobStore)
	eng.SetIssueService(issueSvc)

	requirementDraftSvc := services.NewRequirementDraftService(db)
	notificationSvc := services.NewNotificationService(db, runSvc)

	coord := shutdown.New(shutdownHTTPTimeout)
	authSvc := auth.NewService(db, config.GetConfig)

	pmSvc := services.NewPmService(db, agentSvc)
	pmSvc.SetBlobStore(blobStore)
	pmProgress := services.NewPmProgress(pmSvc, runSvc, artifactSvc)
	wfSvc := services.NewWorkflowService(db)
	wfSvc.SetAgents(agentSvc)
	pmMCP := pmmcp.NewHost(pmSvc, pmProgress, wfSvc, runSvc, artifactSvc, eng)
	pmMCP.SetAgents(agentSvc)
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
	sbxSvc.SetAgentSandboxDestroyHook(func(threadID, token string) {
		mcpWire.unregister(token)
		mcpWire.clearSandboxRef(threadID)
	})
	sbxSvc.SetTestSchedulerHooks(services.TestSchedulerHooks{
		Register: func(profile, runID, token string) {
			schedulerMCP.Restore(token, profile, runID, "test", false)
		},
		Unregister: schedulerMCP.Unregister,
	})

	cronSched := services.NewCronScheduler(db, pmSvc, sbxSvc, pmTurns, services.CronTokenHooks{
		Register:   mcpWire.registerCron,
		Unregister: mcpWire.unregister,
	})
	gateAutoSvc := services.NewGateAutoInvokeService(db, pmSvc, sbxSvc, pmTurns, services.CronTokenHooks{
		RegisterPM: mcpWire.registerPMPolicy,
		Register:   mcpWire.registerCron,
		Unregister: mcpWire.unregister,
	})
	eng.SetGateAutoInvoker(gateAutoEngineAdapter{svc: gateAutoSvc})
	runNotifySvc := services.NewRunNotifyService(db, nil, cfg.Server.PublicAdvertise)
	eng.SetRunNotifier(runNotifyEngineAdapter{svc: runNotifySvc})
	// External IM channels (QQ today; extensible). Each bot binds one Agent. Configs are DB-managed via the admin WebUI and hot-reloaded.
	// Memory/scheduler writes follow ChannelConfig session caps (default off).
	// pm-workflow-write is controlled by channel EnabledMcps — enable it only
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

	soloSvc := services.NewSoloService(db, sbxSvc, agentSvc, services.NewProjectService(db))
	soloSvc.SetBlobStore(blobStore)

	h := &handlers.Handlers{
		WF:                  wfSvc,
		Workspace:           workspaceSvc,
		Runs:                runSvc,
		Arts:                artifactSvc,
		APIKeys:             services.NewAPIKeyService(db),
		Agents:              agentSvc,
		SharedAgent:         sharedAgentSvc,
		Dash:                services.NewDashboardService(db, workspaceSvc),
		Sbx:                 sbxSvc,
		SbxChats:            services.NewSandboxChats(sbxSvc),
		Preview:             previewSvc,
		Issues:              issueSvc,
		RequirementDrafts:   requirementDraftSvc,
		Notifications:       notificationSvc,
		Eng:                 eng,
		MCP:                 host,
		Pm:                  pmSvc,
		PmProgress:          pmProgress,
		PmTurns:             pmTurns,
		Solo:                soloSvc,
		PMMCP:               pmMCP,
		MemoryMCP:           memoryMCP,
		ContextMCP:          contextMCP,
		SchedulerMCP:        schedulerMCP,
		Settings:            settingsSvc,
		Shutdown:            coord,
		Auth:                authSvc,
		Channels:            channelSvc,
		RunNotify:           runNotifySvc,
		Browser:             browserSvc,
		Audit:               auditSvc,
		ExternalMcp:         externalMcpSvc,
		McpKeys:             mcpKeySvc,
		Credentials:         credentialSvc,
		OpenCodeCatalog:     openCodeCatalog,
		ModelConnections:    modelConnectionSvc,
		ModelConfigurations: modelConfigurationSvc,
		GateShare:           gateShareSvc,
		GateShareNonces:     gateshare.NewNonceStore(db),
		GateShareTickets:    gateShareTickets,
		Embed:               embedStore,
		PageBridge:          pageHub,
		GateShareSessions:   gateShareSessions,
		GateShareLimiter:    gateshare.NewIPLimiter(),
		PublicAdvertise:     cfg.Server.PublicAdvertise,
		InjectBundles:       injectStore,
		Blobs:               blobStore,
		Onboarding:          services.NewOnboardingService(agentSvc, sharedAgentSvc, wfSvc, credentialSvc),
		Team:                services.NewTeamService(workspaceSvc, agentSvc, pmSvc, sbxSvc),
	}
	var maintenancePrepared atomic.Bool
	var maintenanceMu sync.Mutex
	h.Maintenance = func(ctx context.Context, begin bool) (map[string]any, error) {
		maintenanceMu.Lock()
		defer maintenanceMu.Unlock()
		if begin {
			coord.BeginDraining()
			eng.Halt()
			cronSched.Pause()
			gateAutoSvc.BeginMaintenance()
			channelMgr.BeginMaintenance()
		}
		state := map[string]any{"draining": coord.IsDraining(), "workflowDrivers": eng.ActiveDrivers(), "pmThreads": pmTurns.ActiveThreads(), "cronExecutions": cronSched.ActiveExecutions(), "automaticApprovals": gateAutoSvc.PendingMaintenance(), "channelPending": channelMgr.PendingMaintenance(), "soloTurns": soloSvc.ActiveCount()}
		safe := coord.IsDraining() && eng.ActiveDrivers() == 0 && pmTurns.ActiveThreads() == 0 && soloSvc.ActiveCount() == 0 && cronSched.ActiveExecutions() == 0 && gateAutoSvc.PendingMaintenance() == 0 && channelMgr.PendingMaintenance() == 0
		state["safeToStop"] = safe && maintenancePrepared.Load()
		if begin && safe {
			if err := sbxSvc.PrepareWorkspaceMaintenance(ctx); err != nil {
				state["safeToStop"] = false
				return state, fmt.Errorf("archive interactive sandboxes: %w", err)
			}
			maintenancePrepared.Store(true)
			state["safeToStop"] = true
		}
		if begin && !safe {
			return state, fmt.Errorf("maintenance is draining; poll status, then POST again to archive idle interactive sandboxes")
		}
		return state, nil
	}
	h.Team.Credentials = credentialSvc
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
	// Agent MCP clients hold a tool-list SSE stream open for the whole run.
	srv.RegisterOnShutdown(host.CloseToolStreams)

	log.Info().Str("port", port).
		Str("config_path", cfgPath).Msg("approving server starting")

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server exited")
		}
	}()
	// Every provider hook is wired now; continue agent nodes whose sandboxes
	// survived the previous process.
	eng.ResumeInterrupted()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	sig := <-quit
	log.Info().Str("signal", sig.String()).Msg("signal received")

	coord.BeginDraining()
	log.Info().Msg("drain started")

	// Stop admitting new work. Queued runs stay queued and running drivers
	// requeue at their next node boundary, so the next process picks both up.
	eng.Halt()

	// Sandboxes and ACP connections are left alone on purpose: closing an ACP
	// client makes the in-flight turn fail and discard its sandbox, while the
	// next process reattaches running agent sandboxes and resumes their nodes.
	stopSweeper()
	channelMgr.StopAll()
	browserSvc.Stop()

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownHTTPTimeout)
	defer cancelShutdown()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn().Err(err).Msg("http server shutdown")
	}

	// Exit without unwinding: node goroutines must not get a chance to record
	// failures for work the next process is about to resume.
	log.Info().Msg("exit")
	os.Exit(0)
}

// secretsKeyFallback loads or creates the auto-generated secrets key when none
// is configured; empty when a key is configured or the mode forbids a fallback.
func secretsKeyFallback(cfg *config.Config) string {
	if cfg == nil || cfg.SecretsKey() != "" {
		return ""
	}
	path := cfg.SecretsKeyFile()
	if path == "" {
		return ""
	}
	key, err := crypto.LoadOrCreateKeyFile(path)
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("secrets key file unavailable; credentials cannot be saved")
		return ""
	}
	log.Warn().Str("path", path).Msg("no secrets key configured; using the generated key file (back it up with the database)")
	return key
}
