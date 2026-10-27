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

// Fingerprints retain no secret material. A sandbox records the model route and
// Git authorization it was created with. Later changes do not affect that sandbox.
func modelBindingFingerprint(agent Agent, env map[string]string) string {
	if agent.ModelBinding == nil {
		return ""
	}
	data, _ := json.Marshal(struct {
		Backend    string                `json:"backend"`
		Binding    *modelrouting.Binding `json:"binding"`
		Connection string                `json:"connection"`
		Credential string                `json:"credential"`
		Model      string                `json:"model"`
		Secret     string                `json:"secret"`
	}{agent.AcpBackend, agent.ModelBinding, env[runtime.EnvModelConnection], env[runtime.EnvModelCredentialID], env[runtime.EnvACPBridgeModel],
		env[runtime.EnvModelAPIKey] + env[runtime.CredentialEnvKeyForBackend(agent.AcpBackend)]})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func gitBindingsFingerprint(credentials []sandbox.GitCredential) string {
	return sandbox.SpecFingerprint(sandbox.Spec{GitBindingsConfigured: true, GitCredentials: credentials})
}

func (s *SandboxService) currentModelFingerprint(agent Agent) (string, error) {
	if agent.ModelBinding == nil || agent.ModelBinding.Kind != "configuration" || strings.TrimSpace(agent.ModelBinding.ConfigurationID) == "" {
		return "", fmt.Errorf("请先为 Agent 选择模型配置")
	}
	env := map[string]string{}
	if err := runtime.ApplyModelBinding(env, agent.AcpBackend, agent.ModelBinding, s.modelBinding); err != nil {
		return "", err
	}
	return modelBindingFingerprint(agent, env), nil
}

func (s *SandboxService) currentGitFingerprint(agent Agent) (string, error) {
	credentials, err := runtime.ResolveGitBindings(agent.GitBindings, s.gitCredential)
	if err != nil {
		return "", err
	}
	return gitBindingsFingerprint(credentials), nil
}

func (s *SandboxService) currentBindingFingerprints(agent Agent) (model, git string, err error) {
	model, err = s.currentModelFingerprint(agent)
	if err != nil {
		return "", "", err
	}
	git, err = s.currentGitFingerprint(agent)
	if err != nil {
		return "", "", err
	}
	return model, git, nil
}

// appliedModelFingerprint is the route captured when the sandbox was created.
// Reconnects keep it; they do not re-read the Agent's current configuration.
func appliedModelFingerprint(row *models.Sandbox, live *liveSandbox) string {
	stored := ""
	if row != nil {
		stored = row.ModelBindingFingerprint
	}
	liveValue := ""
	if live != nil {
		liveValue = live.modelBindingFingerprint
	}
	return appliedFingerprint(stored, liveValue)
}

// appliedGitFingerprint is the Git authorization captured when the sandbox was
// created. Reconnects keep it and do not recheck the current selection.
func appliedGitFingerprint(row *models.Sandbox, live *liveSandbox) string {
	stored := ""
	if row != nil {
		stored = row.GitBindingsFingerprint
	}
	liveValue := ""
	if live != nil {
		liveValue = live.gitBindingsFingerprint
	}
	return appliedFingerprint(stored, liveValue)
}

func appliedFingerprint(stored, live string) string {
	if stored != "" {
		return stored
	}
	return live
}
