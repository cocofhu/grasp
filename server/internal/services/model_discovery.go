package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/modelrouting"
)

var ErrModelDiscovery = errors.New("model discovery unavailable")

const (
	modelDiscoveryTimeout = 20 * time.Second
	modelDiscoveryBytes   = 4 << 20
	modelDiscoveryCount   = 2000
)

// DiscoverModels reads the selected account's advertised models without
// changing its saved allow-list. Only metadata leaves this service; neither
// upstream response errors nor request URLs/headers are exposed to callers.
func (s *ModelConnectionService) DiscoverModels(ctx context.Context, id string) ([]modelrouting.Model, error) {
	connection, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if err := s.validateConnection(&connection); err != nil || connection.State != "active" {
		return nil, fmt.Errorf("%w: connection is not active or valid", ErrModelDiscovery)
	}
	credential, err := s.modelCredential(connection.CredentialID)
	if err != nil || !credential.Enabled || credential.RevokedAt != nil || credential.ValueEnc == "" {
		return nil, fmt.Errorf("%w: credential is unavailable", ErrModelDiscovery)
	}
	secret, err := crypto.Decrypt(credential.ValueEnc)
	if err != nil || strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("%w: credential is unavailable", ErrModelDiscovery)
	}
	base := strings.TrimRight(connection.BaseURL, "/")
	if connection.Protocol == modelrouting.ProtocolAnthropic {
		// Match the native Messages SDK: preserve gateway prefixes and append
		// v1 once, rather than replacing a provider's /anthropic route.
		base = strings.TrimSuffix(base, "/v1") + "/v1"
	}
	endpoint, err := url.Parse(base + "/models")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid endpoint", ErrModelDiscovery)
	}
	ctx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	client := &http.Client{
		Timeout: modelDiscoveryTimeout,
		// Redirects must never forward an account's secret to another route.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	models := make([]modelrouting.Model, 0)
	seen := map[string]bool{}
	remaining := int64(modelDiscoveryBytes)
	after := ""
	for page := 0; page < 4; page++ {
		requestURL := *endpoint
		if connection.Protocol == modelrouting.ProtocolAnthropic {
			query := requestURL.Query()
			query.Set("limit", "1000")
			if after != "" {
				query.Set("after_id", after)
			}
			requestURL.RawQuery = query.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid request", ErrModelDiscovery)
		}
		req.Header.Set("Accept", "application/json")
		if connection.Auth.Scheme == "x-api-key" {
			req.Header.Set("x-api-key", strings.TrimSpace(secret))
		} else {
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(secret))
		}
		if connection.Protocol == modelrouting.ProtocolAnthropic {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		response, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: request failed; retry or enter a model manually", ErrModelDiscovery)
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			return nil, fmt.Errorf("%w: upstream HTTP %d", ErrModelDiscovery, response.StatusCode)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, remaining+1))
		_ = response.Body.Close()
		if readErr != nil || int64(len(body)) > remaining {
			return nil, fmt.Errorf("%w: response exceeds the size limit or could not be read", ErrModelDiscovery)
		}
		remaining -= int64(len(body))
		var result struct {
			Data    json.RawMessage `json:"data"`
			HasMore bool            `json:"has_more"`
			LastID  string          `json:"last_id"`
		}
		if err := json.Unmarshal(body, &result); err != nil || len(result.Data) == 0 || result.Data[0] != '[' {
			return nil, fmt.Errorf("%w: invalid model list response", ErrModelDiscovery)
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(result.Data, &rows); err != nil || len(rows) > modelDiscoveryCount {
			return nil, fmt.Errorf("%w: invalid or oversized model list", ErrModelDiscovery)
		}
		for _, raw := range rows {
			var row discoveredModel
			if json.Unmarshal(raw, &row) != nil {
				continue
			}
			model := row.model()
			if model.ID == "" || seen[model.ID] {
				continue
			}
			seen[model.ID] = true
			models = append(models, model)
		}
		if len(models) > modelDiscoveryCount {
			return nil, fmt.Errorf("%w: model list exceeds the limit", ErrModelDiscovery)
		}
		if connection.Protocol != modelrouting.ProtocolAnthropic || !result.HasMore {
			return models, nil
		}
		if result.LastID == "" || result.LastID == after || remaining == 0 {
			return nil, fmt.Errorf("%w: incomplete model list response", ErrModelDiscovery)
		}
		after = result.LastID
	}
	return nil, fmt.Errorf("%w: model list exceeds the page limit", ErrModelDiscovery)
}

// Providers often add metadata to the standard list shape. Preserve only
// explicit, understood fields; names never imply tool or reasoning support.
type discoveredModel map[string]json.RawMessage

func (row discoveredModel) model() modelrouting.Model {
	model := modelrouting.Model{
		ID: row.stringValue("id"), Name: row.stringValue("name", "display_name"), Source: "discovered",
		ContextWindow: row.positiveInt("context_window", "max_input_tokens"),
		MaxTokens:     row.positiveInt("max_tokens", "max_output_tokens"),
	}
	for _, input := range row.stringList("input") {
		if input == "text" || input == "image" {
			model.Input = append(model.Input, input)
		}
	}
	var capabilities map[string]json.RawMessage
	_ = json.Unmarshal(row["capabilities"], &capabilities)
	model.Capabilities = modelrouting.ModelCapabilities{
		ToolCalls: discoveredCapability(capabilities, "toolCalls", "tool_calls"),
		Vision:    discoveredCapability(capabilities, "vision"),
		Reasoning: discoveredCapability(capabilities, "reasoning", "thinking"),
	}
	seen := map[string]bool{}
	for _, effort := range row.stringList("reasoning_efforts") {
		if effort != "" && strings.TrimSpace(effort) == effort && !seen[effort] {
			model.ReasoningEfforts = append(model.ReasoningEfforts, effort)
			seen[effort] = true
		}
	}
	return model
}

func (row discoveredModel) stringValue(names ...string) string {
	for _, name := range names {
		var value string
		if json.Unmarshal(row[name], &value) == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (row discoveredModel) positiveInt(names ...string) int {
	for _, name := range names {
		var value int
		if json.Unmarshal(row[name], &value) == nil && value > 0 {
			return value
		}
	}
	return 0
}

func (row discoveredModel) stringList(name string) []string {
	var values []string
	if json.Unmarshal(row[name], &values) != nil {
		return nil
	}
	return values
}

func discoveredCapability(values map[string]json.RawMessage, names ...string) *bool {
	for _, name := range names {
		raw := values[name]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var supported bool
		if json.Unmarshal(raw, &supported) == nil {
			return &supported
		}
		var detail struct {
			Supported *bool `json:"supported"`
		}
		if json.Unmarshal(raw, &detail) == nil && detail.Supported != nil {
			return detail.Supported
		}
	}
	return nil
}
