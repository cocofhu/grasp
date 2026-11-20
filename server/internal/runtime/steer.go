package runtime

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// SteerDeliveryTimeout bounds one native steer. chatsession holds its
// transition gate meanwhile, so a cancel waits for at most this long.
const SteerDeliveryTimeout = 10 * time.Second

// SessionTurnSteerer is an optional provider capability: deliver input into
// the in-flight turn of a parked review session (lane "") or of a visitor
// lane. A status other than SteerDelivered leaves the input with the caller.
type SessionTurnSteerer interface {
	SteerSessionTurn(ctx context.Context, runID, nodeID, lane, text string, images []models.PromptImage) (sandbox.SteerStatus, error)
}

// SteerResult maps a bridge steer answer onto the chatsession contract: a
// delivered input joined the turn, a turn that already ended needs no
// interrupt, and anything else (unsupported or failed) falls back to
// interrupting the turn.
func SteerResult(status sandbox.SteerStatus, err error) chatsession.SteerResult {
	switch {
	case err == nil && status == sandbox.SteerDelivered:
		return chatsession.SteerJoined
	case err == nil && status == sandbox.SteerNoTurn:
		return chatsession.SteerTurnEnded
	case err != nil:
		log.Warn().Err(err).Str("status", string(status)).Msg("steer: native delivery failed; interrupting the turn instead")
	}
	return chatsession.SteerRejected
}
