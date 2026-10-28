package engine

import (
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// applyReplyMetadata stores only this provider call's delta. StateRun's token
// aggregate spans the whole visit and cannot identify an individual reply.
func applyReplyMetadata(message *models.ReactMessage, turn runtime.ReactTurn) {
	message.Usage = models.CloneTokenUsage(turn.Usage)
	message.UsageByModel = models.CloneTokenUsageByModel(turn.UsageByModel)
	message.DurationMs = turn.DurationMs
	if turn.Execution != nil {
		execution := *turn.Execution
		message.Execution = &execution
		if !execution.StartedAt.IsZero() {
			started, ended := execution.StartedAt, time.Now().UTC()
			message.StartedAt, message.EndedAt = &started, &ended
		}
	}
}
