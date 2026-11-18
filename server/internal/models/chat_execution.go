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
	ServiceTier     string    `json:"serviceTier,omitempty"`
	StartedAt       time.Time `json:"startedAt"`
}

// ReplyMetadata is what one assistant reply reports about itself: the route it
// ran on, its own token delta and how long it took. Visit-wide totals belong
// on a reply only when that reply is the whole visit (an unattended run).
type ReplyMetadata struct {
	Execution    *ChatExecution    `json:"execution,omitempty"`
	Usage        *TokenUsage       `json:"usage,omitempty"`
	UsageByModel TokenUsageByModel `json:"usageByModel,omitempty"`
	DurationMs   int64             `json:"durationMs,omitempty"`
}

// NewReplyMetadata deep-copies its inputs so the reply never aliases them.
func NewReplyMetadata(execution *ChatExecution, usage *TokenUsage, byModel TokenUsageByModel, durationMs int64) ReplyMetadata {
	out := ReplyMetadata{Usage: CloneTokenUsage(usage), UsageByModel: CloneTokenUsageByModel(byModel), DurationMs: durationMs}
	if execution != nil {
		cp := *execution
		out.Execution = &cp
	}
	return out
}

// ReplyMetadata is the visit as one reply: an automatic run nobody talked to.
func (sr StateRun) ReplyMetadata() ReplyMetadata {
	return NewReplyMetadata(sr.Execution, sr.Usage, sr.UsageByModel, int64(sr.DurationSec)*1000)
}
