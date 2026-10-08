package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/mcp/structured"
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
// For clarify Agents, a pending ask_question raised during the re-prompt aborts
// the completion push and returns those questions (caller must not discard).
// Auto Agents keep the discard-and-continue semantics.
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
	retries := nudgeRetries(req, false)
	for i := 0; i <= retries; i++ {
		if outcomeReady() {
			return clarifyPending{}, nil
		}
		if i == retries {
			log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
				Int("retries", retries).
				Msg("node_complete still missing after re-prompt; engine will fail closed")
			return clarifyPending{}, nil
		}
		prompt := models.OutcomeRetry
		chatCtx, cancel := c.turnCtx(ctx, req)
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
		if pending.any() && req.Caps.Clarify() {
			return pending, nil
		}
	}
	return clarifyPending{}, nil
}

// ensureStructured makes an Agent's required product exist
// before the node completes: it checks the run store and, while absent (or
// only present as an upstream same-name write), re-prompts the agent (same
// session) to call the naming set_* tool, looping up to nudgeRetries times.
// Intermediate turns are folded into events. Unlike the old produces path
// there is no workspace harvest — structured products are written only
// through MCP.
// For clarify Agents, a pending ask_question/ask_form raised
// during the re-prompt aborts the StructuredRetry push and returns those
// interactions. Other callers keep discard-and-continue semantics.
func (c *acpProvider) ensureStructured(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, name, tool string, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) (clarifyPending, error) {
	satisfied := func() bool {
		return artifactOwnedByNode(c.host, req.RunID, req.Token, req.NodeID, name)
	}
	retries := nudgeRetries(req, false)
	for i := 0; i <= retries; i++ {
		if satisfied() {
			return clarifyPending{}, nil
		}
		if i == retries {
			log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
				Str("artifact", name).Str("tool", tool).
				Int("retries", retries).
				Msg("structured product still missing after re-prompt; engine will fail closed")
			return clarifyPending{}, nil
		}
		prompt := models.StructuredRetryFor(name, tool)
		chatCtx, cancel := c.turnCtx(ctx, req)
		res, err := c.streamChat(chatCtx, acp, req, prompt, nil)
		cancel()
		if err != nil {
			log.Warn().Err(err).Str("run", req.RunID).Str("node", req.NodeID).
				Str("artifact", name).Msg("structured product re-prompt failed")
			return clarifyPending{}, fmt.Errorf("agent chat: %w", err)
		}
		absorbChat(usage, byModel, events, res)
		pending := takeClarifyPending(c.host, req.RunID, req.NodeID)
		if pending.any() && req.Caps.Clarify() {
			return pending, nil
		}

	}
	return clarifyPending{}, nil
}

// artifactOwnedByNode reports whether name exists and its last writer is nodeID.
// Aligns with engine.finalizeAgent so upstream leftovers do not skip re-prompt.
// When the engine has noted this visit's revision baseline, a JSON conclusion
// left by an earlier execution of the same node does not count — the agent
// must write it again. page.html stays on the last-writer rule.
func artifactOwnedByNode(host *mcp.Host, runID, token, nodeID, name string) bool {
	if host == nil {
		return false
	}
	infos, err := host.ListArtifacts(runID, token)
	if err != nil {
		return false
	}
	owned := false
	for _, info := range infos {
		if info.Name == name {
			owned = info.Node == nodeID
			break
		}
	}
	if !owned {
		return false
	}
	if name == mcp.PageArtifactName || !host.VisitNoted(runID, nodeID) {
		return true
	}
	return host.FreshThisVisit(runID, nodeID, name)
}

// ensureRequiredProducts re-prompts until every required product is owned by
// the current node (not merely present under the same name).
func (c *acpProvider) ensureRequiredProducts(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) (clarifyPending, error) {
	for _, s := range nodereg.RequiredSchemas(req.Caps, "") {
		tool := s.SetTool
		if tool == "" {
			tool = "write_artifact"
		}
		pending, err := c.ensureStructured(ctx, req, acp, s.ArtifactName, tool, events, usage, byModel)
		if pending.any() || err != nil {
			return pending, err
		}
	}
	if req.Caps.WritesSchema(models.SchemaRootCause) {
		return c.ensureRootCauseConsistency(ctx, req, acp, events, usage, byModel)
	}
	return clarifyPending{}, nil
}

// ensureRootCauseConsistency enforces the work_kind ↔ root_cause.json rule for
// Agents that declare root_cause: bug work needs a valid report; other work
// must not keep one.
func (c *acpProvider) ensureRootCauseConsistency(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) (clarifyPending, error) {
	reason := func() string {
		cr, err := c.host.ReadArtifact(req.RunID, req.Token, mcp.ClarifiedRequirementArtifactName)
		if err != nil {
			return "" // required clarified product handled separately
		}
		wk := mcp.ClarifiedWorkKind(cr)
		hasRC := artifactOwnedByNode(c.host, req.RunID, req.Token, req.NodeID, mcp.RootCauseArtifactName)
		if wk == "" {
			return "需求缺少 work_kind(bug|feature|other)。请立即调用 set_clarified_requirement 补上工作类型;若为 bug 还需 set_root_cause 写入 root_cause.json。"
		}
		if wk == "bug" {
			if !hasRC {
				return "工作类型为 bug,尚未写入 root_cause.json。请立即调用 set_root_cause 提交通过校验的根因报告(含证据与至少一张图),不要写成 page.html。"
			}
			raw, rerr := c.host.ReadArtifact(req.RunID, req.Token, mcp.RootCauseArtifactName)
			if rerr != nil {
				return "工作类型为 bug,但 root_cause.json 无法读取。请重新调用 set_root_cause。"
			}
			if perr := parseRootCauseJSON(raw); perr != nil {
				return "root_cause.json 未通过校验:" + perr.Error() + "。请用 set_root_cause 重新写入合法报告。"
			}
			return ""
		}
		// feature|other: leftover report blocks confirm.
		if hasRC || artifactPresent(c.host, req.RunID, req.Token, mcp.RootCauseArtifactName) {
			c.host.DeleteArtifact(req.RunID, mcp.RootCauseArtifactName)
			if artifactPresent(c.host, req.RunID, req.Token, mcp.RootCauseArtifactName) {
				return "工作类型为 " + wk + ",不得保留 root_cause.json。请把 work_kind 保持为非 bug,并去掉根因产物(重新 set_clarified_requirement 为非 bug 时平台会尝试清除;若仍在请勿再写 set_root_cause)。"
			}
		}
		return ""
	}

	retries := nudgeRetries(req, false)
	for i := 0; i <= retries; i++ {
		msg := reason()
		if msg == "" {
			return clarifyPending{}, nil
		}
		if i == retries {
			log.Warn().Str("run", req.RunID).Str("node", req.NodeID).
				Str("reason", msg).
				Msg("root_cause consistency still failing after re-prompt")
			return clarifyPending{}, fmt.Errorf("%s", msg)
		}
		chatCtx, cancel := c.turnCtx(ctx, req)
		res, err := c.streamChat(chatCtx, acp, req, "【必须完成】"+msg, nil)
		cancel()
		if err != nil {
			return clarifyPending{}, fmt.Errorf("agent chat: %w", err)
		}
		absorbChat(usage, byModel, events, res)
		pending := takeClarifyPending(c.host, req.RunID, req.NodeID)
		if pending.any() && req.Caps.Clarify() {
			return pending, nil
		}
	}
	return clarifyPending{}, nil
}

func artifactPresent(host *mcp.Host, runID, token, name string) bool {
	infos, err := host.ListArtifacts(runID, token)
	if err != nil {
		return false
	}
	for _, info := range infos {
		if info.Name == name {
			return true
		}
	}
	return false
}

func parseRootCauseJSON(raw string) error {
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return err
	}
	_, err := structured.ParseRootCause(args)
	return err
}

// ensurePlanComplete drives the run plan to completion for an Agent that tracks plan progress. It
// reads the plan's outstanding items (host.PlanIncomplete); while any remain it
// re-prompts the agent (same session) to finish them, up to nudgeRetries times.
// A missing/unparseable plan is treated as "nothing to enforce" (nil). If items
// still remain after the loop it returns an error so the engine fails the node.
func (c *acpProvider) ensurePlanComplete(ctx context.Context, req NodeReq, acp *sandbox.ACPClient, events *[]models.AcpEvent, usage **models.TokenUsage, byModel *models.TokenUsageByModel) error {
	maxRounds := nudgeRetries(req, true)
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
		prompt := models.PlanIncompleteRetryFor(inc)
		chatCtx, cancel := c.turnCtx(ctx, req)
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
