package engine

import (
	"context"
	"time"

	"github.com/cocofhu/grasp/internal/chatcmd"
	"github.com/cocofhu/grasp/internal/models"
)

func appendContextCommand(messages []models.ReactMessage, item *reviewQueueItem) []models.ReactMessage {
	human := models.ReactMessage{
		Role: "human", Text: item.Text, At: time.Now().Format(time.RFC3339), TurnID: item.ID,
		ClientMessageID: item.ClientMessageID, GateNodeID: item.GateNodeID, ContextCommand: item.Command.Name,
	}
	ack := human
	ack.Role, ack.Text, ack.Outcome = "agent", chatcmd.Scheduled(item.Command.Name), models.OutcomeOK
	return append(messages, human, ack)
}

func (e *Engine) saveContextCommand(ctx context.Context, conv *models.ReactConversation, item *reviewQueueItem) (bool, error) {
	conv.Messages = appendContextCommand(conv.Messages, item)
	if err := e.db.WithContext(ctx).Model(conv).Select("Messages").Updates(conv).Error; err != nil {
		return ctx.Err() != nil, err
	}
	if item.Source == "gate" {
		// Gate preview also renders acknowledgments through the live event stream.
		e.publishAcp(conv.RunID, conv.NodeID, []models.AcpEvent{{Kind: "message", Text: conv.Messages[len(conv.Messages)-1].Text}}, true)
	}
	e.publishReact(conv.RunID, conv.NodeID)
	return false, nil
}
