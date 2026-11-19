package models

import "github.com/cocofhu/grasp/internal/chatcmd"

// ChatContext separates the model's current dialogue from the archived rows.
// A reset remains pending until a subsequent Agent turn succeeds.
type ChatContext struct {
	Messages []ReactMessage
	Pending  string
	Started  bool
}

func ActiveChatContext(history []ReactMessage) ChatContext {
	context := ChatContext{Messages: history}
	start, hasCommands := 0, false
	for i, row := range history {
		switch row.ContextCommand {
		case chatcmd.New:
			context.Started = true
			start = i + 1
			context.Pending = chatcmd.New
			hasCommands = true
		case chatcmd.Compact:
			context.Pending = chatcmd.Compact
			hasCommands = true
		default:
			if context.Pending != "" && row.Role == "agent" && !row.Handoff && ReplyOutcome(row) == OutcomeOK {
				context.Pending = ""
			}
		}
	}
	if hasCommands {
		context.Messages = make([]ReactMessage, 0, len(history)-start)
		for _, row := range history[start:] {
			if row.ContextCommand == "" {
				context.Messages = append(context.Messages, row)
			}
		}
	}
	return context
}
