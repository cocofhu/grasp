package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

var authorizationCredentialRef = regexp.MustCompile(`\$\{credential:([^{}]+)\}`)

// currentAuthorizationFingerprint resolves the complete current grant before
// provider recovery. Its only durable output is a hash: no plaintext credential
// enters SQL, a receipt, a log or an error. This differs from a model route
// fingerprint: rotating Codex logins and non-model grants must change it too.
// Call outside SQL transactions, and re-resolve before final publication.
func (s *SandboxService) currentAuthorizationFingerprint(ctx context.Context, agent Agent) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s.db != nil {
		if _, inside := s.db.Statement.ConnPool.(gorm.TxCommitter); inside {
			return "", fmt.Errorf("authorization resolution requires a transaction-free handle")
		}
	}
	if modelrouting.ValidateConfigurationReference(agent.ModelBinding) != nil {
		return "", fmt.Errorf("请先选择有效的模型配置")
	}
	if s.modelBinding == nil {
		return "", fmt.Errorf("当前模型授权不可用")
	}
	resolved, err := s.modelBinding(agent.AcpBackend, agent.ModelBinding)
	if err != nil || strings.TrimSpace(resolved.Secret) == "" {
		return "", fmt.Errorf("当前模型授权不可用")
	}
	git, err := runtime.ResolveGitBindings(agent.GitBindings, s.gitCredential)
	if err != nil {
		return "", fmt.Errorf("当前 Git 授权不可用")
	}
	// The selected resolvers enforce purpose/domain and current secret state.
	// Metadata revisions additionally distinguish equal-value rotations and
	// changes to the scope of a still-selected credential.
	dependencies := map[string]models.Credential{}
	remember := func(id string) error {
		id = strings.TrimSpace(id)
		if id == "" {
			return fmt.Errorf("所选凭据缺少身份")
		}
		if _, ok := dependencies[id]; ok {
			return nil
		}
		var row models.Credential
		if s.db == nil {
			return fmt.Errorf("当前授权元数据不可用")
		}
		if err := s.db.WithContext(ctx).Select("id", "purpose", "issuer", "tool_id", "secret_kind", "host", "secret_revision", "type", "provider", "env_key", "target", "metadata", "enabled", "revoked_at").
			Where("id = ? AND enabled = ? AND revoked_at IS NULL", id, true).First(&row).Error; err != nil {
			return fmt.Errorf("所选凭据已失效或当前授权元数据不可用")
		}
		dependencies[id] = row
		return nil
	}
	if err := remember(resolved.CredentialID); err != nil {
		return "", err
	}
	if agent.GitBindings != nil {
		for _, item := range agent.GitBindings.Items {
			if err := remember(item.CredentialID); err != nil {
				return "", err
			}
		}
	}
	selected := map[string]string{}
	for key, id := range agent.CredentialBindings {
		if id == nil {
			continue // An explicit clear remains part of agent's canonical hash.
		}
		key = strings.TrimSpace(key)
		if key == "" || envauth.IsSecretEnvKey(key) || envauth.IsPlatformReservedEnvKey(key) || runtime.IsGitAuthorizationEnvKey(key) || s.selectedCredential == nil {
			return "", fmt.Errorf("额外凭据绑定不可用")
		}
		actual, value, ok := s.selectedCredential(strings.TrimSpace(*id))
		if !ok || actual != key || strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("额外凭据已失效或类型不匹配")
		}
		if err := remember(*id); err != nil {
			return "", err
		}
		selected[key] = value
	}
	// Credential references are permitted only in explicit MCP templates.
	// A missing reference must not survive as an apparently valid header.
	templates, err := json.Marshal(agent.MCP)
	if err != nil {
		return "", fmt.Errorf("MCP 授权模板无效")
	}
	text := string(templates)
	mcpRefs := authorizationCredentialRef.FindAllStringSubmatch(text, -1)
	if strings.Contains(authorizationCredentialRef.ReplaceAllString(text, ""), "${credential:") {
		return "", fmt.Errorf("MCP 凭据引用无效")
	}
	references := map[string]string{}
	if len(mcpRefs) > 0 {
		if s.credentialReferences == nil {
			return "", fmt.Errorf("MCP 凭据解析不可用")
		}
		available := s.credentialReferences()
		for _, match := range mcpRefs {
			id := strings.TrimSpace(match[1])
			value, ok := available["credential:"+id]
			if !ok || strings.TrimSpace(value) == "" {
				return "", fmt.Errorf("MCP 所选凭据已失效或不可用")
			}
			if err := remember(id); err != nil {
				return "", err
			}
			references[id] = value
		}
	}
	// Ignore display-only icon changes and normalize file ordering; capabilities,
	// effective layout, templates, ordinary env and explicit clear choices stay
	// in the grant. Hashes never serve as a substitute for the resolvers above.
	agent.Icon = nil
	agent.Files = slices.Clone(agent.Files)
	slices.SortFunc(agent.Files, func(a, b AgentFile) int { return strings.Compare(a.Path, b.Path) })
	platformEnv := envauth.StripSecretEnvKeys(s.env)
	data, err := json.Marshal(struct {
		Version      int
		Agent        Agent
		Model        modelrouting.Resolved
		ModelSecret  string
		Git          []sandbox.GitCredential
		Dependencies map[string]models.Credential
		Selected     map[string]string
		References   map[string]string
		PlatformEnv  map[string]string
		MCPEndpoint  string
	}{1, agent, resolved, resolved.Secret, git, dependencies, selected, references, platformEnv, s.mcpEndpoint})
	if err != nil {
		return "", fmt.Errorf("当前授权配置无法规范化")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
