package provider

import (
	"context"
	"errors"
	"maps"
)

// EndInterrupted ends a turn that stopped before it finished, by the cause
// its ctx was cancelled with, and returns what Prompt returns. ErrTurnRecover
// sends nothing: the bridge continues the turn, which must not look finished.
func EndInterrupted(ctx context.Context, emit func(map[string]any), session string, usage map[string]TokenUsage) (TurnResult, error) {
	cause := context.Cause(ctx)
	res := TurnResult{Usage: usage, UsagePartial: len(usage) > 0}
	err := error(context.Canceled)
	switch {
	case errors.Is(cause, ErrTurnRecover):
		res.StopReason = "recover"
		return res, cause
	case errors.Is(cause, ErrTurnTimeout):
		res.StopReason, err = TimeoutStopReason(cause), cause
		emit(ErrorFrame(cause.Error()))
	case errors.Is(cause, ErrAgentExited):
		res.StopReason, err = "failed", cause
		emit(ErrorFrame(cause.Error()))
	default:
		res.StopReason = "cancelled"
	}
	emit(PromptDoneFrame(session, res.StopReason, usage, res.UsagePartial))
	return res, err
}

// EndFailed ends a turn that failed with the user-facing msg. A turn whose
// ctx was cancelled ends as interrupted: the failure is the interruption.
func EndFailed(ctx context.Context, emit func(map[string]any), session, msg string) (TurnResult, error) {
	if ctx.Err() != nil {
		return EndInterrupted(ctx, emit, session, nil)
	}
	emit(ErrorFrame(msg))
	emit(PromptDoneFrame(session, "failed", nil, false))
	return TurnResult{StopReason: "failed"}, errors.New(msg)
}

// AddUsage adds usage to total, per model.
func AddUsage(total, usage map[string]TokenUsage) {
	for model, u := range usage {
		total[model] = total[model].Plus(u)
	}
}

// CopyUsage is usage for a caller to keep, or nil when empty.
func CopyUsage(usage map[string]TokenUsage) map[string]TokenUsage {
	if len(usage) == 0 {
		return nil
	}
	return maps.Clone(usage)
}
