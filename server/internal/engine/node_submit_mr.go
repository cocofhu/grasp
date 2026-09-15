package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"
)

// setRunBranch records the run's working branch in both the in-memory execCtx
// and its own DB column. Keeping c.run.Branch in sync lets later reads of the
// context see it; the branch is persisted via a scoped column update. An empty
// branch is ignored so it never wipes a branch a prior node already recorded.
func (e *Engine) setRunBranch(c *execCtx, git *runtime.GitInfo) {
	if git == nil || strings.TrimSpace(git.Branch) == "" {
		return
	}
	c.run.Branch = git.Branch
	logDB(e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).UpdateColumn("branch", git.Branch), c.run.ID, "set run branch")
}

// execSubmitMR runs the submit_mr node: an agent (LLM) node whose contract is
// to resolve conflicts against the target branch, push the source branch, and
// open a merge request, then mark completion via node_complete. Platform
// DefaultChecks require the outcome mark only — they do NOT verify git push /
// MR existence / conflicts (those are agent-attested; optional business RPC
// may validate later). On success, outputs.mr_url is exported as the global
// `mr_url` variable for downstream references (e.g. a human_gate body).
//
// config.repo supports three modes (see submit_mr_interp.go):
//   - blank: keep legacy Agent-guided / single-repo fallback (no engine loop)
//   - single: strict-interpolate → ∈repos check → one RunAgent
//   - list ({{vars.repos}}): engine iterates vars.repos by name, fail-fast
func (e *Engine) execSubmitMR(c *execCtx, node *models.Node) nodeOutcome {
	rawRepo := strings.TrimSpace(str(node.Config["repo"]))
	rawSrc := strings.TrimSpace(str(node.Config["source_branch"]))
	rawTgt := strings.TrimSpace(str(node.Config["target_branch"]))

	repoR := e.strictInterpolate(c, rawRepo)
	mode := detectSubmitMRRepoMode(rawRepo, repoR)

	sourceBranch, err := e.resolveStrictBranchField(c, rawSrc, "源分支")
	if err != nil {
		return nodeOutcome{status: "failed", err: err.Error(), outputMd: "提交 MR 失败:" + err.Error()}
	}
	targetBranch, err := e.resolveStrictBranchField(c, rawTgt, "目标分支")
	if err != nil {
		return nodeOutcome{status: "failed", err: err.Error(), outputMd: "提交 MR 失败:" + err.Error()}
	}

	switch mode {
	case submitMRModeBlank:

		return e.runSubmitMROnce(c, node, "", sourceBranch, targetBranch)

	case submitMRModeList:
		if !repoR.ok {
			return nodeOutcome{status: "failed", err: repoR.err, outputMd: "提交 MR 失败:" + repoR.err}
		}
		repos := runtime.ResolveReposFromVars(c.vars)
		if len(repos) == 0 {
			msg := "vars.repos 列表为空或无法解析"
			return nodeOutcome{status: "failed", err: msg, outputMd: "提交 MR 失败:" + msg}
		}
		var (
			lastURL   string
			lastOut   map[string]any
			allEvents []models.AcpEvent
			summaries []string
			lastGit   *runtime.GitInfo
		)
		for _, r := range repos {
			oc, git := e.runSubmitMROnceWithGit(c, node, r.Name, sourceBranch, targetBranch)
			if oc.events != nil {
				allEvents = append(allEvents, oc.events...)
			}
			if oc.status == "failed" {
				errMsg := fmt.Sprintf("仓 %s: %s", r.Name, oc.err)
				if oc.err == "" {
					errMsg = fmt.Sprintf("仓 %s: 提交 MR 失败", r.Name)
				}
				return nodeOutcome{
					status:   "failed",
					err:      errMsg,
					outputMd: "提交 MR 失败:" + errMsg,
					outputs:  oc.outputs,
					events:   allEvents,
				}
			}
			lastOut = oc.outputs
			lastGit = git
			lastURL = strings.TrimSpace(str(oc.outputs["mr_url"]))
			summaries = append(summaries, fmt.Sprintf("- %s: %s", r.Name, lastURL))
		}
		if lastGit != nil {
			e.setRunBranch(c, lastGit)
		}
		c.setVar("mr_url", lastURL)
		e.persistVar(c.run.ID, "mr_url", lastURL)
		md := "已按 vars.repos 逐仓提交 MR:\n" + strings.Join(summaries, "\n")
		return nodeOutcome{status: "completed", outputMd: md, outputs: lastOut, events: allEvents}

	default:
		if !repoR.ok {
			return nodeOutcome{status: "failed", err: repoR.err, outputMd: "提交 MR 失败:" + repoR.err}
		}
		repoName := strings.TrimSpace(repoR.value)
		if repoName == "" || repoName == reposListSentinel {
			msg := "目标仓:原配置非空但插值结果为空"
			return nodeOutcome{status: "failed", err: msg, outputMd: "提交 MR 失败:" + msg}
		}
		if !repoNameInVars(repoName, c.vars) {
			msg := fmt.Sprintf("仓名 %q 不在 vars.repos 中", repoName)
			return nodeOutcome{status: "failed", err: msg, outputMd: "提交 MR 失败:" + msg}
		}
		return e.runSubmitMROnce(c, node, repoName, sourceBranch, targetBranch)
	}
}

// runSubmitMROnce pins repo/source/target on the node request, runs the agent,
// and requires node_complete (no platform verifyMR gate).
func (e *Engine) runSubmitMROnce(c *execCtx, node *models.Node, repo, sourceBranch, targetBranch string) nodeOutcome {
	oc, git := e.runSubmitMROnceWithGit(c, node, repo, sourceBranch, targetBranch)
	if oc.status == "completed" {
		e.setRunBranch(c, git)
		mrURL := strings.TrimSpace(str(oc.outputs["mr_url"]))
		c.setVar("mr_url", mrURL)
		e.persistVar(c.run.ID, "mr_url", mrURL)
	}
	return oc
}

func (e *Engine) runSubmitMROnceWithGit(c *execCtx, node *models.Node, repo, sourceBranch, targetBranch string) (nodeOutcome, *runtime.GitInfo) {
	req := e.nodeReq(c, node)
	if req.Config == nil {
		req.Config = map[string]any{}
	}
	req.Config["repo"] = repo
	req.Config["source_branch"] = sourceBranch
	req.Config["target_branch"] = targetBranch

	res, err := e.provider.RunAgent(context.Background(), req)
	if err != nil {
		return nodeOutcome{status: "failed", err: err.Error(), outputMd: "提交 MR 失败:" + err.Error()}, nil
	}
	oc := e.withOutcome(c, node, res, func(r runtime.NodeResult) nodeOutcome {
		outputs := r.Outputs
		if outputs == nil {
			outputs = map[string]any{}
		}
		mrURL := strings.TrimSpace(str(outputs["mr_url"]))
		md := "已提交 MR"
		if mrURL != "" {
			md = "已提交 MR:" + mrURL
		} else if sum := strings.TrimSpace(str(outputs["outcome_summary"])); sum != "" {
			md = sum
		}
		return nodeOutcome{status: "completed", outputMd: md, outputs: outputs, events: r.Events}
	})
	return oc, res.Git
}

// execProposalSelect resolves a single final proposal from the upstream
// proposals.json. When the configured auto var is truthy it auto-selects the
// recommended option and continues; when only one valid candidate exists it
// auto-adopts that candidate even in manual mode (no pseudo-choice pause);
// otherwise it pauses on a human_gate whose actions are the proposals, and
// ResumeGate finalizes the choice.
func (e *Engine) execProposalSelect(c *execCtx, node *models.Node) nodeOutcome {
	from := firstNonEmptyStr(str(node.Config["from"]), mcp.ProposalsArtifactName)
	content, ok := e.store.Get(c.run.ID, from)
	if !ok {
		return nodeOutcome{status: "failed", err: "未找到上游方案 " + from,
			outputMd: "方案确认失败:未找到上游方案 " + from}
	}
	autoVar := firstNonEmptyStr(str(node.Config["auto_var"]), "auto_confirm")
	outVar := firstNonEmptyStr(str(node.Config["output_var"]), "selected_proposal")
	if truthy(c.vars[autoVar]) {
		final, id, ok := mcp.SelectProposal(content, "")
		if !ok {
			return nodeOutcome{status: "failed", err: "方案解析失败", outputMd: "方案确认失败:方案解析失败"}
		}
		oc := e.finalizeProposal(c, node, final, id, outVar)

		if oc.status == "completed" {
			e.retireGateUpstreamSession(c, node)
		}
		return oc
	}

	// Manual mode: a single valid candidate needs no human pick — adopt it
	// immediately so one-item proposals.json never hangs on “等待人工选择方案”.
	if choices := mcp.ProposalChoices(content); len(choices) == 1 {
		final, id, ok := mcp.SelectProposal(content, choices[0].ID)
		if !ok {
			return nodeOutcome{status: "failed", err: "方案解析失败", outputMd: "方案确认失败:方案解析失败"}
		}
		iter := c.iter[node.ID]
		var pending models.Gate
		if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).
			First(&pending).Error; err == nil && !pending.Resolved {
			pending.Resolved = true
			logDB(e.db.Save(&pending), c.run.ID, "auto-resolve single-candidate proposal_select gate")
		}
		oc := e.finalizeProposal(c, node, final, id, outVar)
		if oc.status == "completed" {
			e.retireGateUpstreamSession(c, node)
		}
		return oc
	}

	iter := c.iter[node.ID]
	var gate models.Gate
	err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).First(&gate).Error
	if err == nil && gate.Resolved {
		return nodeOutcome{status: "completed", outputMd: "方案已选择", outputs: map[string]any{"resolved": true}}
	}
	if err != nil {
		var actions []models.GateAction
		for _, ch := range mcp.ProposalChoices(content) {
			actions = append(actions, models.GateAction{ID: ch.ID, Label: ch.Title})
		}
		gate = models.Gate{RunID: c.run.ID, NodeID: node.ID, Iteration: iter, WorkflowID: c.run.WorkflowID, WorkflowName: c.run.WorkflowName,
			Title:       firstNonEmptyStr(str(node.Config["title"]), "选择方案"),
			BodyMd:      mcp.RenderProposalsMarkdown(content),
			Actions:     actions,
			RequestedAt: time.Now()}
		logDB(e.db.Create(&gate), c.run.ID, "create proposal_select gate")
	}
	return nodeOutcome{status: "paused", outputMd: "等待人工选择方案…"}
}

// finalizeProposal writes the chosen proposal as proposal.json, assigns the
// selected id to the output variable, and returns a completed outcome.
func (e *Engine) finalizeProposal(c *execCtx, node *models.Node, finalJSON, id, outVar string) nodeOutcome {
	if _, err := e.store.Save(c.run.ID, node.ID, mcp.ProposalArtifactName, "json", finalJSON); err != nil {
		return nodeOutcome{status: "failed", err: err.Error(), outputMd: "写入最终方案失败:" + err.Error()}
	}
	c.setVar(outVar, id)
	e.persistVar(c.run.ID, outVar, id)
	outputs := map[string]any{
		"proposal":          mcp.RenderProposalMarkdown(finalJSON),
		"proposal_json":     finalJSON,
		"selected_proposal": id,
		outVar:              id,
	}
	return nodeOutcome{status: "completed", outputMd: "已选定方案 " + id, outputs: outputs}
}

// exportBranchVar publishes an implement node's per-repo working branches to
// the global variable `branches` (JSON name→branch map) so downstream nodes
// can consume it — both in templates ({{vars.branches}}) and, crucially, to
// check each repo out in their fresh sandbox clones (resolveRepos injects
// the branch via GIT_REPOS). Without this a downstream node clones the
// default branch and never sees the implementation.
func (e *Engine) exportBranchVar(c *execCtx, outputs map[string]any) {
	br := strings.TrimSpace(str(outputs["branches"]))
	if br == "" {
		return
	}
	c.setVar("branches", br)
	e.persistVar(c.run.ID, "branches", br)
}

// exportPreflightVars writes each preflight.json field name→value (plaintext,
// including passwords) into run vars. Does not mutate SandboxEnv.
func (e *Engine) exportPreflightVars(c *execCtx) {
	content, ok := e.store.Get(c.run.ID, mcp.PreflightArtifactName)
	if !ok {
		return
	}
	var doc struct {
		Fields []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"fields"`
	}
	if json.Unmarshal([]byte(content), &doc) != nil {
		return
	}
	for _, f := range doc.Fields {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		c.setVar(name, f.Value)
		e.persistVar(c.run.ID, name, f.Value)
	}
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// finalizeAgentProducts finalizes a successful agent/react/approve node: it
// enforces reserved product contracts (and optional extras) when declared,
// auto-captures a generic deliverable when they are not, and records any
// branch the node pushed. A required-but-missing product yields a failed
// outcome — this is a last-resort guard; the provider already re-prompts the
// agent to write it before finishing. When no product contract applies the
// check is skipped entirely and the node just flows through.
func (e *Engine) finalizeAgentProducts(c *execCtx, node *models.Node, res runtime.NodeResult) nodeOutcome {

	if required := nodereg.RequiredProducts(node.Type); len(required) > 0 {
		oc := e.finalizeProducts(c, node, res, required, nodereg.OptionalProducts(node.Type))
		if node.Type == "implement" && oc.status == "completed" {
			e.exportBranchVar(c, oc.outputs)
		}
		if node.Type == "preflight" && oc.status == "completed" {
			e.exportPreflightVars(c)
		}
		return oc
	}
	if produces := str(node.Config["produces"]); produces != "" {
		if _, ok := e.store.Get(c.run.ID, produces); !ok {
			return nodeOutcome{status: "failed", err: "produces contract not satisfied: " + produces,
				outputMd: "产物契约未满足:" + produces, events: res.Events}
		}
	}
	e.captureDeliverable(c, node, res)
	e.setRunBranch(c, res.Git)
	return nodeOutcome{status: "completed", outputMd: res.OutputMd, outputs: res.Outputs, events: res.Events}
}
