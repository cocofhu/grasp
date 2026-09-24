// Package handlers implements the REST + WS API binding services and the
// FSM engine to HTTP. Response shapes mirror the frontend types so the Vue
// app maps responses to its view models with minimal transformation.
package handlers

import (
	"sync"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/contextmcp"
	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/memorymcp"
	"github.com/cocofhu/grasp/internal/opencodecatalog"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/schedulermcp"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/cocofhu/grasp/internal/shutdown"
)

// Handlers bundles dependencies for route handlers.
type Handlers struct {
	WF                *services.WorkflowService
	Projects          *services.ProjectService
	Runs              *services.RunService
	Arts              *services.ArtifactService
	APIKeys           *services.APIKeyService
	Agents            *services.AgentService
	SharedAgent       *services.SharedAgentService
	Org               *services.OrgService
	Dash              *services.DashboardService
	Sbx               *services.SandboxService
	Eng               *engine.Engine
	MCP               *mcp.Host
	Pm                *services.PmService
	PmProgress        *services.PmProgress
	PmTurns           *services.PmTurnRunner
	PMMCP             *pmmcp.Host
	MemoryMCP         *memorymcp.Host
	ContextMCP        *contextmcp.Host
	SchedulerMCP      *schedulermcp.Host
	Preview           *services.PreviewService
	Issues            *services.IssueService
	RequirementDrafts *services.RequirementDraftService
	Notifications     *services.NotificationService
	Settings          *services.SettingsService
	Shutdown          *shutdown.Coordinator
	Auth              *auth.Service
	PlatformRules     *services.PlatformRuleService
	Channels          *services.ChannelConfigService
	RunNotify         *services.RunNotifyService
	Browser           *browser.Service
	Audit             *services.ProjectAuditService
	ExternalMcp       *services.ProjectExternalMcpService
	ProjectMcpKeys    *services.ProjectMcpApiKeyService
	Onboarding        *services.OnboardingService
	GateShare         *gateshare.Service
	GateShareNonces   *gateshare.NonceStore
	GateShareTickets  *gateshare.TicketStore
	GateShareSessions *gateshare.PreviewSessionHub
	GateShareLimiter  *gateshare.IPLimiter
	// Embed backs the preview-page chat drawer (tickets + bearer sessions).
	Embed *embed.Store
	// PublicAdvertise is the browser-facing base for QQ/preview deep links.
	// Gate/review share URLs mint from Request.Host instead. Public CSRF
	// compares Origin/Referer to this request's Host (never client
	// X-Forwarded-Host; advertise host is not used for CSRF).
	PublicAdvertise string
	Team            *services.TeamService
	// CanViewProjectAudit optionally overrides the default audit ACL
	// (is_admin OR authenticated user who can UpdateProject). Tests use this
	// to simulate a read-only member denial while production keeps the hook nil.
	CanViewProjectAudit func(username, projectID string) bool
	// InjectBundles serves ConfigHome .tgz for gateway SANDBOX_INJECT (no session auth).
	InjectBundles *sandbox.BundleStore
	// Blobs serves externalized attachment bytes (GET /api/blobs/:id).
	Blobs blob.Store
	// OpenCodeCatalog backs the OpenCode provider / model pickers. Nil disables
	// the endpoints, leaving the UI on hand-typed ids.
	OpenCodeCatalog *opencodecatalog.Store
	doctorMu        sync.Mutex
	doctorSessions  map[string]doctorArtifactSession
}
