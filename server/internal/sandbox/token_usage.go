package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// UsageStatus describes the actual turn, including failures with no reply text.
func (r *ChatResult) UsageStatus(err error) string {
	if errors.Is(err, context.Canceled) || r.StopReason == "cancelled" {
		return models.TokenLedgerStatusCancelled
	}
	if err != nil || r.Failed || r.Interrupted || strings.TrimSpace(r.ErrorText) != "" {
		return models.TokenLedgerStatusFailed
	}
	return models.TokenLedgerStatusOK
}

// MergeAttemptUsage keeps the final attempt's content and outcome, while
// retaining consumption from earlier attempts (including an empty final error).
func MergeAttemptUsage(previous, current *ChatResult) *ChatResult {
	if previous == nil || previous.Usage == nil || current != nil && previous.OpID != "" && previous.OpID == current.OpID {
		return current
	}
	if current == nil {
		current = &ChatResult{}
	}
	current.Usage = models.AddTokenUsage(models.CloneTokenUsage(previous.Usage), current.Usage)
	current.UsageByModel = models.AddTokenUsageByModel(models.CloneTokenUsageByModel(previous.UsageByModel), current.UsageByModel)
	current.UsagePartial = current.UsagePartial || previous.UsagePartial
	return current
}

// parsePromptDoneUsage parses prompt_done.usage (map[modelID]TokenUsage),
// optionally backfills weak keys (default/unknown/empty) onto bridgeModel when
// non-empty, and returns the component total plus per-model buckets.
//
// Returns (nil, nil) when raw is empty/null or unparseable. Returns a non-nil
// total (incl. all-zero) whenever the usage field is present and parseable so
// callers can distinguish "reported 0" from "not reported". By-model is always
// non-nil when total is non-nil (may be empty for `{}`).
func parsePromptDoneUsage(raw json.RawMessage, bridgeModel string) (*models.TokenUsage, models.TokenUsageByModel) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var byModel map[string]models.TokenUsage
	if err := json.Unmarshal(raw, &byModel); err != nil {
		return nil, nil
	}
	bridge := strings.TrimSpace(bridgeModel)
	out := make(models.TokenUsageByModel, len(byModel))
	total := &models.TokenUsage{}

	for key, u := range byModel {
		if !u.Valid() {
			return nil, nil
		}
		modelKey, source, filled := resolveModelBucket(key, bridge)
		existing, ok := out[modelKey]
		if !ok {
			existing = models.ModelTokenUsage{Source: source, Filled: filled}
		} else {
			existing.Filled = existing.Filled || filled
			existing.Source = mergeParsedSource(existing.Source, source, existing.Filled)
		}
		existing.InputTokens += u.InputTokens
		existing.OutputTokens += u.OutputTokens
		existing.CacheReadTokens += u.CacheReadTokens
		existing.CacheWriteTokens += u.CacheWriteTokens
		out[modelKey] = existing

		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheReadTokens += u.CacheReadTokens
		total.CacheWriteTokens += u.CacheWriteTokens
	}
	if !total.Valid() {
		return nil, nil
	}
	return total, out
}

// resolveModelBucket maps an upstream key to the display/persistence bucket.
// Real modelIDs are kept as-is; weak keys go to bridge when configured, else
// 「未知/未分桶」.
func resolveModelBucket(rawKey, bridge string) (modelKey, source string, filled bool) {
	if models.IsWeakModelKey(rawKey) {
		if bridge != "" {
			return bridge, models.TokenUsageSourceBridge, true
		}
		return models.TokenUsageModelUnknown, models.TokenUsageSourceUnknown, false
	}
	return rawKey, models.TokenUsageSourceUpstream, false
}

func mergeParsedSource(a, b string, filled bool) string {
	if filled {
		return models.TokenUsageSourceBridge
	}
	if a == models.TokenUsageSourceUnknown || b == models.TokenUsageSourceUnknown {
		return models.TokenUsageSourceUnknown
	}
	if a == models.TokenUsageSourceBridge || b == models.TokenUsageSourceBridge {
		return models.TokenUsageSourceBridge
	}
	if a != "" {
		return a
	}
	return b
}
