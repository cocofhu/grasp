package engine

import (
	"context"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// CAPA A7 failure reasons (Demo S2/S3/S4) — keep wording stable for tests/UI.
const (
	errMCPSurfaceEmpty     = "MCP 工具面为空/不可达，无法完成 node_complete"
	errMissingNodeComplete = "未调用 node_complete 标记完成"
	errOutcomeMarkLost     = "node_complete 标记丢失/不可用（产物无法解析或无法采纳）"
)

// consumeNodeOutcome drains the agent's node_complete mark and merges its
// outputs into res. Missing mark or status=failed yields a failed nodeOutcome.
//
// CAPA A7: when the Host memory mark is missing, adopt a parseable
// node_complete.json when present; otherwise classify failure with the evidence
// triple Host Peek ∪ current-iteration StateRun.McpCalls ∪ artifact state —
// never treat "Host buffer currently empty" alone as "tool surface unreachable".
func (e *Engine) consumeNodeOutcome(c *execCtx, node *models.Node, res *runtime.NodeResult) (nodeOutcome, bool) {
	o, ok := e.host.TakeOutcome(c.run.ID, node.ID)
	if !ok {
		if adopted, adoptedOK := e.adoptOutcomeArtifact(c, node); adoptedOK {
			o = adopted
			ok = true
		}
	}
	if !ok {
		errMsg := e.missingOutcomeErr(c, node)
		// True-zero MCP + no artifact usually means the tool surface was
		// unreachable (API/transport blip), not that the agent forgot a call —
		// mark retryable so NodeAutoRetryMax can recover with a fresh attempt.
		retryable := errMsg == errMCPSurfaceEmpty
		return nodeOutcome{
			status:   "failed",
			err:      errMsg,
			outputMd: "节点失败:" + errMsg,
			outputs:  res.Outputs,
			events:   res.Events,
			usage:    res.Usage, usageByModel: res.UsageByModel,
			retryable: retryable,
		}, false
	}
	res.Outputs = mcp.MergeOutcomeOutputs(res.Outputs, o)
	if o.Status == mcp.OutcomeFailed {
		errMsg := strings.TrimSpace(o.Error)
		if errMsg == "" {
			errMsg = strings.TrimSpace(o.Summary)
		}
		if errMsg == "" {
			errMsg = "agent reported failure"
		}
		md := "节点失败:" + errMsg
		if strings.TrimSpace(res.OutputMd) != "" {
			md = res.OutputMd + "\n\n---\n**outcome failed**:" + errMsg
		}
		return nodeOutcome{
			status:   "failed",
			err:      errMsg,
			outputMd: md,
			outputs:  res.Outputs,
			events:   res.Events,
			usage:    res.Usage, usageByModel: res.UsageByModel,
		}, false
	}
	return nodeOutcome{}, true
}

// adoptOutcomeArtifact restores a parseable node_complete.json into the Host
// buffer and returns it for immediate consume. success and failed marks both
// count as "normally finalized" (differ only in result).
func (e *Engine) adoptOutcomeArtifact(c *execCtx, node *models.Node) (mcp.NodeOutcome, bool) {
	if e == nil || e.host == nil || c == nil || node == nil {
		return mcp.NodeOutcome{}, false
	}
	o, state := e.host.PeekOutcomeArtifact(c.run.ID)
	if state != mcp.OutcomeArtifactAdoptable {
		return mcp.NodeOutcome{}, false
	}
	e.host.RestoreOutcomeFromArtifact(c.run.ID, node.ID)
	// Restore leaves the mark in the buffer; drain so callers mirror TakeOutcome.
	taken, ok := e.host.TakeOutcome(c.run.ID, node.ID)
	if !ok {
		taken = o
	}
	log.Info().Str("run_id", c.run.ID).Str("node_id", node.ID).
		Str("status", taken.Status).
		Msg("adopted node_complete.json after Host mark missing")
	return taken, true
}

// currentIterationMcpCalls returns StateRun.McpCalls for the node's latest
// (current) iteration row — durable evidence after flushMcpCalls.
func (e *Engine) currentIterationMcpCalls(runID, nodeID string) []models.McpCall {
	if e == nil || e.db == nil || runID == "" || nodeID == "" {
		return nil
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error; err != nil {
		return nil
	}
	return sr.McpCalls
}

// afterDefaultChecks runs the optional business RPC validator only after
// platform DefaultChecks have already passed (artifacts, test/review gates,
// preview ports, etc.). Default failure paths must not call this.
func (e *Engine) afterDefaultChecks(c *execCtx, node *models.Node, oc nodeOutcome) nodeOutcome {
	if oc.status != "completed" && oc.status != "paused" {
		return oc
	}
	status := mcp.OutcomeSuccess
	if s, _ := oc.outputs["outcome_status"].(string); s == mcp.OutcomeFailed {
		status = mcp.OutcomeFailed
	}
	in := mcp.OutcomeValidateIn{
		RunID:    c.run.ID,
		NodeID:   node.ID,
		NodeType: node.Type,
		Outcome: mcp.NodeOutcome{
			Status:  status,
			Summary: str(oc.outputs["outcome_summary"]),
			Outputs: oc.outputs,
		},
	}
	out, err := e.host.ValidateOutcome(context.Background(), in)
	if err != nil {
		oc.status = "failed"
		oc.err = "业务校验失败:" + err.Error()
		oc.outputMd = "业务校验失败:" + err.Error()
		return oc
	}
	if !out.Accept {
		msg := strings.TrimSpace(out.Message)
		if msg == "" {
			msg = "业务校验未通过"
		}
		oc.status = "failed"
		oc.err = msg
		if strings.TrimSpace(oc.outputMd) != "" {
			oc.outputMd += "\n\n---\n**业务校验未通过**:" + msg
		} else {
			oc.outputMd = "业务校验未通过:" + msg
		}
		return oc
	}
	if len(out.OutputsPatch) > 0 {
		if oc.outputs == nil {
			oc.outputs = map[string]any{}
		}
		for k, v := range out.OutputsPatch {
			oc.outputs[k] = v
		}
	}
	return oc
}

// withOutcome consumes node_complete then runs next (platform DefaultChecks).
// Callers that apply additional gates must invoke afterDefaultChecks themselves
// after those gates; otherwise use finishAgentOutcome.
func (e *Engine) withOutcome(c *execCtx, node *models.Node, res runtime.NodeResult, next func(runtime.NodeResult) nodeOutcome) nodeOutcome {
	// Ownership before TakeOutcome: a Cancel/ResumeFrom zombie that returns from
	// RunAgent after a newer driver claimed the slot must not drain the fresh
	// visit's node_complete mark (host outcomes are keyed only by run+node).
	if c.execGen != 0 && !e.isExecOwner(c.run.ID, c.execGen) {
		return nodeOutcome{
			status:   "cancelled",
			err:      "lost exec ownership",
			outputMd: "dropped late outcome: lost exec ownership",
			events:   res.Events,
			usage:    res.Usage, usageByModel: res.UsageByModel,
		}
	}
	if fail, ok := e.consumeNodeOutcome(c, node, &res); !ok {
		return fail
	}
	oc := next(res)
	if oc.usage == nil {
		oc.usage = res.Usage
	}
	if oc.usageByModel == nil {
		oc.usageByModel = res.UsageByModel
	}
	return oc
}

// finishAgentOutcome runs withOutcome then afterDefaultChecks (Default→RPC).
func (e *Engine) finishAgentOutcome(c *execCtx, node *models.Node, res runtime.NodeResult, next func(runtime.NodeResult) nodeOutcome) nodeOutcome {
	oc := e.withOutcome(c, node, res, next)
	return e.afterDefaultChecks(c, node, oc)
}

// missingOutcomeErr (CAPA A7) picks the failure reason when no adoptable mark
// exists. Evidence triple: Host Peek ∪ StateRun.McpCalls ∪ artifact presence.
// ① true-zero MCP + no artifact → empty surface
// ② MCP evidence but no usable mark → forgot node_complete
// ③ artifact present but corrupt/unadoptable → mark lost
func (e *Engine) missingOutcomeErr(c *execCtx, node *models.Node) string {
	hostCalls := e.host.PeekMcpCalls(c.run.ID, node.ID)
	stateCalls := e.currentIterationMcpCalls(c.run.ID, node.ID)
	_, artState := e.host.PeekOutcomeArtifact(c.run.ID)
	return missingOutcomeErr(hostCalls, stateCalls, artState)
}

// missingOutcomeErr is the pure classifier (testable without Engine).
func missingOutcomeErr(hostCalls, stateCalls []models.McpCall, artState mcp.OutcomeArtifactState) string {
	if artState == mcp.OutcomeArtifactCorrupt {
		return errOutcomeMarkLost
	}
	hasMCP := len(hostCalls) > 0 || len(stateCalls) > 0
	if !hasMCP && artState == mcp.OutcomeArtifactAbsent {
		return errMCPSurfaceEmpty
	}
	// Adoptable artifacts are handled before this helper; if we still see
	// Adoptable here it means adoption failed unexpectedly — treat as mark lost.
	if artState == mcp.OutcomeArtifactAdoptable {
		return errOutcomeMarkLost
	}
	if hasMCP {
		return errMissingNodeComplete
	}
	return errMCPSurfaceEmpty
}
