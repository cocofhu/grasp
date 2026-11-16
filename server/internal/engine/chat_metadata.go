package engine

import (
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// applyReplyMetadata stores only this provider call's delta. StateRun's token
// aggregate spans the whole visit and cannot identify an individual reply.
func applyReplyMetadata(message *models.ReactMessage, turn runtime.ReactTurn) {
	message.ReplyMetadata = turn.ReplyMetadata()
	if execution := message.Execution; execution != nil && !execution.StartedAt.IsZero() {
		started, ended := execution.StartedAt, time.Now().UTC()
		message.StartedAt, message.EndedAt = &started, &ended
	}
}
