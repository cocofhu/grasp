package runtime

import (
	"context"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// SessionTurnSteerer is an optional provider capability: deliver input into
// the in-flight turn of a parked review session (lane "") or of a visitor
// lane. A status other than SteerDelivered leaves the input with the caller.
type SessionTurnSteerer interface {
	SteerSessionTurn(ctx context.Context, runID, nodeID, lane, text string, images []models.PromptImage) (sandbox.SteerStatus, error)
}

// SteerResult maps a bridge steer answer onto the chatsession contract: a
// delivered input joined the turn, a turn that already ended needs no
// interrupt, an agent that cannot take input mid-turn is interrupted, and an
// input the agent handed back or that failed to arrive waits for the turn to
// finish: the turn's work is worth more than the head start.
func SteerResult(status sandbox.SteerStatus, err error) chatsession.SteerResult {
	switch {
	case err == nil && status == sandbox.SteerDelivered:
		return chatsession.SteerJoined
	case err == nil && status == sandbox.SteerNoTurn:
		return chatsession.SteerDeferred
	case err == nil && status == sandbox.SteerUnsupported:
		return chatsession.SteerRejected
	case err != nil:
		log.Warn().Err(err).Str("status", string(status)).Msg("steer: native delivery failed; the input runs after the turn")
	}
	return chatsession.SteerDeclined
}
