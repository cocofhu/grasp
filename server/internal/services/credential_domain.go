package services

import (
	"strings"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
)

func credentialDomain(row models.Credential) (purpose, issuer, tool, kind string) {
	purpose, issuer, tool, kind = row.Purpose, row.Issuer, row.ToolID, row.SecretKind
	if purpose == "" {
		switch row.Type {
		case "ai":
			purpose = "tool_auth"
			if row.Provider == "opencode" || row.Provider == "deepseek_harness" {
				purpose = "model_api"
			}
		case "git", "ssh":
			purpose = row.Type
		}
	}
	if issuer == "" {
		issuer = row.Provider
		if row.Provider == "opencode" {
			if provider, ok := row.Metadata["provider"].(string); ok {
				issuer = provider
			}
		}
		if row.Provider == "deepseek_harness" {
			issuer = "deepseek"
		}
	}
	if tool == "" && purpose == "tool_auth" {
		tool = row.Provider
		if tool == "claude" || tool == "anthropic" {
			tool = "claude_code"
		}
	}
	if kind == "" {
		kind = "api_key"
		if tool == "codex" {
			kind = "login_file"
		}
		if tool == "trae" || purpose == "git" {
			kind = "token"
		}
		if purpose == "ssh" {
			kind = "ssh_private_key"
			if strings.Contains(strings.ToLower(row.EnvKey+row.Name), "host") {
				kind = "ssh_known_hosts"
			}
		}
	}
	return
}

// Legacy writes keep their exact env bindings. Explicit domain writes have no
// tool-specific env binding for model keys, so keys cannot be globally injected.
func normalizeCredentialDomainInput(in *CredentialInput) error {
	in.Purpose = strings.ToLower(strings.TrimSpace(in.Purpose))
	in.Issuer = strings.TrimSpace(in.Issuer)
	in.ToolID = strings.TrimSpace(in.ToolID)
	in.SecretKind = strings.TrimSpace(in.SecretKind)
	if in.Purpose == "" {
		return nil
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
	case "git", "ssh":
		in.Type = in.Purpose
	default:
		return ErrCredentialPurpose
	}
	if in.SecretKind == "" {
		_, _, _, in.SecretKind = credentialDomain(models.Credential{Type: in.Type, Purpose: in.Purpose, Provider: in.Provider, ToolID: in.ToolID, EnvKey: in.EnvKey})
	}
	switch in.Purpose {
	case "model_api":
		if in.SecretKind != "api_key" && in.SecretKind != "token" {
			return ErrCredentialPurpose
		}
	case "tool_auth":
		if in.ToolID == "codex" && in.SecretKind != "login_file" {
			return ErrCredentialPurpose
		}
		if in.ToolID != "codex" && in.SecretKind != "api_key" && in.SecretKind != "token" {
			return ErrCredentialPurpose
		}
	case "git":
		if in.SecretKind != "token" && in.SecretKind != "api_key" {
			return ErrCredentialPurpose
		}
	case "ssh":
		if in.SecretKind != "ssh_private_key" && in.SecretKind != "ssh_known_hosts" {
			return ErrCredentialPurpose
		}
	}
	return nil
}

func nextSecretRevision(current uint64) uint64 {
	if current < 1 {
		return 2
	}
	return current + 1
}
