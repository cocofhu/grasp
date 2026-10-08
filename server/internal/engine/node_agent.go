package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/mcp/structured"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/rs/zerolog/log"
)

// Outlet handles of an Agent whose capabilities declare verdict products.
const (
	handlePass = "pass"
	handleFail = "fail"
)

// execAgent runs an Agent node. The capability snapshot decides the shape:
// clarify Agents open a human dialogue, auto Agents run to node_complete and
// optionally enter post-run review.
func (e *Engine) execAgent(c *execCtx, node *models.Node) nodeOutcome {
	if node.Caps == nil {
		msg := fmt.Sprintf("Agent %s 未声明能力", agentProfile(node))
		return nodeOutcome{status: "failed", err: msg, outputMd: "执行失败:" + msg}
	}
	if node.Caps.Clarify() {
		return e.execClarifyEnter(c, node)
	}
	oc := e.runAutoAgent(c, node)
	if oc.status == "completed" && node.Caps.ReviewEnabled() {
		return e.enterReview(c, node, oc)
	}
	return oc
}

func (e *Engine) runAutoAgent(c *execCtx, node *models.Node) nodeOutcome {
	req := e.nodeReq(c, node)
	res, err := e.provider.RunAgent(context.Background(), req)
	if err != nil {
		return nodeOutcome{status: "failed", err: err.Error(),
			outputMd: "Agent 执行失败:" + err.Error(), retryable: true, events: res.Events, usage: res.Usage, usageByModel: res.UsageByModel}
	}
	return e.finishAgentOutcome(c, node, res, func(r runtime.NodeResult) nodeOutcome {
		return e.finalizeAgent(c, node, r)
	})
}

func agentProfile(node *models.Node) string {
	if p := strings.TrimSpace(str(node.Config["agent_profile"])); p != "" {
		return p
	}
	return node.ID
}

// finalizeAgent derives an Agent node's outcome from the products it owns:
// required products must have been written by this node, every declared
// product present is lifted into outputs, schema export hooks run, and the
// verdict products (if any) decide the pass / fail outlet. Shared by the
// initial run, clarify confirm and review confirm so all paths agree.
func (e *Engine) finalizeAgent(c *execCtx, node *models.Node, res runtime.NodeResult) nodeOutcome {
	caps := node.Caps
	outputs := res.Outputs
	if outputs == nil {
		outputs = map[string]any{}
	}
	for _, sc := range nodereg.RequiredSchemas(caps, e.runWorkKind(c.run.ID)) {
		if _, ok := e.artifactDeliveredThisVisit(c, node, sc.ArtifactName); !ok {
			msg := "产物契约未满足:本次执行未写入 " + sc.ArtifactName
			return nodeOutcome{status: "failed", err: msg, outputMd: msg, events: res.Events}
		}
	}
	declared := nodereg.DeclaredSchemas(caps)
	for _, sc := range declared {
		if content, ok := e.artifactDeliveredThisVisit(c, node, sc.ArtifactName); ok {
			e.liftSchema(c, node, outputs, sc, content)
		}
	}
	if len(declared) == 0 {
		e.captureDeliverable(c, node, res)
	}
	e.setRunBranch(c, res.Git)
	if caps.CommitsCode() {
		e.exportBranchVar(c, outputs)
	}
	if caps.WritesSchema(models.SchemaPreflight) {
		e.exportPreflightVars(c)
	}
	oc := nodeOutcome{status: "completed", outputMd: res.OutputMd, outputs: outputs, events: res.Events}
	return e.applyVerdict(c, node, oc)
}

// runWorkKind reads work_kind from the run's clarified requirement ("" when
// absent), which decides whether root_cause is required.
func (e *Engine) runWorkKind(runID string) string {
	content, ok := e.store.Get(runID, mcp.ClarifiedRequirementArtifactName)
	if !ok {
		return ""
	}
	return structured.ClarifiedWorkKind(content)
}

// artifactDeliveredThisVisit returns content when this execution itself wrote
// name. page.html keeps the last-writer rule: its per-iteration snapshot is
// outputs.page and is outside the conclusion-isolation fix. JSON conclusions
// count only when the visit's revision moved or WriteArtifact ran after entry.
func (e *Engine) artifactDeliveredThisVisit(c *execCtx, node *models.Node, name string) (string, bool) {
	content, owned := e.artifactOwnedByNode(c.run.ID, node.ID, name)
	if !owned {
		return "", false
	}
	if name == mcp.PageArtifactName {
		return content, true
	}
	e.ensureArtifactVisit(c, node)
	if e.host.VisitNoted(c.run.ID, node.ID) && !e.host.FreshThisVisit(c.run.ID, node.ID, name) {
		return "", false
	}
	return content, true
}

// ensureArtifactVisit reloads the revision baseline persisted at startNodeRun
// when this process did not record the visit itself.
func (e *Engine) ensureArtifactVisit(c *execCtx, node *models.Node) {
	if e == nil || e.host == nil || c == nil || node == nil {
		return
	}
	if e.host.VisitNoted(c.run.ID, node.ID) {
		return
	}
	iter := c.iter[node.ID]
	if iter < 1 {
		return
	}
	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).
		First(&sr).Error; err != nil || !sr.ArtifactBaseSet {
		return
	}
	e.host.RestoreArtifactVisit(c.run.ID, node.ID, sr.ArtifactBaseRev, sr.ArtifactVisitWrites)
}

// artifactOwnedByNode returns content only when the named artifact exists and
// its last writer is nodeID: an upstream product with the same name never
// satisfies this node's contract.
func (e *Engine) artifactOwnedByNode(runID, nodeID, name string) (string, bool) {
	content, ok := e.store.Get(runID, name)
	if !ok {
		return "", false
	}
	for _, info := range e.store.List(runID) {
		if info.Name == name {
			return content, info.Node == nodeID
		}
	}
	return "", false
}

// liftSchema exposes a product in the node outputs: rendered markdown at the
// schema key and raw JSON at key+"_json". page.html is re-saved as html (plus
// a node-scoped copy) so previews keep one source per node.
func (e *Engine) liftSchema(c *execCtx, node *models.Node, outputs map[string]any, sc nodereg.Schema, content string) {
	if sc.Name == models.SchemaPage {
		if _, err := e.store.Save(c.run.ID, node.ID, mcp.PageArtifactName, "html", content); err != nil {
			log.Warn().Err(err).Str("node", node.ID).Msg("page re-save failed")
		}
		if _, err := e.store.Save(c.run.ID, node.ID, visualNodePageName(node.ID), "html", content); err != nil {
			log.Warn().Err(err).Str("node", node.ID).Msg("node-scoped page save failed")
		}
		outputs[sc.OutputKey()] = content
		return
	}
	if sc.Render != nil {
		outputs[sc.OutputKey()] = sc.Render(content)
	} else {
		outputs[sc.OutputKey()] = content
	}
	outputs[sc.OutputKey()+"_json"] = content
}

// applyVerdict judges a completed outcome by every verdict product the Agent
// declares (all must pass). It selects the pass / fail outlet, records the
// reason in vars.reason, and turns a failing verdict into a failed outcome so
// the fail edge (or the run failure) applies. Agents without verdict products
// are left untouched.
func (e *Engine) applyVerdict(c *execCtx, node *models.Node, oc nodeOutcome) nodeOutcome {
	verdicts := nodereg.VerdictSchemas(node.Caps)
	if oc.status != "completed" || len(verdicts) == 0 {
		return oc
	}
	planJSON, _ := e.store.Get(c.run.ID, mcp.PlanArtifactName)
	var reasons []string
	for _, sc := range verdicts {
		content, _ := e.store.Get(c.run.ID, sc.ArtifactName)
		if pass, reason := sc.Verdict(content, planJSON); !pass {
			reasons = append(reasons, reason)
		}
	}
	if oc.outputs == nil {
		oc.outputs = map[string]any{}
	}
	reason := "验证全部通过"
	if len(reasons) == 0 {
		oc.handle = handlePass
	} else {
		reason = strings.Join(reasons, ";")
		oc.handle = handleFail
		oc.status = "failed"
		oc.err = reason
		if strings.TrimSpace(oc.outputMd) != "" {
			oc.outputMd += "\n\n---\n**验证未通过**:" + reason
		} else {
			oc.outputMd = "验证未通过:" + reason
		}
	}
	oc.outputs["action"] = oc.handle
	c.setVar("reason", reason)
	e.persistVar(c.run.ID, "reason", reason)
	return oc
}

// setRunBranch records the run's working branch in both the in-memory execCtx
// and its own DB column. An empty branch never wipes one a prior node recorded.
func (e *Engine) setRunBranch(c *execCtx, git *runtime.GitInfo) {
	if git == nil || strings.TrimSpace(git.Branch) == "" {
		return
	}
	c.run.Branch = git.Branch
	logDB(e.db.Model(&models.Run{}).Where("id = ?", c.run.ID).UpdateColumn("branch", git.Branch), c.run.ID, "set run branch")
}

// exportBranchVar publishes the per-repo working branches as vars.branches
// (JSON name→branch map), so downstream sandboxes check the implementation
// out instead of cloning the default branch.
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
