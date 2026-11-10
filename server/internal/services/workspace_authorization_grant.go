package services

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This proof contains hashes and non-secret dependency metadata only. Full
// selected credentials are resolved by P1 before SQL; a matching durable hash
// alone is never used to infer that a currently revoked credential is valid.
type workspaceAuthorizationGrant struct {
	Fingerprints   []string
	Credentials    []models.Credential
	Configurations []models.ModelConfiguration
	Connections    []models.ModelConnection
}

var workspaceCredentialColumns = []string{"id", "purpose", "issuer", "tool_id", "secret_kind", "host", "secret_revision", "type", "provider", "env_key", "target", "metadata", "enabled", "revoked_at"}

func authorizationMCPText(value MCPServer) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func (a *WorkspaceAuthorization) prepareGrant(ctx context.Context, resource workspaceAuthorizationResource) (workspaceAuthorizationGrant, error) {
	var proof workspaceAuthorizationGrant
	if a.sandbox.skills == nil || a.sandbox.modelBinding == nil {
		return proof, ErrWorkspaceJobAuthority
	}
	profile, ok := a.sandbox.skills.Get(resource.Profile)
	if !ok {
		return proof, ErrWorkspaceJobUnsupported
	}
	agents := []Agent{a.sandbox.effectiveAgent(profile)}
	if resource.Purpose == SandboxPurposeSolo {
		var environment models.Sandbox
		if err := a.db.WithContext(ctx).Select("id", "purpose", "thread_id", "profile", "authorization_fingerprint").Take(&environment, resource.ID).Error; err != nil {
			return proof, err
		}
		grant, err := a.sandbox.soloRecoveryAuthorization(ctx, &environment)
		if err != nil {
			return proof, err
		}
		agents[0] = grant.agent
		if grant.desired != grant.fingerprint {
			configuration := grant.session.CurrentConfigurationID
			if configuration == "" {
				configuration = grant.session.InitialConfigurationID
			}
			selected, err := selectSoloConfiguration(a.db, a.sandbox.effectiveAgent(profile), configuration)
			if err != nil {
				return proof, err
			}
			agents = append(agents, selected)
		}
	}
	credentials, configurations, connections := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, agent := range agents {
		fingerprint, err := a.sandbox.currentAuthorizationFingerprint(ctx, agent)
		if err != nil {
			return proof, err
		}
		proof.Fingerprints = append(proof.Fingerprints, fingerprint)
		resolved, err := a.sandbox.modelBinding(agent.AcpBackend, agent.ModelBinding)
		if err != nil || resolved.CredentialID == "" || agent.ModelBinding == nil || agent.ModelBinding.ConfigurationID == "" {
			return proof, ErrWorkspaceJobAuthority
		}
		credentials[resolved.CredentialID], configurations[agent.ModelBinding.ConfigurationID] = true, true
		if resolved.Connection != nil {
			connections[resolved.Connection.ID] = true
			credentials[resolved.Connection.CredentialID] = true
		}
		if agent.GitBindings != nil {
			for _, binding := range agent.GitBindings.Items {
				credentials[binding.CredentialID] = true
			}
		}
		for _, id := range agent.CredentialBindings {
			if id != nil {
				credentials[strings.TrimSpace(*id)] = true
			}
		}
		for _, server := range agent.MCP {
			// P1 already validates every reference and its current secret.
			for _, match := range authorizationCredentialRef.FindAllStringSubmatch(authorizationMCPText(server), -1) {
				credentials[strings.TrimSpace(match[1])] = true
			}
		}
	}
	if len(credentials)+len(configurations)+len(connections) > 256 {
		return proof, ErrWorkspaceJobUnsupported
	}
	for _, id := range sortedAuthorizationIDs(credentials) {
		var row models.Credential
		if id == "" {
			return proof, ErrWorkspaceJobAuthority
		}
		if err := a.db.WithContext(ctx).Select(workspaceCredentialColumns).Take(&row, "id = ? AND enabled = ? AND revoked_at IS NULL", id, true).Error; err != nil {
			return proof, err
		}
		proof.Credentials = append(proof.Credentials, row)
	}
	for _, id := range sortedAuthorizationIDs(configurations) {
		var row models.ModelConfiguration
		if err := a.db.WithContext(ctx).Take(&row, "id = ? AND state = ?", id, "active").Error; err != nil {
			return proof, err
		}
		proof.Configurations = append(proof.Configurations, row)
	}
	for _, id := range sortedAuthorizationIDs(connections) {
		var row models.ModelConnection
		if err := a.db.WithContext(ctx).Take(&row, "id = ? AND state = ?", id, "active").Error; err != nil {
			return proof, err
		}
		proof.Connections = append(proof.Connections, row)
	}
	return proof, nil
}

func sortedAuthorizationIDs(ids map[string]bool) []string {
	values := make([]string, 0, len(ids))
	for id := range ids {
		values = append(values, id)
	}
	slices.Sort(values)
	return values
}

func (a *WorkspaceAuthorization) checkGrantMetadata(db *gorm.DB, before workspaceAuthorizationGrant) error {
	query := func() *gorm.DB {
		fresh := db.Session(&gorm.Session{NewDB: true})
		if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
			fresh = fresh.Clauses(clause.Locking{Strength: "SHARE"})
		}
		return fresh
	}
	for _, prior := range before.Credentials {
		var current models.Credential
		if err := query().Select(workspaceCredentialColumns).Take(&current, "id = ? AND enabled = ? AND revoked_at IS NULL", prior.ID, true).Error; err != nil {
			return err
		}
		if authorizationDigest(current) != authorizationDigest(prior) {
			return ErrWorkspaceJobConflict
		}
	}
	for _, prior := range before.Configurations {
		var current models.ModelConfiguration
		if err := query().Take(&current, "id = ? AND state = ?", prior.ID, "active").Error; err != nil {
			return err
		}
		if authorizationDigest(current) != authorizationDigest(prior) {
			return ErrWorkspaceJobConflict
		}
	}
	for _, prior := range before.Connections {
		var current models.ModelConnection
		if err := query().Take(&current, "id = ? AND state = ?", prior.ID, "active").Error; err != nil {
			return err
		}
		if authorizationDigest(current) != authorizationDigest(prior) {
			return ErrWorkspaceJobConflict
		}
	}
	return nil
}
