package models

import "strings"

// Display / persistence key for usage that has no model dimension (reported
// totals not covered by a model bucket, or weak keys when ACP_BRIDGE_MODEL was
// not configured).
const TokenUsageModelUnknown = "未知/未分桶"

// TokenUsageModelUnknownDisplay is the default user-visible label when no project alias is set.
const TokenUsageModelUnknownDisplay = "未知模型"

// Source labels persisted on each model bucket.
const (
	TokenUsageSourceUpstream = "upstream"
	TokenUsageSourceBridge   = "via ACP_BRIDGE_MODEL"
	TokenUsageSourceUnknown  = "unknown"
)

// ModelTokenUsage is one model's four-component usage plus attribution.
type ModelTokenUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
	// Source: upstream | via ACP_BRIDGE_MODEL | unknown
	Source string `json:"source,omitempty"`
	// Filled is true when any portion of this bucket came from weak-key
	// backfill via ACP_BRIDGE_MODEL (including when merged with a real key).
	Filled bool `json:"filled,omitempty"`
}

// Total returns the four-component sum for one model bucket.
func (u ModelTokenUsage) Total() int64 {
	return u.InputTokens + u.OutputTokens + u.CacheReadTokens + u.CacheWriteTokens
}

// AsTokenUsage returns the four components without source/filled metadata.
func (u ModelTokenUsage) AsTokenUsage() TokenUsage {
	return TokenUsage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}

// TokenUsageByModel maps ingest-merged modelKey → bucket. nil = not reported;
// non-nil (incl. empty) means usage was reported with per-model detail.
type TokenUsageByModel map[string]ModelTokenUsage

// IsWeakModelKey reports whether an upstream usage map key should be treated as
// a weak key eligible for ACP_BRIDGE_MODEL backfill (default/unknown/empty).
func IsWeakModelKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	return k == "" || k == "default" || k == "unknown"
}

// AddTokenUsageByModel merges src into dst by model key (component-wise). nil
// sources are ignored; the first non-nil source establishes presence.
func AddTokenUsageByModel(dst, src TokenUsageByModel) TokenUsageByModel {
	if src == nil {
		return dst
	}
	if dst == nil {
		return CloneTokenUsageByModel(src)
	}
	for k, su := range src {
		du, ok := dst[k]
		if !ok {
			dst[k] = su
			continue
		}
		du.InputTokens += su.InputTokens
		du.OutputTokens += su.OutputTokens
		du.CacheReadTokens += su.CacheReadTokens
		du.CacheWriteTokens += su.CacheWriteTokens
		du.Filled = du.Filled || su.Filled
		du.Source = mergeModelUsageSource(du.Source, su.Source, du.Filled)
		dst[k] = du
	}
	return dst
}

func mergeModelUsageSource(a, b string, filled bool) string {
	if filled {
		return TokenUsageSourceBridge
	}
	if a == TokenUsageSourceUnknown || b == TokenUsageSourceUnknown {
		if a == TokenUsageSourceUpstream || b == TokenUsageSourceUpstream {
			return TokenUsageSourceUpstream
		}
		return TokenUsageSourceUnknown
	}
	if a == TokenUsageSourceBridge || b == TokenUsageSourceBridge {
		return TokenUsageSourceBridge
	}
	if a != "" {
		return a
	}
	return b
}

// CloneTokenUsageByModel returns a shallow copy of the map (bucket values
// copied by value), or nil when src is nil.
func CloneTokenUsageByModel(src TokenUsageByModel) TokenUsageByModel {
	if src == nil {
		return nil
	}
	out := make(TokenUsageByModel, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
