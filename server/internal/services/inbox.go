package services

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"gorm.io/gorm"
)

var terminalRunStatuses = []string{"completed", "failed", "cancelled"}

// pendingGateScope restricts to unresolved gates whose node is still
// waiting_human on a non-terminal run. Dangling unresolved rows after the node
// left waiting must not re-enter the approvals inbox or context.
func pendingGateScope(db *gorm.DB) *gorm.DB {
	return db.Joins("JOIN runs ON runs.id = gates.run_id").
		Joins("JOIN state_runs ON state_runs.run_id = gates.run_id AND state_runs.node_id = gates.node_id AND state_runs.iteration = gates.iteration").
		Where("gates.resolved = ? AND state_runs.status = ? AND runs.status NOT IN ?",
			false, "waiting_human", terminalRunStatuses)
}

// reactAutoEnabled reports whether a react node should self-answer without
// waiting for a human (mirrors engine.autoReactEnabled).
func reactAutoEnabled(node *models.Node, vars map[string]any) bool {
	if node == nil {
		return false
	}
	if node.Type == "approve" {
		return false
	}
	autoVar := strings.TrimSpace(configStr(node.Config["auto_var"]))
	if autoVar == "" {
		return false
	}
	return varTruthy(vars[autoVar])
}

func configStr(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func varTruthy(v any) bool {
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	case int:
		return n != 0
	case string:
		return n != "" && n != "false"
	case nil:
		return false
	default:
		return true
	}
}

func nodeLabel(g models.Graph, nodeID string) string {
	if label := GraphNodeLabel(g, nodeID); label != "" {
		return label
	}
	return nodeID
}

func latestMessageAt(msgs []models.ReactMessage, fallback time.Time) time.Time {
	var latest time.Time
	for _, m := range msgs {
		if t, err := time.Parse(time.RFC3339, m.At); err == nil && t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return fallback
	}
	return latest
}

// GateShareInboxStatus is leak-free share-link state for Inbox chips (no token).
type GateShareInboxStatus struct {
	State            string     `json:"state"`
	TTLTier          string     `json:"ttlTier,omitempty"`
	PermissionPreset string     `json:"permissionPreset,omitempty"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	RemainingSec     *int64     `json:"remainingSec,omitempty"`
	UsedAt           *time.Time `json:"usedAt,omitempty"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	CanCreate        bool       `json:"canCreate"`
	CanManage        bool       `json:"canManage"`
	HasPass          bool       `json:"hasPass"`
	HasFail          bool       `json:"hasFail"`
}

// GateInboxItem is a pending gate in the unified inbox (GET /gates).
type GateInboxItem struct {
	Type              string                `json:"type"`
	RunID             string                `json:"runId"`
	NodeID            string                `json:"nodeId"`
	Iteration         int                   `json:"iteration"`
	WorkflowID        string                `json:"workflowId"`
	WorkflowName      string                `json:"workflowName"`
	RunTitle          string                `json:"runTitle,omitempty"`
	Title             string                `json:"title"`
	BodyMd            string                `json:"bodyMd"`
	Actions           []models.GateAction   `json:"actions"`
	Form              []models.GateField    `json:"form"`
	UpstreamNodeID    string                `json:"upstreamNodeId,omitempty"`
	UpstreamIteration int                   `json:"upstreamIteration,omitempty"`
	RequestedAt       time.Time             `json:"requestedAt"`
	Tags              []string              `json:"tags"`
	NodeType          string                `json:"nodeType,omitempty"`
	ShareLink         *GateShareInboxStatus `json:"shareLink,omitempty"`
}

func gateInboxItem(g models.Gate, meta runInboxMeta) GateInboxItem {
	nodeType := ""
	if n := meta.Graph.FindNode(g.NodeID); n != nil {
		nodeType = n.Type
	}
	return GateInboxItem{
		Type: "gate", NodeType: nodeType, RunID: g.RunID, NodeID: g.NodeID, Iteration: g.Iteration,
		WorkflowID: g.WorkflowID, WorkflowName: g.WorkflowName, RunTitle: meta.Title,
		Title: g.Title, BodyMd: g.BodyMd, Actions: g.Actions, Form: g.Form,
		UpstreamNodeID: g.UpstreamNodeID, UpstreamIteration: g.UpstreamIteration,
		RequestedAt: g.RequestedAt, Tags: append([]string{}, meta.Tags...),
	}
}

// ClarifyInboxItem is a pending react clarification or product review in the
// unified inbox. Type is the inbox channel (always "clarify" for this path);
// Kind is the list-badge semantic: "clarify" (react), "review" (ReviewCapable),
// or "app_preview" (application preview waiting for confirm & continue).
type ClarifyInboxItem struct {
	Type string `json:"type"`
	Kind string `json:"kind"` // clarify | review | app_preview | preflight
	// State is "starting" while the node's sandbox is still booting (no
	// conversation yet, so no transcript and no reply accepted). "replying"
	// means the item is parked at waiting_human and the review/clarify session
	// is busy (in-flight turn or waiting>0, same as sessionBusy). Empty means
	// parked and idle. starting takes priority over replying.
	State        string                `json:"state,omitempty"`
	RunID        string                `json:"runId"`
	NodeID       string                `json:"nodeId"`
	Iteration    int                   `json:"iteration"`
	WorkflowID   string                `json:"workflowId"`
	WorkflowName string                `json:"workflowName"`
	RunTitle     string                `json:"runTitle,omitempty"`
	Label        string                `json:"label"`
	Done         bool                  `json:"done"`
	RequestedAt  time.Time             `json:"requestedAt"`
	UpdatedAt    time.Time             `json:"updatedAt"`
	Tags         []string              `json:"tags"`
	ShareLink    *GateShareInboxStatus `json:"shareLink,omitempty"`
}

// IsShareableReviewSession reports whether the node can mint a ShareLinkKindReview
// temp link (Inbox kind=review, kind=app_preview, or kind=clarify / node.Type=react).
// Badge kind stays clarifyInboxKind: react → clarify, app_preview stays distinct,
// and proposal_select (default "clarify" kind but not Type=react) stays excluded.
func IsShareableReviewSession(node *models.Node) bool {
	if node != nil && nodereg.ClarifyInteractive(node.Type) {
		return true
	}
	k := clarifyInboxKind(node)
	return k == "review" || k == "app_preview"
}

// clarifyInboxKind returns badge semantic for a waiting_human conversation.
// react/approve → clarify; preflight → preflight; app_preview → app_preview;
// other ReviewCapable product nodes → review; default clarify.
func clarifyInboxKind(node *models.Node) string {
	if node == nil {
		return "clarify"
	}
	if node.Type == "app_preview" {
		return "app_preview"
	}
	if node.Type == "preflight" {
		return "preflight"
	}
	if node.Type != "react" && node.Type != "approve" && nodereg.ReviewCapable(node.Type) {
		return "review"
	}
	return "clarify"
}

type inboxEntry struct {
	sortAt time.Time
	item   any
}

// AllPendingInboxItems merges unresolved gates and pending (non-auto) react
// clarifications on alive runs, sorted newest-first by sortAt.
func (s *RunService) AllPendingInboxItems() []any {
	items, _ := s.PendingInboxItems("", "", nil, 0, 0)
	return items
}

// PendingInboxItems returns merged inbox items sorted newest-first. When limit > 0
// the result is sliced to [offset:offset+limit]; total is always the full count.
func (s *RunService) PendingInboxItems(wf, projectID string, tags []string, offset, limit int) ([]any, int) {
	entries := s.pendingInboxEntries(wf, projectID, tags)
	total := len(entries)
	if limit > 0 {
		end := offset + limit
		if offset > total {
			offset = total
		}
		if end > total {
			end = total
		}
		entries = entries[offset:end]
	}
	out := make([]any, len(entries))
	for i, e := range entries {
		out[i] = e.item
	}
	return out, total
}

func (s *RunService) pendingInboxEntries(wf, projectID string, tags []string) []inboxEntry {
	gates := s.pendingGatesFiltered(wf, projectID, tags)
	clarifies := s.pendingClarificationsFiltered(wf, projectID, tags)
	clarifies = append(clarifies, s.startingApprovesFiltered(wf, projectID, tags)...)

	runIDs := make([]string, 0, len(gates)+len(clarifies))
	seen := map[string]bool{}
	for _, g := range gates {
		if !seen[g.RunID] {
			seen[g.RunID] = true
			runIDs = append(runIDs, g.RunID)
		}
	}
	for _, c := range clarifies {
		if !seen[c.RunID] {
			seen[c.RunID] = true
			runIDs = append(runIDs, c.RunID)
		}
	}
	runMeta := s.runMetaByIDs(runIDs)

	entries := make([]inboxEntry, 0, len(gates)+len(clarifies))
	for _, g := range gates {
		meta := runMeta[g.RunID]
		entries = append(entries, inboxEntry{sortAt: g.RequestedAt, item: gateInboxItem(g, meta)})
	}
	for _, c := range clarifies {
		meta := runMeta[c.RunID]
		if meta.Title != "" {
			c.RunTitle = meta.Title
		}
		c.Tags = append([]string{}, meta.Tags...)
		entries = append(entries, inboxEntry{sortAt: c.UpdatedAt, item: c})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].sortAt.After(entries[j].sortAt)
	})
	return entries
}

type runInboxMeta struct {
	Title string
	Tags  []string
	Graph models.Graph
}

// runMetaByIDs returns Run.Title/Tags keyed by run id.
func (s *RunService) runMetaByIDs(runIDs []string) map[string]runInboxMeta {
	out := make(map[string]runInboxMeta, len(runIDs))
	if len(runIDs) == 0 {
		return out
	}
	var rows []models.Run
	s.db.Model(&models.Run{}).Where("id IN ?", runIDs).Find(&rows)
	for _, r := range rows {
		out[r.ID] = runInboxMeta{Title: strings.TrimSpace(r.Title), Tags: append([]string{}, r.Tags...), Graph: r.Graph}
	}
	return out
}

func (s *RunService) pendingGatesFiltered(wf, projectID string, tags []string) []models.Gate {
	q := pendingGateScope(s.db)
	if wf != "" {
		q = q.Where("runs.workflow_id = ?", wf)
	} else if projectID != "" {
		q = q.Where("runs.workflow_id IN (?)", s.db.Model(&models.WorkflowDef{}).Select("id").Where("project_id = ?", projectID))
	}
	q = applyRunTagsFilter(q, "runs.tags", tags)
	var gates []models.Gate
	q.Order("gates.requested_at desc").Find(&gates)
	return gates
}

func (s *RunService) pendingClarificationsFiltered(wf, projectID string, tags []string) []ClarifyInboxItem {
	return s.filterClarifyByWorkflow(s.pendingClarifications(tags), wf, projectID)
}

func (s *RunService) startingApprovesFiltered(wf, projectID string, tags []string) []ClarifyInboxItem {
	return s.filterClarifyByWorkflow(s.startingApproves(tags), wf, projectID)
}

func (s *RunService) filterClarifyByWorkflow(clarifies []ClarifyInboxItem, wf, projectID string) []ClarifyInboxItem {
	if wf == "" && projectID == "" {
		return clarifies
	}
	var projectWFs map[string]struct{}
	if wf == "" && projectID != "" {
		var ids []string
		s.db.Model(&models.WorkflowDef{}).Where("project_id = ?", projectID).Pluck("id", &ids)
		projectWFs = make(map[string]struct{}, len(ids))
		for _, id := range ids {
			projectWFs[id] = struct{}{}
		}
	}
	out := make([]ClarifyInboxItem, 0, len(clarifies))
	for _, c := range clarifies {
		if wf != "" {
			if c.WorkflowID == wf {
				out = append(out, c)
			}
			continue
		}
		if _, ok := projectWFs[c.WorkflowID]; ok {
			out = append(out, c)
		}
	}
	return out
}

func (s *RunService) pendingClarifications(tags []string) []ClarifyInboxItem {
	var convs []models.ReactConversation
	s.db.Joins("JOIN runs ON runs.id = react_conversations.run_id").
		Where("react_conversations.done = ? AND runs.status NOT IN ?", false, terminalRunStatuses).
		Scopes(func(db *gorm.DB) *gorm.DB { return applyRunTagsFilter(db, "runs.tags", tags) }).
		Order("react_conversations.id asc").
		Find(&convs)
	if len(convs) == 0 {
		return nil
	}

	runIDs := make([]string, 0, len(convs))
	seenRun := map[string]bool{}
	for _, c := range convs {
		if !seenRun[c.RunID] {
			seenRun[c.RunID] = true
			runIDs = append(runIDs, c.RunID)
		}
	}

	var runs []models.Run
	s.db.Where("id IN ?", runIDs).Find(&runs)
	runByID := make(map[string]models.Run, len(runs))
	for _, r := range runs {
		runByID[r.ID] = r
	}

	varsByRun := s.varsByRun(runIDs)

	seen := map[string]bool{}
	out := make([]ClarifyInboxItem, 0, len(convs))
	for _, conv := range convs {
		key := fmt.Sprintf("%s:%s:%d", conv.RunID, conv.NodeID, conv.Iteration)
		if seen[key] {
			continue
		}
		seen[key] = true

		run, ok := runByID[conv.RunID]
		if !ok {
			continue
		}
		node := run.Graph.FindNode(conv.NodeID)
		if reactAutoEnabled(node, varsByRun[conv.RunID]) {
			continue
		}
		// app_preview waits via pure ReAct (no Gate row). Surface it on the
		// clarify inbox channel with kind=app_preview so Gates Inbox / MCP /
		// badge counts stay aligned with Run review.
		// Only surface clarifications where the node is genuinely waiting for
		// human input — exclude failed sandbox-setup paths (even if a stale
		// conversation row exists from before the fix).
		var sr models.StateRun
		if err := s.db.Where("run_id = ? AND node_id = ? AND iteration = ?", conv.RunID, conv.NodeID, conv.Iteration).
			Order("id desc").First(&sr).Error; err != nil || sr.Status != "waiting_human" {
			continue
		}

		sortAt := latestMessageAt(conv.Messages, run.StartedAt)
		out = append(out, ClarifyInboxItem{
			Type: "clarify", Kind: clarifyInboxKind(node),
			RunID: conv.RunID, NodeID: conv.NodeID, Iteration: conv.Iteration,
			WorkflowID: run.WorkflowID, WorkflowName: run.WorkflowName,
			RunTitle: strings.TrimSpace(run.Title),
			Label:    nodeLabel(run.Graph, conv.NodeID), Done: conv.Done, Tags: append([]string{}, run.Tags...),
			RequestedAt: run.StartedAt, UpdatedAt: sortAt,
		})
	}
	return out
}

// startingApproves lists approve nodes whose sandbox is still booting: the
// StateRun row is already "running" (written the moment the FSM enters the node)
// but no conversation exists yet, so pendingClarifications cannot see them. They
// surface as loading cards so an approval appears in the inbox the instant the
// run starts, on any tab and across refreshes.
func (s *RunService) startingApproves(tags []string) []ClarifyInboxItem {
	var states []models.StateRun
	s.db.Model(&models.StateRun{}).
		Joins("JOIN runs ON runs.id = state_runs.run_id").
		Where("state_runs.node_type = ? AND state_runs.status = ? AND runs.status NOT IN ?",
			"approve", "running", terminalRunStatuses).
		Scopes(func(db *gorm.DB) *gorm.DB { return applyRunTagsFilter(db, "runs.tags", tags) }).
		Order("state_runs.id asc").
		Find(&states)
	if len(states) == 0 {
		return nil
	}

	runIDs := make([]string, 0, len(states))
	seenRun := map[string]bool{}
	for _, sr := range states {
		if !seenRun[sr.RunID] {
			seenRun[sr.RunID] = true
			runIDs = append(runIDs, sr.RunID)
		}
	}

	var runs []models.Run
	s.db.Where("id IN ?", runIDs).Find(&runs)
	runByID := make(map[string]models.Run, len(runs))
	for _, r := range runs {
		runByID[r.ID] = r
	}

	var convs []models.ReactConversation
	s.db.Select("run_id", "node_id", "iteration").Where("run_id IN ?", runIDs).Find(&convs)
	hasConv := make(map[string]bool, len(convs))
	for _, c := range convs {
		hasConv[fmt.Sprintf("%s:%s:%d", c.RunID, c.NodeID, c.Iteration)] = true
	}

	out := make([]ClarifyInboxItem, 0, len(states))
	for _, sr := range states {
		key := fmt.Sprintf("%s:%s:%d", sr.RunID, sr.NodeID, sr.Iteration)
		if hasConv[key] {
			continue
		}
		run, ok := runByID[sr.RunID]
		if !ok {
			continue
		}
		startedAt := run.StartedAt
		if sr.StartedAt != nil {
			startedAt = *sr.StartedAt
		}
		out = append(out, ClarifyInboxItem{
			Type: "clarify", Kind: "clarify", State: "starting",
			RunID: sr.RunID, NodeID: sr.NodeID, Iteration: sr.Iteration,
			WorkflowID: run.WorkflowID, WorkflowName: run.WorkflowName,
			RunTitle: strings.TrimSpace(run.Title),
			Label:    nodeLabel(run.Graph, sr.NodeID), Tags: append([]string{}, run.Tags...),
			RequestedAt: startedAt, UpdatedAt: startedAt,
		})
	}
	return out
}

// ReviewSessionBusy reports whether a parked review/clarify session is busy
// (sessionBusy: in-flight turn or queue waiting > 0).
type ReviewSessionBusy func(runID, nodeID string) bool

// AttachInboxReplyingState writes state=replying onto parked clarify items whose
// review session is busy. starting is left untouched (higher priority). Human
// gates are ignored. Items stay in the slice — busy never unlists them.
func AttachInboxReplyingState(items []any, busy ReviewSessionBusy) {
	if busy == nil || len(items) == 0 {
		return
	}
	for i, it := range items {
		c, ok := it.(ClarifyInboxItem)
		if !ok {
			continue
		}
		if c.State == "starting" {
			continue
		}
		if busy(c.RunID, c.NodeID) {
			c.State = "replying"
			items[i] = c
			continue
		}
		if c.State == "replying" {
			c.State = ""
			items[i] = c
		}
	}
}

func (s *RunService) varsByRun(runIDs []string) map[string]map[string]any {
	out := make(map[string]map[string]any, len(runIDs))
	if len(runIDs) == 0 {
		return out
	}
	var vars []models.RunVariable
	s.db.Where("run_id IN ?", runIDs).Find(&vars)
	for _, v := range vars {
		if out[v.RunID] == nil {
			out[v.RunID] = map[string]any{}
		}
		out[v.RunID][v.Name] = v.Value
	}
	return out
}
