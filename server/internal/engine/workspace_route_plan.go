package engine

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

const workspaceAutoRetryCheckpoint = "__workspace_auto_retries__"

// workspaceRoutePlan contains logical data, never nested prepared references.
// One owned OutcomeRef therefore contains everything needed before route-time
// payload columns exist, and portable backups preserve a blocked checkpoint.
type workspaceRoutePlan struct {
	Version              int                           `json:"version"`
	Graph                models.Graph                  `json:"graph"`
	State                models.StateRun               `json:"state"`
	Next                 string                        `json:"next"`
	Status               string                        `json:"status"`
	Attempt              int                           `json:"attempt"`
	Trace                []models.TraceEntry           `json:"trace"`
	Checkpoints          map[string]map[string]any     `json:"checkpoints"`
	Variables            []workspaceCheckpointVariable `json:"variables"`
	Changes              map[string]any                `json:"changes"`
	AutoRetries          map[string]int                `json:"autoRetries"`
	Trigger              finishTrigger                 `json:"trigger"`
	Source               checkpointRunHeader           `json:"source"`
	UsageDelta           *models.TokenUsage            `json:"usageDelta,omitempty"`
	UsageByModelDelta    models.TokenUsageByModel      `json:"usageByModelDelta,omitempty"`
	UsageAlreadyRecorded bool                          `json:"usageAlreadyRecorded,omitempty"`
}

type workspaceCheckpointVariable struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Secret bool   `json:"secret"`
	Value  any    `json:"value"`
	Raw    string `json:"raw"`
}

func copyWorkflowValue[T any](value T) (T, error) {
	var copied T
	body, err := json.Marshal(value)
	if err != nil {
		return copied, err
	}
	err = json.Unmarshal(body, &copied)
	return copied, err
}

// planWorkspaceRoute selects once against the original graph, variable and
// artifact state. It performs no SQL writes; guard artifact reads happen here,
// before the immutable outcome is uploaded and before a metadata transaction.
func (e *Engine) planWorkspaceRoute(c *execCtx, node *models.Node, oc nodeOutcome, opts finishOpts, state models.StateRun, variables []workspaceCheckpointVariable) (workspaceRoutePlan, error) {
	plan := workspaceRoutePlan{Version: 1, Graph: c.graph, State: state, Status: "queued", Attempt: c.run.Attempt, Trace: append([]models.TraceEntry{}, c.run.Trace...), Variables: variables, Changes: map[string]any{}, Trigger: opts.trigger, UsageDelta: oc.usage, UsageByModelDelta: oc.usageByModel}
	plan.UsageAlreadyRecorded = oc.usage != nil && oc.usage.LedgerRecorded
	var err error
	plan.Checkpoints, err = copyWorkflowValue(c.run.Checkpoints)
	if err != nil {
		return plan, err
	}
	if plan.Checkpoints == nil {
		plan.Checkpoints = map[string]map[string]any{}
	}
	plan.AutoRetries = map[string]int{}
	for id, count := range c.autoRetries {
		plan.AutoRetries[id] = count
	}
	shadow := *c
	shadow.vars = map[string]any{}
	for key, value := range c.vars {
		shadow.vars[key] = value
	}
	shadow.nodeOutputs = map[string]map[string]any{}
	for key, value := range c.nodeOutputs {
		shadow.nodeOutputs[key] = value
	}
	if oc.status == "failed" && oc.err != "" {
		shadow.vars["last_error"] = oc.err
		plan.Changes["last_error"] = oc.err
	}
	if oc.status != "failed" {
		shadow.nodeOutputs[node.ID] = oc.outputs
	}
	trace := func(event, detail, target string, kind models.EdgeKind) {
		plan.Trace = append(plan.Trace, models.TraceEntry{NodeID: node.ID, Event: event, Detail: detail, To: target, Kind: kind, At: time.Now().UTC().Format(time.RFC3339)})
	}
	if opts.resumeDetail != "" {
		trace("resume", opts.resumeDetail, "", "")
	}
	if oc.status == "failed" {
		if strings.TrimSpace(oc.err) == "" {
			trace("exit", "执行失败", "", "")
		}
	} else {
		trace("exit", "", "", "")
	}
	rollback := func(edge models.Edge) bool {
		nextAttempt := plan.Attempt + 1
		if edge.MaxAttempts > 0 && nextAttempt > edge.MaxAttempts {
			trace("exit", "rollback attempts exhausted", "", "")
			return false
		}
		plan.Attempt = nextAttempt
		if values, ok := plan.Checkpoints[edge.Target]; ok {
			for key, value := range values {
				plan.Changes[key] = value
				shadow.vars[key] = value
			}
		}
		for _, key := range edge.Carry {
			if _, ok := shadow.vars[key]; !ok {
				plan.Changes[key] = ""
				shadow.vars[key] = ""
			}
		}
		trace("rollback", fmt.Sprintf("attempt=%d 携带 %v 回滚到 checkpoint", plan.Attempt, edge.Carry), edge.Target, models.EdgeRollback)
		plan.Next = edge.Target
		return true
	}
	edges := c.graph.OutEdges(node.ID)
	if oc.status == "failed" {
		for _, edge := range edges {
			if oc.handle != "" {
				if edge.SourceHandle != oc.handle {
					continue
				}
			} else if edge.SourceHandle != "" || edge.KindOrDefault() != models.EdgeRollback {
				continue
			}
			if edge.KindOrDefault() == models.EdgeRollback {
				if rollback(edge) {
					break
				}
				continue
			}
			plan.Next = edge.Target
			trace("transition", "", edge.Target, edge.KindOrDefault())
			break
		}
		if plan.Next == "" && oc.handle == "" {
			for _, edge := range edges {
				if edge.KindOrDefault() == models.EdgeFailure {
					plan.Next = edge.Target
					trace("transition", "", edge.Target, models.EdgeFailure)
					break
				}
			}
		}
		if plan.Next == "" && opts.trigger == triggerAuto && e.AutoRetryMax() > 0 && isAutoRetryable(oc) && plan.AutoRetries[node.ID] < e.AutoRetryMax() {
			plan.AutoRetries[node.ID]++
			plan.Next = node.ID
			trace("resume", fmt.Sprintf("自动从失败位置重试 %d/%d:%s", plan.AutoRetries[node.ID], e.AutoRetryMax(), shortReason(firstNonEmptyStr(oc.err, oc.outputMd))), "", "")
		}
		if plan.Next == "" {
			plan.Status = "failed"
			trace("exit", "no failure transition; run failed", "", "")
		}
	} else {
		ec := e.evalContext(&shadow, outcomeAction(oc))
		for _, edge := range edges {
			if edge.SourceHandle != oc.handle || edge.KindOrDefault() == models.EdgeFailure || !guardPasses(edge.When, ec) {
				continue
			}
			if edge.KindOrDefault() == models.EdgeRollback {
				if rollback(edge) {
					break
				}
				continue
			}
			plan.Next = edge.Target
			trace("transition", "", edge.Target, edge.KindOrDefault())
			break
		}
		if plan.Next == "" {
			plan.Status = "completed"
		}
	}
	if plan.Next != "" {
		plan.Checkpoints[admissionFromCheckpoint] = map[string]any{"node": plan.Next}
	} else {
		delete(plan.Checkpoints, admissionFromCheckpoint)
	}
	if len(plan.AutoRetries) > 0 {
		counts := map[string]any{}
		for key, count := range plan.AutoRetries {
			counts[key] = count
		}
		plan.Checkpoints[workspaceAutoRetryCheckpoint] = counts
	}
	if opts.trigger == triggerConfirm {
		if cp := plan.Checkpoints[services.PendingAutoCheckpoint]; cp != nil && str(cp["node"]) != plan.Next {
			delete(plan.Checkpoints, services.PendingAutoCheckpoint)
		}
	}
	// This is the departing visit's variable snapshot, before a possible
	// rollback restores the successor's variables.
	plan.State.VarsSnapshot = map[string]any{}
	for key, value := range c.vars {
		plan.State.VarsSnapshot[key] = blob.StripDataInValue(value)
	}
	return plan, nil
}
