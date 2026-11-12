package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

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

func (s *SandboxService) currentBindingFingerprints(agent Agent, repos []models.SessionRepo) (model, git string, err error) {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" || strings.TrimSpace(agent.ModelBinding.ConfigurationID) == "" {
		return "", "", fmt.Errorf("请先为 Agent 选择模型配置")
	}
	_, repoCreds := s.repoGit(repos)
	credentials, err := runtime.ResolveRepoCredentials(repoCreds, s.gitCredential)
	if err != nil {
		return "", "", err
	}
	env := map[string]string{}
	if err := runtime.ApplyModelBinding(env, agent.AcpBackend, agent.ModelBinding, s.modelBinding); err != nil {
		return "", "", err
	}
	return modelBindingFingerprint(agent, env), gitBindingsFingerprint(credentials), nil
}

func checkAppliedBindings(model, git string, row *models.Sandbox, live *liveSandbox) error {
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
	if model != appliedModel {
		return fmt.Errorf("模型或凭据已变更或尚未同步，请重新打开沙箱")
	}
	if git != appliedGit {
		return fmt.Errorf("仓库凭据已变更或尚未同步，请重新打开沙箱")
	}
	return nil
}
