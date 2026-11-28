package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// Fingerprints retain no secret material. They let long-lived sessions refuse
// another turn when the selected authorization or model route has changed.
func modelBindingFingerprint(agent Agent, env map[string]string) string {
	if agent.ModelBinding == nil {
		return ""
	}
	secret := env[runtime.EnvModelAPIKey] + env[runtime.CredentialEnvKeyForBackend(agent.AcpBackend)]
	if runtime.NormalizeBackend(agent.AcpBackend) == runtime.BackendCodex && env[runtime.EnvModelConnection] == "" && env[runtime.EnvModelCredentialID] != "" {
		// The hub aligns this credential's login before every turn, including
		// platform renewals and user re-logins. Its rotating tokens are not a
		// route change. Resolution still rejects revoked/cleared credentials,
		// and the credential ID, model and configuration remain fingerprinted.
		secret = ""
	}
	data, _ := json.Marshal(struct {
		Backend    string                `json:"backend"`
		Binding    *modelrouting.Binding `json:"binding"`
		Connection string                `json:"connection"`
		Credential string                `json:"credential"`
		Model      string                `json:"model"`
		Catalog    string                `json:"catalog"`
		Secret     string                `json:"secret"`
	}{agent.AcpBackend, agent.ModelBinding, env[runtime.EnvModelConnection], env[runtime.EnvModelCredentialID], env[runtime.EnvACPBridgeModel],
		env[runtime.EnvConfiguredModels],
		secret})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func gitBindingsFingerprint(credentials []sandbox.GitCredential) string {
	return sandbox.SpecFingerprint(sandbox.Spec{GitCredentials: credentials})
}

// repoGit is the clone list of repos with each repository's current commit
// author and credential.
func (s *SandboxService) repoGit(repos []models.SessionRepo) ([]sandbox.RepoSpec, []runtime.RepoCredential) {
	return runtime.CloneRepos(repos, nil, s.project)
}

type sandboxBindings struct {
	model, git, legacyModel string
	capabilities            map[string]string
}

func (s *SandboxService) currentBindingFingerprints(agent Agent, repos []models.SessionRepo) (model, git string, err error) {
	bindings, err := s.currentSandboxBindings(agent, repos)
	return bindings.model, bindings.git, err
}

func (s *SandboxService) currentSandboxBindings(agent Agent, repos []models.SessionRepo) (sandboxBindings, error) {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" || strings.TrimSpace(agent.ModelBinding.ConfigurationID) == "" {
		return sandboxBindings{}, fmt.Errorf("请先为 Agent 选择模型配置")
	}
	_, repoCreds := s.repoGit(repos)
	credentials, err := runtime.ResolveRepoCredentials(repoCreds, s.gitCredential)
	if err != nil {
		return sandboxBindings{}, err
	}
	env := map[string]string{}
	if err := runtime.ApplyModelBinding(env, agent.AcpBackend, agent.ModelBinding, s.modelBinding); err != nil {
		return sandboxBindings{}, err
	}
	bindings := sandboxBindings{model: modelBindingFingerprint(agent, env), git: gitBindingsFingerprint(credentials)}
	if legacy := runtime.LegacyOpenAIModelBindingEnv(env); legacy != nil {
		bindings.legacyModel = modelBindingFingerprint(agent, legacy)
		bindings.capabilities = map[string]string{
			runtime.EnvModelConnection:  env[runtime.EnvModelConnection],
			runtime.EnvConfiguredModels: env[runtime.EnvConfiguredModels],
		}
	}
	return bindings, nil
}

func checkAppliedBindings(bindings sandboxBindings, row *models.Sandbox, live *liveSandbox) (refresh bool, err error) {
	appliedModel, appliedGit := "", ""
	if live != nil {
		appliedModel, appliedGit = live.modelBindingFingerprint, live.gitBindingsFingerprint
	}
	if row != nil {
		if row.ModelBindingFingerprint != "" {
			appliedModel = row.ModelBindingFingerprint
		}
		if row.GitBindingsFingerprint != "" {
			appliedGit = row.GitBindingsFingerprint
		}
	}
	refresh = bindings.model != appliedModel && bindings.legacyModel != "" && bindings.legacyModel == appliedModel
	if bindings.model != appliedModel && !refresh {
		return false, fmt.Errorf("模型或凭据已变更或尚未同步，请重新打开沙箱")
	}
	if bindings.git != appliedGit {
		return false, fmt.Errorf("仓库凭据已变更或尚未同步，请重新打开沙箱")
	}
	return refresh, nil
}

// The caller holds the sandbox's recovery lock. Update only the two derived
// environment values, preserving the workspace, native sessions and grants.
func (s *SandboxService) refreshSandboxModelCapabilities(ctx context.Context, row *models.Sandbox, sb *sandbox.Sandbox, live *liveSandbox, bindings sandboxBindings) error {
	if live != nil {
		s.mu.Lock()
		if live.busy || live.switching {
			s.mu.Unlock()
			return fmt.Errorf("sandbox busy")
		}
		live.switching = true
		acp := live.acp
		s.mu.Unlock()
		defer func() { s.mu.Lock(); live.switching = false; s.mu.Unlock() }()
		if acp != nil {
			acp.Close()
		}
	}
	if err := s.mgr.EnsureRuntime(ctx, sb); err != nil {
		return fmt.Errorf("update runtime before refreshing model capabilities: %w", err)
	}
	if err := s.mgr.RefreshModelCapabilities(ctx, sb, bindings.capabilities); err != nil {
		return fmt.Errorf("refresh sandbox model capabilities: %w", err)
	}
	if err := sandbox.WaitForACPReady(ctx, sb.Host, sb.Port, sb.Password, 120*time.Second); err != nil {
		return fmt.Errorf("sandbox runtime after refreshing model capabilities: %w", err)
	}
	res := s.db.Model(&models.Sandbox{}).
		Where("id = ? AND status = ? AND model_binding_fingerprint = ? AND git_bindings_fingerprint = ?",
			row.ID, "running", row.ModelBindingFingerprint, row.GitBindingsFingerprint).
		Update("model_binding_fingerprint", bindings.model)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("sandbox binding changed during capability refresh")
	}
	row.ModelBindingFingerprint = bindings.model
	if live != nil {
		s.mu.Lock()
		live.modelBindingFingerprint = bindings.model
		s.mu.Unlock()
	}
	return nil
}
