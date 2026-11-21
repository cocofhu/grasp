package provider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// SubagentPoll is how often a finished turn re-checks the subagents it waits
// for; HaltPoll how often a halt re-checks that what it interrupted stopped.
// SubagentPoll is a var so tests can shrink it.
var SubagentPoll = time.Second

const HaltPoll = 100 * time.Millisecond

// ErrSubagentHostExited ends a wait whose agent process exited first.
var ErrSubagentHostExited = errors.New("agent exited while subagents were running")

// SubagentWait is what a finished turn waits on.
type SubagentWait struct {
	// CallID names the wait's tool call in the transcript.
	CallID string
	// Running counts the subagents still running. An error means their
	// state is unknown, and unknown is not idle.
	Running func(context.Context) (int, error)
	// Halt stops every subagent; an interrupted wait returns after it.
	Halt func()
	// HostDone closes when the process running the subagents exits.
	HostDone <-chan struct{}
	Emit     func(update map[string]any)
}

// AwaitSubagents holds a finished turn until no subagent runs, so
// prompt_done never reports work as finished while background agents still
// change the workspace. It fails when ctx ends or the host exits first.
func AwaitSubagents(ctx context.Context, w SubagentWait) error {
	n, err := w.Running(ctx)
	if err == nil && n == 0 {
		return nil
	}
	title := fmt.Sprintf("等待 %d 个子 Agent 完成", n)
	if err != nil {
		log.Printf("provider: 子 Agent 状态未知，继续等待: %v", err)
		title = "等待子 Agent 完成"
	}
	w.Emit(map[string]any{"sessionUpdate": "tool_call", "toolCallId": w.CallID, "status": "in_progress", "title": title})
	end := func(status string) {
		w.Emit(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": w.CallID, "status": status})
	}
	tick := time.NewTicker(SubagentPoll)
	defer tick.Stop()
	for err != nil || n > 0 {
		select {
		case <-ctx.Done():
			w.Halt()
			end("failed")
			return ctx.Err()
		case <-w.HostDone:
			end("failed")
			return ErrSubagentHostExited
		case <-tick.C:
			if n, err = w.Running(ctx); err != nil {
				log.Printf("provider: 子 Agent 状态未知，继续等待: %v", err)
			}
		}
	}
	end("completed")
	return nil
}
