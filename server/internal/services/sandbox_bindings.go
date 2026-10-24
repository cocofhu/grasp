package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

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

func gitBindingsFingerprint(agent Agent, credentials []sandbox.GitCredential) string {
	if agent.GitBindings == nil {
		return ""
	}
	return sandbox.SpecFingerprint(sandbox.Spec{GitBindingsConfigured: true, GitCredentials: credentials})
}

func (s *SandboxService) currentBindingFingerprints(agent Agent) (model, git string, err error) {
	credentials, err := runtime.ResolveGitBindings(agent.GitBindings, s.selectedCredential)
	if err != nil {
		return "", "", err
	}
	env := map[string]string{}
	legacyModel := ""
	if agent.AcpBackend == "opencode" && agent.ModelBinding == nil {
		legacyModel, err = runtime.ApplyLegacyOpenCodeCredential(env, agent.OpenCodeCredentialID, s.openCodeCredential)
		if err != nil {
			return "", "", err
		}
	}
	if err := runtime.ApplyModelBinding(env, agent.AcpBackend, agent.ModelBinding, s.modelBinding); err != nil {
		return "", "", err
	}
	model = modelBindingFingerprint(agent, env)
	if legacyModel != "" {
		model = legacyModel
	}
	return model, gitBindingsFingerprint(agent, credentials), nil
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
		return fmt.Errorf("Git 授权已变更或尚未同步，请重新打开沙箱")
	}
	return nil
}
