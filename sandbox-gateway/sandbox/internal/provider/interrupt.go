package provider

import (
	"context"
	"errors"
)

// Interruption is how a turn stopped before it finished ends, by the cause its
// ctx was cancelled with (nil when the agent itself reported the abort).
type Interruption struct {
	StopReason string
	// Err is what Prompt returns.
	Err error
	// Diagnostic is the error_text to show, if any.
	Diagnostic string
	// Final reports whether prompt_done is sent. ErrTurnRecover sends nothing:
	// the bridge continues the turn, which must not look finished.
	Final bool
}

// InterruptionFor maps the cancel cause of a stopped turn.
func InterruptionFor(cause error) Interruption {
	switch {
	case errors.Is(cause, ErrTurnRecover):
		return Interruption{StopReason: "recover", Err: cause}
	case errors.Is(cause, ErrTurnTimeout):
		return Interruption{StopReason: TimeoutStopReason(cause), Err: cause, Diagnostic: cause.Error(), Final: true}
	}
	return Interruption{StopReason: "cancelled", Err: context.Canceled, Final: true}
}
