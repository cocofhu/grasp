package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// SandboxPurposeSolo marks resident Solo session sandboxes. They have no idle
// TTL and are only destroyed when the user closes the session.
const SandboxPurposeSolo = "solo"

// ErrSoloSandboxLimit is returned when the resident Solo sandbox cap is reached.
type ErrSoloSandboxLimit struct{ Max int }

func (e ErrSoloSandboxLimit) Error() string {
	return fmt.Sprintf("已达到 Solo 沙箱上限(%d),请先关闭不用的 Solo 会话", e.Max)
}

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
	OnEnv                  func(sources map[string]string)
	OnReady                func(sandboxID uint)
	OnFail                 func(sandboxID uint, err error)
}

// MaxSoloSandboxes returns the resident Solo sandbox cap.
func (s *SandboxService) MaxSoloSandboxes() int { return int(s.maxSolo.Load()) }

// SetMaxSoloSandboxes updates the Solo cap at runtime (non-positive ignored).
func (s *SandboxService) SetMaxSoloSandboxes(n int) {
	if n > 0 {
		s.maxSolo.Store(int64(n))
	}
}

// SetSoloSandboxDestroyHook registers a callback for purpose=solo teardown,
// so sessions whose sandbox was removed elsewhere stop showing as running.
func (s *SandboxService) SetSoloSandboxDestroyHook(fn func(sandboxID uint)) {
	s.mu.Lock()
	s.soloOnDestroy = fn
	s.mu.Unlock()
}

func (s *SandboxService) invokeSoloDestroyHook(id uint) {
	s.mu.Lock()
	fn := s.soloOnDestroy
	s.mu.Unlock()
	if fn != nil {
		fn(id)
	}
}

func (s *SandboxService) activeSoloSandboxCount() int {
	var n int64
	s.db.Model(&models.Sandbox{}).
		Where("status IN ? AND purpose = ?", []string{"running", "creating", "pulling"}, SandboxPurposeSolo).
		Count(&n)
	return int(n)
}

// SoloRepoSpecs converts a project into the sandbox clone list.
func SoloRepoSpecs(p *models.Project, branch string) []sandbox.RepoSpec {
	if p == nil || strings.TrimSpace(p.RepoURL) == "" {
		return nil
	}
	name := strings.TrimSpace(p.RepoDirName)
	if name == "" {
		name = sandbox.RepoNameFromURL(p.RepoURL)
	}
	if name == "" {
		name = "repo"
	}
	b := strings.TrimSpace(branch)
	if b == "" {
		b = strings.TrimSpace(p.DefaultBranch)
	}
	return []sandbox.RepoSpec{{Name: name, URL: strings.TrimSpace(p.RepoURL), Branch: b}}
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
	modelFingerprint, gitFingerprint, err := s.currentBindingFingerprints(agent)
	if err != nil {
		return nil, err
	}
	if maxN := s.MaxSoloSandboxes(); s.activeSoloSandboxCount() >= maxN {
		return nil, ErrSoloSandboxLimit{Max: maxN}
	}

	snapshots := opts.Repos
	if len(snapshots) == 0 && opts.Project != nil {
		snapshots = []models.SessionRepo{sessionRepoFromProject(opts.Project, opts.Branch, 0)}
	}
	repos := SoloRepoSpecsList(snapshots)
	runID := "solo-" + sanitizeID(sessionID) + "-" + randID()
	token := uuid.NewString()
	if s.host != nil {
		token = s.host.RegisterRun(runID)
		if agent.Capabilities != nil {
			s.host.SetActiveNode(runID, "solo", agent.Capabilities.Clone())
		}
	}
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
		RunID: runID, Token: token, ThreadID: sessionID, RepoURL: firstTestRepoURL(repos),
	}
	if err := s.db.Create(row).Error; err != nil {
		if s.host != nil {
			s.host.UnregisterRun(runID)
		}
		return nil, err
	}
	extra := agentContainerExtra{Repos: repos, Resident: true, LaunchKind: "solo", OnEnv: opts.OnEnv, InitialChatModel: strings.TrimSpace(opts.InitialModelID), InitialChatReasoningEffort: &opts.ReasoningEffort}
	id := row.ID
	if opts.OnReady != nil {
		extra.OnReady = func() { opts.OnReady(id) }
	}
	if opts.OnFail != nil {
		extra.OnFail = func(err error) { opts.OnFail(id, err) }
	}
	log.Info().Str("name", name).Str("profile", profile).Str("session", sessionID).
		Int("repos", len(repos)).Uint("id", id).Msg("solo sandbox creating")
	go s.startAgentContainer(id, name, profile, sessionID, runID, token, platformSpecs, agent, extra)
	return row, nil
}
