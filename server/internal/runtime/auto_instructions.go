package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

var ErrInstructionDeliveryUncertain = errors.New("补充要求的处理结果尚未确认，请核对本轮记录后重试")

// AutoInstruction is a user-authored supplement for one already-running visit.
// The native session processes it at a turn boundary, without manual takeover.
type AutoInstruction struct {
	ID   string
	Text string
}

// AutoInstructions is a durable ordered mailbox supplied by the engine. Next
// claims one message. sealIfEmpty closes admission atomically with finding an
// empty queue; a racing user submission is either processed or clearly refused.
type AutoInstructions interface {
	Next(ctx context.Context, sealIfEmpty bool) (*AutoInstruction, error)
	Complete(ctx context.Context, id, state, reason string) error
	RestartAttempt(ctx context.Context) error
}

func (c *acpProvider) applyAutoInstruction(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, item *AutoInstruction) (*sandbox.ChatResult, error) {
	// A previous node_complete cannot acknowledge a later human requirement.
	c.host.ClearOutcome(req.RunID, req.NodeID)
	turn, cancel := c.turnCtx(ctx, req)
	result, err := c.streamChat(turn, acp, req, "用户对当前工作流节点补充要求（仍保持自动执行）：\n\n"+item.Text+"\n\n请处理该要求，核对产物契约并重新通过 node_complete 报告本轮结果。", nil)
	cancel()
	if err == nil && result == nil {
		err = errors.New("原生会话没有返回补充要求处理结果")
	}
	if err == nil {
		err = c.codexTurnLoginErr(result, req)
	}
	state, reason := "applied", ""
	if err != nil {
		state, reason = "unknown", "原生会话未确认处理结果，请核对后决定是否重试"
	}
	ack, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	if ackErr := req.Instructions.Complete(ack, item.ID, state, reason); ackErr != nil {
		return result, fmt.Errorf("%w：%v", ErrInstructionDeliveryUncertain, ackErr)
	}
	if err != nil {
		return result, fmt.Errorf("%w：%v", ErrInstructionDeliveryUncertain, err)
	}
	return result, nil
}
