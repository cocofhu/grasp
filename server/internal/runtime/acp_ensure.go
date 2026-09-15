package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/rs/zerolog/log"
)

// clarifyPending is human-facing interaction raised mid-turn (ask_question /
// ask_form). Either field nonempty means the dialogue must stay open.
type clarifyPending struct {
	Questions []models.ReactQuestion
	Forms     []models.ReactForm
}

func (p clarifyPending) any() bool {
	return len(p.Questions) > 0 || len(p.Forms) > 0
}

func takeClarifyPending(h *mcp.Host, runID, nodeID string) clarifyPending {
	return clarifyPending{
		Questions: h.TakePendingQuestions(runID, nodeID),
		Forms:     h.TakePendingForms(runID, nodeID),
	}
}

// ensureOutcome re-prompts the agent to call node_complete when the mark is
// still missing (best-effort; engine fails closed if ultimately absent).
// Aligns with ensureStructured: when Host memory HasOutcome is false, first
// adopt a parseable node_complete.json before re-prompting / fail-closed.
// For react nodes, a pending ask_question raised during the re-prompt aborts
// the completion push and returns those questions (caller must not discard).
// Non-react callers keep the prior discard-and-continue semantics.
func (c *acpProvider) ensureOutcome(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) (clarifyPending, error) {
	outcomeReady := func() bool {
		if c.host.HasOutcome(req.RunID, req.NodeID) {
			return true
		}
		return c.host.RestoreOutcomeFromArtifact(req.RunID, req.NodeID)
	}
	if outcomeReady() {
		return clarifyPending{}, nil
	}
	for i := 0; i <= producesRetry; i++ {
		if outcomeReady() {
			return clarifyPending{}, nil
		}
		if i == producesRetry {
			log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
				Int("retries", producesRetry).
				Msg("node_complete still missing after re-prompt; engine will fail closed")
			return clarifyPending{}, nil
		}
		prompt := c.agentPrompts(req).OutcomeRetryText()
		chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
		res, err := c.streamChat(chatCtx, acp, req, prompt, nil)
		cancel()
		if err != nil {
			// Propagate transport/API faults so the engine can auto-retry the
			// node; only a successful but empty re-prompt round falls through
			// to fail-closed below.
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
				Msg("node_complete re-prompt failed")
			return clarifyPending{}, fmt.Errorf("agent chat: %w", err)
		}
		absorbChat(usage, byModel, events, res)
		pending := takeClarifyPending(c.host, req.RunID, req.NodeID)
		if pending.any() && nodereg.ClarifyInteractive(req.NodeType) {
			return pending, nil
		}
	}
	return clarifyPending{}, nil
}

// ensureStructured makes a framework node's reserved structured product exist
// before the node completes: it checks the run store and, while absent (or
// only present as an upstream same-name write), re-prompts the agent (same
// session) to call the naming set_* tool, looping up to producesRetry times.
// Intermediate turns are folded into events. Unlike the old produces path
// there is no workspace harvest — structured products are written only
// through MCP.
// For react/approve/preflight nodes, a pending ask_question/ask_form raised
// during the re-prompt aborts the StructuredRetry push and returns those
// interactions. Other callers keep discard-and-continue semantics.
func (c *acpProvider) ensureStructured(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, name, tool string, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) (clarifyPending, error) {
	satisfied := func() bool {
		return artifactOwnedByNode(c.host, req.RunID, req.Token, req.NodeID, name)
	}
	for i := 0; i <= producesRetry; i++ {
		if satisfied() {
			return clarifyPending{}, nil
		}
		if i == producesRetry {
			log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
				Str("artifact", name).Str("tool", tool).
				Int("retries", producesRetry).
				Msg("structured product still missing after re-prompt; engine will fail closed")
			return clarifyPending{}, nil
		}
		prompt := c.agentPrompts(req).StructuredRetryFor(name, tool)
		chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
		res, err := c.streamChat(chatCtx, acp, req, prompt, nil)
		cancel()
		if err != nil {
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
				Str("artifact", name).Msg("structured product re-prompt failed")
			return clarifyPending{}, fmt.Errorf("agent chat: %w", err)
		}
		absorbChat(usage, byModel, events, res)
		pending := takeClarifyPending(c.host, req.RunID, req.NodeID)
		if pending.any() && nodereg.ClarifyInteractive(req.NodeType) {
			return pending, nil
		}

	}
	return clarifyPending{}, nil
}

// artifactOwnedByNode reports whether name exists and its last writer is nodeID.
// Aligns with engine.finalizeProducts so upstream leftovers do not skip re-prompt.
func artifactOwnedByNode(host *mcp.Host, runID, token, nodeID, name string) bool {
	infos, err := host.ListArtifacts(runID, token)
	if err != nil {
		return false
	}
	for _, info := range infos {
		if info.Name == name {
			return info.Node == nodeID
		}
	}
	return false
}

// ensureRequiredProducts re-prompts until every RequiredProducts artifact is
// owned by the current node (not merely present under the same name).
func (c *acpProvider) ensureRequiredProducts(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) (clarifyPending, error) {
	for _, p := range nodereg.RequiredProducts(req.NodeType) {
		pending, err := c.ensureStructured(ctx, req, acp, p.ArtifactName, p.SetTool, events, usage, byModel)
		if pending.any() || err != nil {
			return pending, err
		}
	}
	return clarifyPending{}, nil
}

// ensurePlanComplete drives an implement node's run plan to completion. It
// reads the plan's outstanding items (host.PlanIncomplete); while any remain it
// re-prompts the agent (same session) to finish them, up to max_rounds times.
// A missing/unparseable plan is treated as "nothing to enforce" (nil). If items
// still remain after the loop it returns an error so the engine fails the node.
func (c *acpProvider) ensurePlanComplete(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) error {
	maxRounds := 3
	if mr, ok := toInt(req.Config["max_rounds"]); ok && mr > 0 {
		maxRounds = mr
	}
	for i := 0; i < maxRounds; i++ {
		inc, err := c.host.PlanIncomplete(req.RunID, req.Token)
		if err != nil {

			if err.Error() != "mcp: no plan" {
				log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
					Msg("plan incomplete check failed; skipping enforce")
			}
			return nil
		}
		if len(inc) == 0 {
			return nil
		}
		prompt := c.agentPrompts(req).PlanIncompleteRetryFor(inc)
		chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
		res, err := c.streamChat(chatCtx, acp, req, prompt, nil)
		cancel()
		if err != nil {
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
				Msg("implement plan-complete re-prompt failed")
			return fmt.Errorf("agent chat: %w", err)
		}
		absorbChat(usage, byModel, events, res)
	}
	inc, err := c.host.PlanIncomplete(req.RunID, req.Token)
	if err != nil {
		if err.Error() != "mcp: no plan" {
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
				Msg("plan incomplete final check failed; skipping enforce")
		}
		return nil
	}
	if len(inc) > 0 {
		return fmt.Errorf("计划未全部完成,仍有 %d 项未完成: %s", len(inc), strings.Join(inc, "; "))
	}
	return nil
}

// ensurePreviewRegistered drives an app_preview node to register at least one
// preview port via set_preview, re-prompting up to max_rounds when absent.
func (c *acpProvider) ensurePreviewRegistered(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) error {
	maxRounds := 3
	if mr, ok := toInt(req.Config["max_rounds"]); ok && mr > 0 {
		maxRounds = mr
	}
	for i := 0; i < maxRounds; i++ {
		if c.host.HasPreviewPorts(req.RunID, req.NodeID) {
			return nil
		}
		prompt := c.agentPrompts(req).PreviewRetryText()
		chatCtx, cancel := context.WithTimeout(ctx, c.nodeChatTimeout(req))
		res, err := c.streamChat(chatCtx, acp, req, prompt, nil)
		cancel()
		if err != nil {
			log.Warn().Err(err).Str("node", req.NodeID).Msg("app_preview set_preview re-prompt failed")
			return fmt.Errorf("agent chat: %w", err)
		}
		absorbChat(usage, byModel, events, res)
		_ = takeClarifyPending(c.host, req.RunID, req.NodeID)
	}
	if !c.host.HasPreviewPorts(req.RunID, req.NodeID) {
		return fmt.Errorf("预览契约未满足:未调用 set_preview")
	}
	return nil
}

// nodeNeedsOutcome reports whether this node type must call node_complete.
func nodeNeedsOutcome(nodeType string) bool {
	switch nodeType {
	case "agent", "plan", "implement", "react", "grasp", "approve", "preflight", "research", "proposal",
		"test", "review", "submit_mr", "visual":
		return true

	default:
		return false
	}
}
