package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	gatenode "github.com/cocofhu/grasp/internal/models/nodereg"

	"github.com/rs/zerolog/log"
)

// GateArtifactSaveResult is returned after a successful human primary-artifact save.
type GateArtifactSaveResult struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	SizeBytes int       `json:"sizeBytes"`
	UpdatedAt time.Time `json:"updatedAt"`
	ETag      string    `json:"etag"`
	NodeID    string    `json:"nodeId"`
	// Content is the normalized payload actually stored (Parse*/indent may differ from the request).
	Content string `json:"content"`
}

// ListGatePrimaryProducts returns the primary artifacts for a pending gate,
// with kind/readonly enriched from the artifact store when present.
func (e *Engine) ListGatePrimaryProducts(runID, gateNodeID string) ([]gatenode.GatePrimaryProduct, error) {
	c, gate, node, err := e.loadPendingGate(runID, gateNodeID)
	if err != nil {
		return nil, err
	}
	_ = gate
	produces := collectUpstreamProduces(c, node)
	items := gatenode.GatePrimaryProducts(node, produces)
	for i := range items {
		if a, ok := e.artifactMeta(runID, items[i].Name); ok && strings.TrimSpace(a.Kind) != "" {
			items[i].Kind = a.Kind
		}
		items[i].Readonly = gatenode.IsReadonlyArtifactKind(items[i].Kind)
	}
	return items, nil
}

// SaveGateArtifact saves a primary artifact while a gate is pending
// (waiting_human, unresolved). It validates via Parse*/basics, upserts the
// artifact store, syncs the bound upstream iteration's nodeExecutions outputs,
// refreshes gate BodyMd, and appends a human-edit trace event.
//
// ifMatch, when non-empty, must equal the current ETag or the save is rejected
// with a conflict error (last-write-wins still applies when ifMatch is omitted).
func (e *Engine) SaveGateArtifact(runID, gateNodeID, name, content, ifMatch string) (*GateArtifactSaveResult, error) {
	if e.IsHalted() {
		return nil, errors.New("server is shutting down")
	}
	unlock := e.lockResume(runID + ":" + gateNodeID + ":artifact")
	defer unlock()

	c, gate, node, err := e.loadPendingGate(runID, gateNodeID)
	if err != nil {
		return nil, err
	}

	produces := collectUpstreamProduces(c, node)
	if !gatenode.GateAllowsArtifact(node, name, produces) {
		return nil, fmt.Errorf("产物 %q 不是该门禁的主产物，不可编辑", name)
	}

	storeKind := ""
	if a, ok := e.artifactMeta(runID, name); ok {
		storeKind = a.Kind
	}
	if gatenode.IsNonTextPrimary(name, storeKind) {
		return nil, fmt.Errorf("产物 %q 为非文本主产物，只读不可保存", name)
	}

	norm, err := mcp.ValidateHumanArtifactContent(name, content)
	if err != nil {
		return nil, err
	}

	// Optional optimistic concurrency: compare against current store content.
	if ifMatch != "" {
		if cur, ok := e.store.Get(runID, name); ok {
			curETag := artifactETag(cur, 0)
			// Prefer DB metadata when available.
			if a, aok := e.artifactMeta(runID, name); aok {
				curETag = artifactETag(a.Content, a.SizeBytes)
				if !a.UpdatedAt.IsZero() {
					curETag = artifactETagWithTime(a.Content, a.UpdatedAt)
				}
			}
			if ifMatch != curETag {
				return nil, errArtifactConflict
			}
		}
	}

	id, err := e.store.Save(runID, node.ID, norm.Name, norm.Kind, norm.Content)
	if err != nil {
		return nil, err
	}

	// Sync upstream iteration snapshot so preview + {{nodes.*.outputs.*}} see the edit.
	e.syncUpstreamOutputs(c, &gate, node, norm)

	// Refresh BodyMd from template so fallback markdown preview stays current.
	if bt, _ := node.Config["body_template"].(string); strings.TrimSpace(bt) != "" {
		// Reload outputs into ctx after sync (loadCtx picks latest per node).
		if c2, err2 := e.loadCtx(runID); err2 == nil {
			gate.BodyMd = e.interpolate(c2, bt)
			logDB(e.db.Save(&gate), runID, "refresh gate body after human artifact edit")
		}
	} else if node.Type == "proposal_select" && norm.Name == firstNonEmptyStr(str(node.Config["from"]), mcp.ProposalsArtifactName) {
		gate.BodyMd = mcp.RenderProposalsMarkdown(norm.Content)
		logDB(e.db.Save(&gate), runID, "refresh proposal_select body after human artifact edit")
	}

	detail := fmt.Sprintf("人改产物 name=%s kind=%s size=%d reviewer=operator", norm.Name, norm.Kind, len(norm.Content))
	e.appendTrace(c, models.TraceEntry{NodeID: gateNodeID, Event: "artifact_edit", Detail: detail})

	// Notify UI subscribers (run detail / inbox) to refresh.
	msg, _ := json.Marshal(map[string]any{
		"type": "artifact_edit", "runId": runID, "nodeId": gateNodeID, "name": norm.Name,
	})
	e.broker.Publish(runID, msg)

	a, ok := e.artifactMeta(runID, name)
	updatedAt := time.Now()
	size := len(norm.Content)
	if ok {
		if !a.UpdatedAt.IsZero() {
			updatedAt = a.UpdatedAt
		} else if !a.CreatedAt.IsZero() {
			updatedAt = a.CreatedAt
		}
		size = a.SizeBytes
		id = a.ID
	}
	etag := artifactETagWithTime(norm.Content, updatedAt)
	log.Info().Str("run_id", runID).Str("gate", gateNodeID).Str("artifact", name).Int("size", size).
		Msg("human gate artifact saved")

	return &GateArtifactSaveResult{
		ID: id, Name: norm.Name, Kind: norm.Kind, SizeBytes: size,
		UpdatedAt: updatedAt, ETag: etag, NodeID: node.ID, Content: norm.Content,
	}, nil
}

// errArtifactConflict is returned when If-Match does not match current ETag.
var errArtifactConflict = errors.New("artifact was updated externally; refresh and retry")

func (e *Engine) loadPendingGate(runID, gateNodeID string) (*execCtx, models.Gate, *models.Node, error) {
	c, err := e.loadCtx(runID)
	if err != nil {
		return nil, models.Gate{}, nil, err
	}
	switch c.run.Status {
	case "cancelled", "failed", "completed":
		return nil, models.Gate{}, nil, errors.New("run already ended")
	case "waiting_human":
		// ok
	default:
		return nil, models.Gate{}, nil, fmt.Errorf("run 状态 %q 不允许编辑产物（需要 waiting_human）", c.run.Status)
	}
	node := c.graph.FindNode(gateNodeID)
	if node == nil || (node.Type != "human_gate" && node.Type != "proposal_select") {
		return nil, models.Gate{}, nil, errors.New("gate node not found")
	}
	var gate models.Gate
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, gateNodeID).Order("iteration desc, id desc").First(&gate).Error; err != nil {
		return nil, models.Gate{}, nil, errors.New("no pending gate")
	}
	if gate.Resolved {
		return nil, models.Gate{}, nil, errors.New("gate already resolved")
	}
	return c, gate, node, nil
}

func collectUpstreamProduces(c *execCtx, gateNode *models.Node) []string {
	var names []string
	seen := map[string]bool{}
	for _, p := range gatenode.GatePrimaryProducts(gateNode, nil) {
		if p.NodeID == "" {
			continue
		}
		up := c.graph.FindNode(p.NodeID)
		if up == nil {
			continue
		}
		if prod, _ := up.Config["produces"].(string); strings.TrimSpace(prod) != "" {
			for _, part := range strings.Split(prod, ",") {
				n := strings.TrimSpace(part)
				if n == "" || seen[n] {
					continue
				}
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	return names
}

func (e *Engine) syncUpstreamOutputs(c *execCtx, gate *models.Gate, gateNode *models.Node, norm mcp.HumanArtifactNormalized) {
	// Resolve which upstream node+iteration owns this product.
	upNodeID := gate.UpstreamNodeID
	upIter := gate.UpstreamIteration
	products := gatenode.GatePrimaryProducts(gateNode, nil)
	for _, p := range products {
		if p.Name != norm.Name {
			continue
		}
		if p.NodeID != "" {
			upNodeID = p.NodeID
		}
		break
	}
	if upNodeID == "" {
		// artifact()-only refs: store is enough (interpolate reads Artifact()).
		return
	}
	if upIter <= 0 {
		upIter = c.iter[upNodeID]
	}
	if upIter <= 0 {
		return
	}

	var sr models.StateRun
	if err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, upNodeID, upIter).First(&sr).Error; err != nil {
		log.Warn().Str("run_id", c.run.ID).Str("node_id", upNodeID).Int("iter", upIter).
			Err(err).Msg("sync upstream outputs: state_run not found")
		return
	}
	outs := sr.Outputs
	if outs == nil {
		outs = map[string]any{}
	}
	outKey := norm.OutKey
	if outKey == "" {
		outKey = gatenode.ArtifactToOutputKey[norm.Name]
	}
	if outKey == "page" || norm.Name == "page.html" {
		outs["page"] = norm.Content
	} else if outKey != "" {
		outs[outKey] = norm.Rendered
		jsonKey := norm.JSONKey
		if jsonKey == "" {
			jsonKey = outKey + "_json"
		}
		outs[jsonKey] = norm.Content
	} else {
		// Freeform named artifact: stash under a stable key for debugging.
		outs["artifact_"+norm.Name] = norm.Content
	}
	sr.Outputs = outs
	logDB(e.db.Save(&sr), c.run.ID, "sync upstream outputs after human artifact edit")
	c.nodeOutputs[upNodeID] = outs
}

func (e *Engine) artifactMeta(runID, name string) (models.Artifact, bool) {
	var a models.Artifact
	if err := e.db.Where("run_id = ? AND name = ?", runID, name).First(&a).Error; err != nil {
		return a, false
	}
	return a, true
}

func artifactETag(content string, size int) string {
	sum := sha256.Sum256([]byte(content))
	if size <= 0 {
		size = len(content)
	}
	return fmt.Sprintf("W/\"%d-%s\"", size, hex.EncodeToString(sum[:8]))
}

func artifactETagWithTime(content string, t time.Time) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("W/\"%d-%s\"", t.UnixNano(), hex.EncodeToString(sum[:8]))
}

// ArtifactETag returns the concurrency token for an artifact payload.
// Prefer UpdatedAt when present so it matches SaveGateArtifact / If-Match.
func ArtifactETag(content string, sizeBytes int, updatedAt time.Time) string {
	if !updatedAt.IsZero() {
		return artifactETagWithTime(content, updatedAt)
	}
	return artifactETag(content, sizeBytes)
}

// IsArtifactConflict reports whether err is an external-update conflict.
func IsArtifactConflict(err error) bool {
	return errors.Is(err, errArtifactConflict)
}

// syncAfterPrimaryArtifactWrite is the MCP WriteArtifact post-Save hook.
// Every successful write publishes artifact_edit (name included) so the ReAct
// preview tab can hot-reload. Mapped primary products (page.html, research.json, …)
// during review or waiting_human additionally sync StateRun.outputs and refresh
// pending gate BodyMd — symmetric with SaveGateArtifact. Non-primary names and
// in-flight first runs skip that output sync. Save failures never reach this hook.
// The opening Publish is the only artifact_edit for this write (do not emit a
// second nameless frame — busy UIs would refetch the artifact list twice).
func (e *Engine) syncAfterPrimaryArtifactWrite(runID, nodeID, name, content, kind string) {
	if nodeID != "" {
		e.broker.Publish(runID, artifactEditMsg(runID, nodeID, name, ""))
	}
	outKey, mapped := gatenode.ArtifactToOutputKey[name]
	if !mapped || nodeID == "" {
		return
	}
	// Scope: review phase or run already waiting on human. Skip ordinary
	// first-pass agent writes so we do not race finalize*/saveState.
	if !e.host.InReviewPhase(runID) {
		var run models.Run
		if err := e.db.Select("status").Where("id = ?", runID).First(&run).Error; err != nil || run.Status != "waiting_human" {
			return
		}
	}

	var sr models.StateRun
	// Prefer the active node's current iteration row; fall back to latest.
	err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&sr).Error
	if err != nil {
		return
	}
	if sr.Status == "failed" || sr.Status == "cancelled" {
		return
	}

	outs := sr.Outputs
	if outs == nil {
		outs = map[string]any{}
	}
	if outKey == "page" || name == "page.html" {
		outs["page"] = content
	} else {
		// Structured products: keep raw JSON + rendered markdown when possible.
		outs[outKey+"_json"] = content
		if render := structuredRenderForArtifact(name); render != nil {
			outs[outKey] = render(content)
		}
	}
	sr.Outputs = outs
	logDB(e.db.Save(&sr), runID, "sync outputs after WriteArtifact")

	c, err := e.loadCtx(runID)
	if err != nil {
		return
	}
	c.nodeOutputs[nodeID] = outs
	e.refreshPendingGatesForProducer(c, nodeID)
	_ = kind // kind reserved for future etag/content-type signals
}

// structuredRenderForArtifact returns a markdown renderer for known structured
// artifact names, or nil when the artifact has no companion markdown output.
func structuredRenderForArtifact(name string) func(string) string {
	switch name {
	case mcp.ResearchArtifactName:
		return mcp.RenderResearchMarkdown
	case mcp.ProposalsArtifactName:
		return mcp.RenderProposalsMarkdown
	case mcp.PlanArtifactName:
		return mcp.RenderPlanMarkdown
	case mcp.ReviewArtifactName:
		return mcp.RenderReviewMarkdown
	case mcp.TestResultArtifactName:
		return mcp.RenderTestResultMarkdown
	case mcp.ClarifiedRequirementArtifactName:
		return mcp.RenderClarifiedRequirementMarkdown
	case mcp.ImplementationResultArtifactName:
		return mcp.RenderImplementationResultMarkdown
	case mcp.ProposalArtifactName:
		return mcp.RenderProposalMarkdown
	default:
		return nil
	}
}
