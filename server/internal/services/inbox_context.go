package services

import (
	"regexp"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
)

var gateBodyNodeRef = regexp.MustCompile(`\{\{\s*nodes\.([^.}\s]+)\.outputs\.`)

// Matches nodes.<id>.outputs. whether or not wrapped in {{ }}.
var clarifyNodeOutputRef = regexp.MustCompile(`nodes\.([^.}\s"']+)\.outputs\.`)

// Matches artifact("name") / artifact('name') whether or not wrapped in {{ }}.
var clarifyArtifactRef = regexp.MustCompile(`artifact\s*\(\s*["']([^"']+)["']\s*\)`)

// Inbox context kinds returned by InboxContextKind.
const (
	InboxKindGate = "gate"
	// InboxKindClarify is a react/approve node parked on a live conversation.
	InboxKindClarify = "clarify"
	// InboxKindClarifyStarting is an approve node whose sandbox is still booting:
	// listed in the inbox as a loading card, but with no conversation to serve.
	InboxKindClarifyStarting = "clarify_starting"
)

// InboxContextKind reports whether run+node+iteration is a pending gate, a
// parked clarify item, or a still-booting approve node. Gate takes priority when
// both could match. Returns ("", false) when nothing is pending.
func (s *RunService) InboxContextKind(runID, nodeID string, iteration int) (string, bool) {
	var gate models.Gate
	if err := pendingGateScope(s.db).
		Where("gates.run_id = ? AND gates.node_id = ? AND gates.iteration = ?",
			runID, nodeID, iteration).
		First(&gate).Error; err == nil {
		return InboxKindGate, true
	}
	if s.isPendingClarification(runID, nodeID, iteration) {
		return InboxKindClarify, true
	}
	// A booting approve node has no conversation yet, but its loading card is
	// already listed in the inbox, so its context must resolve too. The distinct
	// kind is what lets the handler serve the starting shell without re-deriving
	// (and re-querying) the same verdict.
	if s.IsStartingApprove(runID, nodeID, iteration) {
		return InboxKindClarifyStarting, true
	}
	return "", false
}

// IsStartingApprove reports whether run+node+iteration is an approve node whose
// sandbox is still booting: StateRun is running and no conversation exists yet.
func (s *RunService) IsStartingApprove(runID, nodeID string, iteration int) bool {
	var run models.Run
	if err := s.db.Select("id", "status").First(&run, "id = ?", runID).Error; err != nil {
		return false
	}
	if containsString(terminalRunStatuses, run.Status) {
		return false
	}
	var n int64
	s.db.Model(&models.ReactConversation{}).
		Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iteration).Count(&n)
	if n > 0 {
		return false
	}
	var sr models.StateRun
	if err := s.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iteration).
		Order("id desc").First(&sr).Error; err != nil {
		return false
	}
	return nodereg.IsGrasp(sr.NodeType) && sr.Status == "running"
}

func (s *RunService) isPendingClarification(runID, nodeID string, iteration int) bool {
	var conv models.ReactConversation
	if err := s.db.Where("run_id = ? AND node_id = ? AND iteration = ? AND done = ?",
		runID, nodeID, iteration, false).First(&conv).Error; err != nil {
		return false
	}
	var run models.Run
	if err := s.db.First(&run, "id = ?", runID).Error; err != nil {
		return false
	}
	if containsString(terminalRunStatuses, run.Status) {
		return false
	}
	node := run.Graph.FindNode(nodeID)
	vars := s.varsByRun([]string{runID})[runID]
	if reactAutoEnabled(node, vars) {
		return false
	}
	var sr models.StateRun
	if err := s.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iteration).
		Order("id desc").First(&sr).Error; err != nil || sr.Status != "waiting_human" {
		return false
	}
	return true
}

func containsString(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// GateUpstreamNodeIDs collects upstream node ids referenced by a gate's
// body_template ({{nodes.<id>.outputs.*}}) and, for proposal_select gates,
// the node that produced the configured proposals artifact.
func GateUpstreamNodeIDs(gateNode *models.Node, artifacts []models.Artifact) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	if gateNode != nil {
		if bt, ok := gateNode.Config["body_template"].(string); ok {
			for _, m := range gateBodyNodeRef.FindAllStringSubmatch(bt, -1) {
				if len(m) > 1 {
					add(m[1])
				}
			}
		}
		if gateNode.Type == "proposal_select" {
			from := "proposals.json"
			if v, ok := gateNode.Config["from"].(string); ok && strings.TrimSpace(v) != "" {
				from = strings.TrimSpace(v)
			}
			for _, a := range artifacts {
				if a.Name == from {
					add(a.NodeID)
					break
				}
			}
		}
	}
	return out
}

// ClarifySlimNodeIDs returns the slim nodeExecutions set for a clarify/review
// inbox item: always the current node, plus any upstream ids parseable from the
// node's config (nodes.*.outputs / artifact()). Research post-run review with
// no template refs therefore degrades to the current node only.
func ClarifySlimNodeIDs(node *models.Node, currentNodeID string, artifacts []models.Artifact) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(currentNodeID)
	for _, id := range GateUpstreamNodeIDs(node, artifacts) {
		add(id)
	}
	if node == nil {
		return out
	}
	artByName := map[string]string{}
	for _, a := range artifacts {
		if a.Name != "" && a.NodeID != "" {
			if _, ok := artByName[a.Name]; !ok {
				artByName[a.Name] = a.NodeID
			}
		}
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			for _, m := range clarifyNodeOutputRef.FindAllStringSubmatch(x, -1) {
				if len(m) > 1 {
					add(m[1])
				}
			}
			for _, m := range clarifyArtifactRef.FindAllStringSubmatch(x, -1) {
				if len(m) > 1 {
					if nid := artByName[strings.TrimSpace(m[1])]; nid != "" {
						add(nid)
					}
				}
			}
		case map[string]any:
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(node.Config)
	return out
}

// SlimNodeExecutions returns upstream node execution history with only
// iteration + status + outputs (no events/mcpCalls/varsSnapshot).
// Large *_json snapshot fields are omitted — clients load full structured
// products via /api/artifacts/:id/content when needed.
func (s *RunService) SlimNodeExecutions(runID string, nodeIDs []string) map[string][]map[string]any {
	out := make(map[string][]map[string]any, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		var states []models.StateRun
		s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
			Order("iteration asc, id asc").Find(&states)
		execs := make([]map[string]any, 0, len(states))
		for _, st := range states {
			execs = append(execs, map[string]any{
				"iteration": st.Iteration,
				"status":    st.Status,
				"outputs":   OmitLargeJSONSnapshots(st.Outputs),
			})
		}
		out[nodeID] = execs
	}
	return out
}

// OmitLargeJSONSnapshots drops keys ending in "_json" (inline structured
// product snapshots such as clarified_requirement_json). Smaller rendered
// markdown keys and page HTML snapshots are kept for this round.
func OmitLargeJSONSnapshots(outputs map[string]any) map[string]any {
	if outputs == nil {
		return nil
	}
	out := make(map[string]any, len(outputs))
	for k, v := range outputs {
		if strings.HasSuffix(k, "_json") {
			continue
		}
		out[k] = v
	}
	return out
}

// ClarifyContext loads the react conversation for inbox-context clarify branch.
func (s *RunService) ClarifyContext(runID, nodeID string, iteration int) (models.ReactConversation, models.Run, bool) {
	var conv models.ReactConversation
	if err := s.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iteration).
		First(&conv).Error; err != nil {
		return models.ReactConversation{}, models.Run{}, false
	}
	run, ok := s.Get(runID)
	if !ok {
		return models.ReactConversation{}, models.Run{}, false
	}
	return conv, run, true
}

// PendingGateAt returns the unresolved gate for an exact run/node/iteration
// that is still waiting_human (same inbox eligibility as PendingInboxItems).
func (s *RunService) PendingGateAt(runID, nodeID string, iteration int) (models.Gate, bool) {
	var g models.Gate
	if err := pendingGateScope(s.db).
		Where("gates.run_id = ? AND gates.node_id = ? AND gates.iteration = ?",
			runID, nodeID, iteration).
		First(&g).Error; err != nil {
		return models.Gate{}, false
	}
	return g, true
}

// ClarifyLabel returns the display label for a react node in inbox-context.
func ClarifyLabel(g models.Graph, nodeID string) string {
	return nodeLabel(g, nodeID)
}
