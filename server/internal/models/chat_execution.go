package models

import "time"

// ChatExecution freezes the route used for one assistant turn. It describes
// the selected execution; UsageByModel separately reports models actually used.
type ChatExecution struct {
	AgentName       string    `json:"agentName"`
	Backend         string    `json:"backend"`
	ConfigurationID string    `json:"configurationId,omitempty"`
	ModelID         string    `json:"modelId,omitempty"`
	ReasoningEffort string    `json:"reasoningEffort,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
}
