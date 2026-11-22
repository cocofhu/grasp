package acp

import (
	"encoding/json"

	"backend/internal/provider"
)

// promptUsage is one usage bucket as agents spell it in a session/prompt
// result: ACP's draft field names plus the cacheCreationTokens alias.
type promptUsage struct {
	InputTokens         int64 `json:"inputTokens"`
	OutputTokens        int64 `json:"outputTokens"`
	ThoughtTokens       int64 `json:"thoughtTokens"`
	CachedReadTokens    int64 `json:"cachedReadTokens"`
	CachedWriteTokens   int64 `json:"cachedWriteTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`
	TotalTokens         int64 `json:"totalTokens"`
}

// tokens maps the bucket onto disjoint counters. Agents disagree on whether
// inputTokens includes cached input and whether outputTokens includes
// thinking (Antigravity changed both between releases), so totalTokens
// decides; without it the counters are taken as disjoint.
func (u promptUsage) tokens() provider.TokenUsage {
	write := u.CachedWriteTokens + u.CacheCreationTokens
	out := provider.TokenUsage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CachedReadTokens, CacheWriteTokens: write}
	cached := u.CachedReadTokens + write
	thought := u.ThoughtTokens
	switch total := u.TotalTokens; {
	case total == 0 || total == u.InputTokens+cached+u.OutputTokens:
	case thought > 0 && total == u.InputTokens+u.OutputTokens+thought:
		out.InputTokens = max(u.InputTokens-cached, 0)
		out.OutputTokens += thought
	case total == u.InputTokens+u.OutputTokens:
		out.InputTokens = max(u.InputTokens-cached, 0)
	case thought > 0 && total == u.InputTokens+cached+u.OutputTokens+thought:
		out.OutputTokens += thought
	}
	return out
}

func (u promptUsage) empty() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.CachedReadTokens == 0 && u.CachedWriteTokens == 0 && u.CacheCreationTokens == 0
}

// PromptResultUsage reads the turn usage from a session/prompt result: the
// draft ACP `usage` object (Antigravity) or `_meta.usage` with an optional
// per-model `modelUsage` breakdown (Grok). Both report the turn, not the
// session.
func PromptResultUsage(result json.RawMessage, model string) map[string]provider.TokenUsage {
	var out struct {
		Usage *promptUsage `json:"usage"`
		Meta  struct {
			Usage *struct {
				promptUsage
				ModelUsage map[string]promptUsage `json:"modelUsage"`
			} `json:"usage"`
		} `json:"_meta"`
	}
	if json.Unmarshal(result, &out) != nil {
		return nil
	}
	usage := map[string]provider.TokenUsage{}
	switch meta := out.Meta.Usage; {
	case out.Usage != nil && !out.Usage.empty():
		usage[model] = out.Usage.tokens()
	case meta != nil && len(meta.ModelUsage) > 0:
		for name, bucket := range meta.ModelUsage {
			if name == "" {
				name = model
			}
			usage[name] = usage[name].Plus(bucket.tokens())
		}
	case meta != nil && !meta.empty():
		usage[model] = meta.tokens()
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}
