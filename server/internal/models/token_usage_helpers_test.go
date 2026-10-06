package models

// sumTokenUsageByModel returns the component-wise total across buckets.
// nil byModel → nil; non-nil (incl. empty) → non-nil total (reported).
// Test-only helper.
func sumTokenUsageByModel(byModel TokenUsageByModel) *TokenUsage {
	if byModel == nil {
		return nil
	}
	out := &TokenUsage{}
	for _, u := range byModel {
		out.InputTokens += u.InputTokens
		out.OutputTokens += u.OutputTokens
		out.CacheReadTokens += u.CacheReadTokens
		out.CacheWriteTokens += u.CacheWriteTokens
	}
	return out
}
