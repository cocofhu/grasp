package modelrouting

import (
	"net/url"
	"slices"
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
)

// ServiceTiers reports the speed tiers available for this exact model and route.
func ServiceTiers(backend, modelID string, connection *Connection) []string {
	backend = strings.TrimSpace(backend)
	modelID = strings.TrimSpace(modelID)
	if modelID == "" || modelID == "auto" {
		return nil
	}
	if connection != nil {
		if backend == "codex" {
			return ConnectionModelServiceTiers(*connection)[modelID]
		}
		return nil
	}
	if backend != "codex" && backend != "cursor" {
		return nil
	}
	for _, model := range clicatalog.Models(backend) {
		if model.ID == modelID {
			return slices.Clone(model.ServiceTiers)
		}
	}
	return nil
}

// ConnectionModelServiceTiers lists the speed tiers Grasp's Codex adapter can
// send for the configured models on the official OpenAI Responses endpoint.
func ConnectionModelServiceTiers(connection Connection) map[string][]string {
	if connection.State != "active" || connection.Protocol != ProtocolResponses || connection.Auth.Scheme != "bearer" {
		return nil
	}
	base, err := url.Parse(strings.TrimSpace(connection.BaseURL))
	if err != nil || base.Scheme != "https" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil
	}
	if !strings.EqualFold(base.Hostname(), "api.openai.com") || (base.Port() != "" && base.Port() != "443") || strings.TrimRight(base.EscapedPath(), "/") != "/v1" {
		return nil
	}
	tiers := make(map[string][]string)
	for _, model := range connection.Models {
		// https://developers.openai.com/api/docs/guides/fast-mode
		switch model.ID {
		case "gpt-6.2-sol", "gpt-6.2-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol":
			tiers[model.ID] = []string{"fast"}
		}
	}
	return tiers
}
