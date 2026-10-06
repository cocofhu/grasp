package engine

import (
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"

	"github.com/rs/zerolog/log"
)

// nodeOutcome is the result of executing a single state.
type nodeOutcome struct {
	status   string // completed | failed | paused
	outputMd string
	outputs  map[string]any
	events   []models.AcpEvent
	// usage is this save's token delta (added onto StateRun.Usage). nil skips.
	usage *models.TokenUsage
	// usageByModel is this save's per-model delta (added onto StateRun.UsageByModel).
	usageByModel models.TokenUsageByModel
	err          string
	// handle is the outlet the outcome leaves through, matched against
	// Edge.SourceHandle: a branch case id / "else", a human_gate action id,
	// or pass / fail for verdict Agents. "" is the plain outlet.
	handle string
	// sandboxSetup marks a react node's sandbox/ACP infrastructure failure
	// (distinct from a normal clarify pause or agent execution fault).
	sandboxSetup bool
	// retryable marks a failure the engine may auto-retry from the failure
	// position (see isAutoRetryable / tryAutoRetry). Zero value false means
	// not auto-retryable; only failure construction sites that opt in set true.
	// Display text (err/outputMd) is intentionally decoupled from retryability.
	retryable bool
}

// executeNode dispatches to the per-type executor via the node registry.
func (e *Engine) executeNode(c *execCtx, node *models.Node) nodeOutcome {
	start := time.Now()
	defer func() {
		log.Info().Str("run_id", c.run.ID).Str("node_id", node.ID).
			Str("node_type", node.Type).Int("cost_ms", int(time.Since(start).Milliseconds())).
			Msg("node executed")
	}()

	if err := e.checkAgentProfileProject(c, node); err != nil {
		msg := err.Error()
		return nodeOutcome{status: "failed", err: msg, outputMd: msg}
	}

	spec, ok := nodereg.Get(node.Type)
	if !ok {
		return nodeOutcome{
			status:   "failed",
			err:      fmt.Sprintf("未知节点类型 %q", node.Type),
			outputMd: fmt.Sprintf("执行失败:未知节点类型 %q", node.Type),
		}
	}
	switch spec.Exec {
	case nodereg.ExecInput:
		return e.execInput(c, node)
	case nodereg.ExecOutput:
		return e.execOutput(c, node)
	case nodereg.ExecSetVar:
		return e.execSetVar(c, node)
	case nodereg.ExecBranch:
		return e.execBranch(c, node)
	case nodereg.ExecAgent:
		return e.execAgent(c, node)
	case nodereg.ExecHumanGate:
		return e.execGate(c, node)
	default:
		msg := fmt.Sprintf("节点类型 %q 未配置执行器", node.Type)
		return nodeOutcome{status: "failed", err: msg, outputMd: "执行失败:" + msg}
	}
}

func (e *Engine) execInput(c *execCtx, node *models.Node) nodeOutcome {
	out := map[string]any{"validated": true}
	for k, v := range c.vars {
		out[k] = v
	}
	return nodeOutcome{status: "completed", outputMd: "输入校验通过。", outputs: out}
}

func (e *Engine) execSetVar(c *execCtx, node *models.Node) nodeOutcome {
	assignments, _ := node.Config["assignments"].([]any)
	for _, a := range assignments {
		m, ok := a.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["var"].(string)
		expr, _ := m["expr"].(string)
		if name == "" {
			continue
		}
		v, err := evalExpr(expr, e.evalContext(c, nil))
		if err != nil {
			v = expr
		}
		c.setVar(name, v)
		e.persistVar(c.run.ID, name, v)
	}
	snap := map[string]any{}
	for k, v := range c.vars {
		snap[k] = v
	}
	return nodeOutcome{status: "completed", outputMd: fmt.Sprintf("赋值完成:%v", snap), outputs: map[string]any{"vars": snap}}
}

// branchElse is the handle a branch leaves through when no case matches.
const branchElse = "else"

// execBranch picks the first case whose when-guard passes and leaves through
// the edge whose sourceHandle is that case's id ("else" when none match).
func (e *Engine) execBranch(c *execCtx, node *models.Node) nodeOutcome {
	cases, _ := node.Config["cases"].([]any)
	ec := e.evalContext(c, nil)
	for i, ci := range cases {
		m, ok := ci.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(str(m["id"]))
		when, _ := m["when"].(string)
		if id != "" && guardPasses(when, ec) {
			return nodeOutcome{status: "completed",
				outputMd: fmt.Sprintf("命中分支 #%d(%s)", i+1, id),
				outputs:  map[string]any{"matched": id}, handle: id}
		}
	}
	return nodeOutcome{status: "completed", outputMd: "无分支命中,走 else",
		outputs: map[string]any{"matched": branchElse}, handle: branchElse}
}
