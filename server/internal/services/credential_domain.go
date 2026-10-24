package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

// Credential domains are stored explicitly. Reading a credential never infers
// a model provider or coding tool from its name, metadata or environment key.
func credentialDomain(row models.Credential) (purpose, issuer, tool, kind string) {
	return row.Purpose, row.Issuer, row.ToolID, row.SecretKind
}

func normalizeCredentialDomainInput(in *CredentialInput) error {
	in.Purpose = strings.ToLower(strings.TrimSpace(in.Purpose))
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.Issuer = strings.TrimSpace(in.Issuer)
	in.ToolID = strings.TrimSpace(in.ToolID)
	in.SecretKind = strings.TrimSpace(in.SecretKind)
	if in.Purpose == "" {
		switch in.Type {
		case "ai":
			return ErrCredentialPurpose
		case "git", "ssh", "mcp", "custom":
			in.Purpose = in.Type
		default:
			return nil
		}
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
		if in.ToolID == "" {
			in.ToolID = in.Provider
		}
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
			if in.EnvKey == "GIT_SSH_KNOWN_HOSTS" {
				in.SecretKind = "ssh_known_hosts"
			}
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
	return nil
}

func nextSecretRevision(current uint64) uint64 {
	if current < 1 {
		return 2
	}
	return current + 1
}
