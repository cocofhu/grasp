package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// SandboxPurposeSolo records ownership; lifecycle policy is shared by all purposes.
const SandboxPurposeSolo = "solo"

// SoloSandboxOpts configures OpenSoloSandbox.
type SoloSandboxOpts struct {
	Profile   string
	SessionID string
	// Repos is the frozen snapshot, in clone order. Empty starts an empty workspace.
	Repos []models.SessionRepo
	// Project and Branch remain for callers that still open a single repository.
	Project                *models.Project
	Branch                 string
	InitialModelID         string
	InitialConfigurationID string
	ReasoningEffort        string
	FastMode               bool
	OnEnv                  func(sources map[string]string)
	OnReady                func(sandboxID uint)
	OnFail                 func(sandboxID uint, err error)
	// Restore replaces the repository clone with a stopped workspace archive.
	Restore func(ctx context.Context, sb *sandbox.Sandbox) error
}

// SetSoloSandboxDestroyHook registers a callback for purpose=solo teardown,
// so sessions whose sandbox was removed elsewhere stop showing as running.
func (s *SandboxService) SetSoloSandboxDestroyHook(fn func(sandboxID uint)) {
	s.mu.Lock()
	s.soloOnDestroy = fn
	s.mu.Unlock()
}

func (s *SandboxService) SetSoloSandboxRecoveredHook(fn func(sandboxID uint)) {
	s.mu.Lock()
	s.soloOnRecovered = fn
	s.mu.Unlock()
}

// SetSoloSandboxPausedHook registers a callback for running residents the
// provider paused (exited/stopped) without a user stop.
func (s *SandboxService) SetSoloSandboxPausedHook(fn func(sandboxID uint)) {
	s.mu.Lock()
	s.soloOnPaused = fn
	s.mu.Unlock()
}

func (s *SandboxService) invokeSoloPausedHook(id uint) {
	s.mu.Lock()
	fn := s.soloOnPaused
	s.mu.Unlock()
	if fn != nil {
		fn(id)
	}
}

func (s *SandboxService) invokeSoloDestroyHook(id uint) {
	s.mu.Lock()
	fn := s.soloOnDestroy
	s.mu.Unlock()
	if fn != nil {
		fn(id)
	}
}

// OpenSoloSandbox creates a resident sandbox for a Solo session. It returns
// the "creating" row immediately; the container starts in the background and
// OnReady / OnFail report the outcome.
func (s *SandboxService) OpenSoloSandbox(ctx context.Context, opts SoloSandboxOpts) (*models.Sandbox, error) {
	profile := strings.TrimSpace(opts.Profile)
	sessionID := strings.TrimSpace(opts.SessionID)
	if profile == "" || sessionID == "" {
		return nil, fmt.Errorf("profile and sessionID are required")
	}
	agent, ok := s.skills.Get(profile)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", profile)
	}
	agent = s.effectiveAgent(agent)
	if selected := strings.TrimSpace(opts.InitialConfigurationID); selected != "" {
		var err error
		agent, err = selectSoloConfiguration(s.db, agent, selected)
		if err != nil {
			return nil, err
		}
	} else if agent.ModelBinding != nil {
		agent.ModelBinding = &modelrouting.Binding{Kind: "configuration", ConfigurationID: agent.ModelBinding.ConfigurationID}
	}
	snapshots := opts.Repos
	if len(snapshots) == 0 && opts.Project != nil {
		snapshots = []models.SessionRepo{SessionRepoFromProject(opts.Project, opts.Branch, 0)}
	}
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(agent, snapshots)
	if err != nil {
		return nil, err
	}

	runID := models.SoloArtifactRunPrefix + sanitizeID(sessionID) + "-" + randID()
	token := mcp.NewToken()
	platformSpecs := []sandbox.MCPServerSpec{}
	if base := strings.TrimRight(config.ResolveMCPAdvertise(s.mcpEndpoint), "/"); base != "" {
		platformSpecs = append(platformSpecs, sandbox.MCPServerSpec{
			Name:    ArtifactStoreMCP,
			URL:     base + "/mcp/runs/" + url.PathEscape(runID),
			Headers: map[string]string{"Authorization": "Bearer " + token},
		})
	}
	name := sandbox.NewContainerName()
	row := &models.Sandbox{
		ModelBindingFingerprint: modelFingerprint, GitBindingsFingerprint: gitFingerprint,
		Name: name, Profile: profile, Purpose: SandboxPurposeSolo, Status: "creating",
		RunID: runID, Token: token, ThreadID: sessionID, RepoURL: firstRepoURL(snapshots), Repos: snapshots,
	}
	if err := s.createSandboxRow(row); err != nil {
		return nil, err
	}
	if s.host != nil {
		s.host.SetActiveNode(runID, "solo", agent.Capabilities.Clone())
	}
	extra := agentContainerExtra{Repos: snapshots, Resident: true, LaunchKind: "solo", OnEnv: opts.OnEnv, InitialChatModel: strings.TrimSpace(opts.InitialModelID), InitialChatReasoningEffort: &opts.ReasoningEffort, InitialChatFastMode: &opts.FastMode, Restore: opts.Restore}
	id := row.ID
	if opts.OnReady != nil {
		extra.OnReady = func() { opts.OnReady(id) }
	}
	if opts.OnFail != nil {
		extra.OnFail = func(err error) { opts.OnFail(id, err) }
	}
	log.Info().Str("name", name).Str("profile", profile).Str("session", sessionID).
		Int("repos", len(snapshots)).Uint("id", id).Msg("solo sandbox creating")
	go s.startAgentContainer(id, name, profile, sessionID, runID, token, platformSpecs, agent, extra)
	return row, nil
}
