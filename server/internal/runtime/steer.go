package runtime

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"

	"github.com/rs/zerolog/log"
)

// SteerDeliveryTimeout bounds the wait for the bridge's answer to a steer.
// It exceeds the bridge's own bound on the native delivery (20s), so the
// answer is definite unless the connection drops. chatsession holds its
// transition gate meanwhile.
const SteerDeliveryTimeout = 30 * time.Second

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
