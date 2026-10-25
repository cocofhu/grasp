// Package modelrouting defines tool-independent model connections and bindings.
// It contains no secret persistence or CLI execution logic.
package modelrouting

import "time"

const (
	ProtocolChat      = "openai-chat-completions"
	ProtocolResponses = "openai-responses"
	ProtocolAnthropic = "anthropic-messages"
)

type ToolSettings struct {
	ModelRoles   map[string]string `json:"modelRoles,omitempty"`
	RouteProfile string            `json:"routeProfile,omitempty"`
}

type Binding struct {
	Kind            string `json:"kind"`
	ConfigurationID string `json:"configurationId,omitempty"`
	CredentialID    string `json:"credentialId,omitempty"`
	ConnectionID    string `json:"connectionId,omitempty"`
	ModelID         string `json:"modelId,omitempty"`
	// AuxiliaryModelIDs are extra models the user may switch to. Order is the
	// display order. ModelID stays the primary and is the only model automation uses.
	// A missing field on older rows reads as an empty list.
	AuxiliaryModelIDs []string     `json:"auxiliaryModelIds,omitempty"`
	ToolSettings      ToolSettings `json:"toolSettings,omitempty"`
}

// Configuration saves a coding tool and its complete, concrete model binding.
// It contains references only; the secret is resolved separately for each run.
type Configuration struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	Name      string    `json:"name"`
	Backend   string    `json:"backend"`
	Binding   Binding   `gorm:"serializer:json" json:"binding"`
	State     string    `json:"state"`
	Revision  uint64    `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Auth struct {
	Scheme     string `json:"scheme"`
	HeaderName string `json:"headerName,omitempty"`
}

type Service struct {
	Region  string `json:"region,omitempty"`
	Product string `json:"product,omitempty"`
}

type ModelCapabilities struct {
	ToolCalls *bool `json:"toolCalls,omitempty"`
	Vision    *bool `json:"vision,omitempty"`
	Reasoning *bool `json:"reasoning,omitempty"`
}

type Model struct {
	ID               string            `json:"id"`
	Name             string            `json:"name,omitempty"`
	Source           string            `json:"source"`
	Capabilities     ModelCapabilities `json:"capabilities,omitempty"`
	Input            []string          `json:"input,omitempty"`
	ContextWindow    int               `json:"contextWindow,omitempty"`
	MaxTokens        int               `json:"maxTokens,omitempty"`
	ReasoningEfforts []string          `json:"reasoningEfforts,omitempty"`
}

type Connection struct {
	ID           string    `gorm:"primaryKey" json:"id"`
	Name         string    `json:"name"`
	ProviderID   string    `json:"providerId"`
	Protocol     string    `json:"protocol"`
	BaseURL      string    `json:"baseUrl"`
	CredentialID string    `gorm:"index" json:"credentialId"`
	Auth         Auth      `gorm:"serializer:json" json:"auth"`
	Service      Service   `gorm:"serializer:json" json:"service,omitempty"`
	Models       []Model   `gorm:"serializer:json" json:"models"`
	State        string    `json:"state"`
	Revision     uint64    `json:"revision"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Resolved exists only while assembling a run. JSON never contains its secret.
type Resolved struct {
	Backend        string      `json:"backend"`
	Binding        Binding     `json:"binding"`
	Connection     *Connection `json:"connection,omitempty"`
	CredentialID   string      `json:"credentialId"`
	SecretKind     string      `json:"secretKind"`
	SecretRevision uint64      `json:"secretRevision"`
	Secret         string      `json:"-"`
}
