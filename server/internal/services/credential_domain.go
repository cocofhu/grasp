package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// Credential domains are stored explicitly. Reading a credential never infers
// a model provider or coding tool from its name, metadata or environment key.
func credentialDomain(row models.Credential) (purpose, issuer, tool, kind string) {
	return row.Purpose, row.Issuer, row.ToolID, row.SecretKind
}

// currentCredential accepts only metadata written using the explicit domain
// contract. Reading never upgrades an old row or invents a missing field.
func currentCredential(row models.Credential) bool {
	if row.BuiltinSlot != "" || row.SecretRevision < 1 || row.Name == "" || strings.TrimSpace(row.Name) != row.Name {
		return false
	}
	in := CredentialInput{Purpose: row.Purpose, Type: row.Type, Issuer: row.Issuer, Provider: row.Provider, ToolID: row.ToolID, SecretKind: row.SecretKind, Host: row.Host, EnvKey: row.EnvKey, Metadata: row.Metadata}
	if normalizeCredentialDomainInput(&in) != nil || in.Purpose != row.Purpose || in.Type != row.Type || in.ToolID != row.ToolID || in.SecretKind != row.SecretKind || in.Issuer != row.Issuer || in.Provider != row.Provider || in.Host != row.Host {
		return false
	}
	if !validCredentialEnvKey(row.EnvKey) {
		return false
	}
	if row.Purpose == "mcp" || row.Purpose == "custom" {
		if envauth.IsSecretEnvKey(row.EnvKey) || runtime.IsGitAuthorizationEnvKey(row.EnvKey) {
			return false
		}
		return row.Purpose != "custom" || row.EnvKey != "" || row.Target != ""
	}
	return row.Purpose == "model_api" && row.EnvKey == "" || row.Purpose != "model_api" && row.EnvKey == defaultCredentialEnvKey(in)
}

func normalizeCredentialDomainInput(in *CredentialInput) error {
	in.Purpose = strings.ToLower(strings.TrimSpace(in.Purpose))
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.Provider = strings.TrimSpace(in.Provider)
	in.Issuer = strings.TrimSpace(in.Issuer)
	in.ToolID = strings.TrimSpace(in.ToolID)
	in.SecretKind = strings.TrimSpace(in.SecretKind)
	if in.Purpose == "" {
		return ErrCredentialPurpose
	}
	switch in.Purpose {
	case "model_api":
		if in.ToolID != "" || in.EnvKey != "" || len(in.Metadata) != 0 {
			return ErrCredentialPurpose
		}
		in.Type = "ai"
		if in.Provider == "" {
			in.Provider = in.Issuer
		}
		if in.SecretKind == "" {
			in.SecretKind = "api_key"
		}
		if in.SecretKind != "api_key" && in.SecretKind != "token" {
			return ErrCredentialPurpose
		}
	case "tool_auth":
		capability, ok := modelrouting.CapabilityFor(in.ToolID)
		if !ok {
			return ErrCredentialPurpose
		}
		platform := false
		for _, mode := range capability.Modes {
			if mode == "platform" {
				platform = true
			}
		}
		if !platform {
			return ErrCredentialPurpose
		}
		in.Type, in.Provider = "ai", in.ToolID
		if in.Issuer == "" {
			in.Issuer = in.ToolID
		}
		if in.SecretKind == "" {
			in.SecretKind = "api_key"
			if in.ToolID == "codex" {
				in.SecretKind = "login_file"
			} else if in.ToolID == "trae" {
				in.SecretKind = "token"
			}
		}
		if in.ToolID == "codex" && in.SecretKind != "login_file" {
			return ErrCredentialPurpose
		}
		if (in.ToolID == "opencode" || in.ToolID == "deepseek_harness") && in.SecretKind != "api_key" {
			return ErrCredentialPurpose
		}
		if in.ToolID != "codex" && in.SecretKind != "api_key" && in.SecretKind != "token" {
			return ErrCredentialPurpose
		}
	case "git":
		in.Type = "git"
		if in.SecretKind == "" {
			in.SecretKind = "token"
		}
		if in.SecretKind != "token" && in.SecretKind != "api_key" {
			return ErrCredentialPurpose
		}
	case "ssh":
		in.Type = "ssh"
		if in.SecretKind == "" {
			in.SecretKind = "ssh_private_key"
		}
		if in.SecretKind != "ssh_private_key" && in.SecretKind != "ssh_known_hosts" {
			return ErrCredentialPurpose
		}
	case "mcp", "custom":
		in.Type = in.Purpose
		if in.SecretKind == "" {
			in.SecretKind = "token"
		}
	default:
		return ErrCredentialPurpose
	}
	if in.Issuer == "" {
		in.Issuer = in.Provider
	}
	if in.Purpose == "git" {
		var err error
		in.Host, err = models.NormalizeGitCredentialHost(in.Provider, in.Host)
		if err != nil {
			return err
		}
	} else if strings.TrimSpace(in.Host) != "" {
		return models.ErrGitCredentialHost
	}
	return nil
}

func nextSecretRevision(current uint64) uint64 {
	return current + 1
}
