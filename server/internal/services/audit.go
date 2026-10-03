package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/textutil"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// MaxAuditPayloadBytes caps serialized payload size while keeping structure.
const MaxAuditPayloadBytes = 16 * 1024

// ProjectAuditService writes and queries append-only project audit events.
// Writes are fail-open: persistence errors are logged and do not fail callers.
type ProjectAuditService struct{ db *gorm.DB }

// NewProjectAuditService builds the service.
func NewProjectAuditService(db *gorm.DB) *ProjectAuditService {
	return &ProjectAuditService{db: db}
}

// AuditActor is the attributed operator for an event.
type AuditActor struct {
	Username       string
	Unattributable bool
}

// ActorFromUsername returns a real user when username is non-empty; otherwise
// system + unattributable. Never fabricates a person name.
// Call sites that intend PM attribution leave CallerKind empty so Record maps
// attributable users to CallerKindPM.
func ActorFromUsername(username string) AuditActor {
	u := strings.TrimSpace(username)
	if u == "" {
		return AuditActor{Username: "system", Unattributable: true}
	}
	return AuditActor{Username: u, Unattributable: false}
}

// SystemActor is the canonical unattributable system operator.
func SystemActor() AuditActor {
	return AuditActor{Username: "system", Unattributable: true}
}

// AuditRecord is the input for a single append-only event.
type AuditRecord struct {
	ProjectID    string
	Actor        AuditActor
	CallerKind   string // pm | apikey | system | external; empty → inferred from Actor
	Action       string
	ResourceType string
	ResourceID   string
	RunID        string // first-class; empty → inferred from resource/payload
	NodeID       string // first-class; empty → inferred from payload
	Outcome      string
	Summary      string
	Payload      map[string]any
	OccurredAt   time.Time // zero → now
}

// Record appends a masked audit event. Fail-open on DB errors.
func (s *ProjectAuditService) Record(rec AuditRecord) {
	if s == nil || s.db == nil || strings.TrimSpace(rec.ProjectID) == "" {
		return
	}
	actor := rec.Actor
	if strings.TrimSpace(actor.Username) == "" {
		actor = SystemActor()
	}
	outcome := rec.Outcome
	if outcome != models.AuditOutcomeFail {
		outcome = models.AuditOutcomeOK
	}
	at := rec.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	payload := MaskAuditPayload(rec.Payload)
	runID, nodeID := elevateRunNode(rec.RunID, rec.NodeID, rec.ResourceType, rec.ResourceID, payload)
	ev := models.ProjectAuditEvent{
		ID:             "aud-" + uuid.NewString()[:12],
		ProjectID:      rec.ProjectID,
		OccurredAt:     at,
		Actor:          actor.Username,
		Unattributable: actor.Unattributable,
		CallerKind:     resolveCallerKind(rec.CallerKind, actor),
		Action:         rec.Action,
		ResourceType:   rec.ResourceType,
		ResourceID:     rec.ResourceID,
		RunID:          runID,
		NodeID:         nodeID,
		Outcome:        outcome,
		Summary:        rec.Summary,
		Payload:        payload,
		CreatedAt:      time.Now(),
	}
	if err := s.db.Create(&ev).Error; err != nil {
		log.Warn().Err(err).
			Str("project_id", rec.ProjectID).
			Str("action", rec.Action).
			Msg("project audit write failed (fail-open)")
	}
}

func resolveCallerKind(explicit string, actor AuditActor) string {
	switch strings.TrimSpace(explicit) {
	case models.CallerKindPM, models.CallerKindAPIKey, models.CallerKindSystem, models.CallerKindExternal:
		return strings.TrimSpace(explicit)
	}
	if actor.Unattributable || actor.Username == "" || actor.Username == "system" {
		return models.CallerKindSystem
	}
	return models.CallerKindPM
}

func elevateRunNode(runID, nodeID, resourceType, resourceID string, payload map[string]any) (string, string) {
	runID = strings.TrimSpace(runID)
	nodeID = strings.TrimSpace(nodeID)
	if runID == "" && strings.EqualFold(strings.TrimSpace(resourceType), "run") {
		runID = strings.TrimSpace(resourceID)
	}
	if runID == "" {
		runID = payloadString(payload, "runId", "run_id", "RunID")
	}
	if nodeID == "" {
		nodeID = payloadString(payload, "nodeId", "node_id", "NodeID")
	}
	return runID, nodeID
}

func payloadString(p map[string]any, keys ...string) string {
	if p == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := p[k]; ok {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// AuditListFilter holds query filters for project audit listing/export.
type AuditListFilter struct {
	ProjectID  string
	From       *time.Time
	To         *time.Time
	Actor      string // legacy exact actor username
	CallerKind string // pm | apikey | system | external
	Action     string // exact or prefix (e.g. "workflow" matches workflow.*)
	Resource   string // substring match on resource_type, resource_id, or summary
	RunID      string // first-class run association
	NodeID     string // first-class node association
	Search     string // substring on summary / resource / action
	Page       int
	PageSize   int
}

// ListPage returns a page of audit events (newest first) plus total count.
func (s *ProjectAuditService) ListPage(f AuditListFilter) ([]models.ProjectAuditEvent, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("audit unavailable")
	}
	q := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page := f.Page
	if page < 1 {
		page = 1
	}
	size := f.PageSize
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	var items []models.ProjectAuditEvent
	err := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Order("occurred_at desc, id desc").
		Offset((page - 1) * size).
		Limit(size).
		Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []models.ProjectAuditEvent{}
	}
	return items, total, nil
}

// AuditListStats holds aggregate counts for the current filter (not just the page).
type AuditListStats struct {
	Total int64 `json:"total"`
	MCP   int64 `json:"mcp"`
	Fail  int64 `json:"fail"`
}

// CountStats returns total / MCP / fail counts for the filter.
func (s *ProjectAuditService) CountStats(f AuditListFilter) (AuditListStats, error) {
	empty := AuditListStats{}
	if s == nil || s.db == nil {
		return empty, fmt.Errorf("audit unavailable")
	}
	base := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f)
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return empty, err
	}
	var mcp int64
	if err := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Where("action = ? OR action LIKE ?", models.AuditActionMCPCall, "mcp.%").
		Count(&mcp).Error; err != nil {
		return empty, err
	}
	var fail int64
	if err := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Where("outcome = ?", models.AuditOutcomeFail).
		Count(&fail).Error; err != nil {
		return empty, err
	}
	return AuditListStats{Total: total, MCP: mcp, Fail: fail}, nil
}

// ListAllMatching returns all events matching the filter (capped) for export.
func (s *ProjectAuditService) ListAllMatching(f AuditListFilter, limit int) ([]models.ProjectAuditEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("audit unavailable")
	}
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	var items []models.ProjectAuditEvent
	err := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Order("occurred_at desc, id desc").
		Limit(limit).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []models.ProjectAuditEvent{}
	}
	return items, nil
}

// AuditFacetResource is one distinct resource option for dropdowns.
type AuditFacetResource struct {
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	Resource     string `json:"resource"`
}

// AuditFacetRun is a readable Run option for "按 Run 查看".
type AuditFacetRun struct {
	RunID string `json:"runId"`
	Label string `json:"label"`
	Sub   string `json:"sub,omitempty"`
}

// AuditFacetNode is a node option scoped to a selected Run.
type AuditFacetNode struct {
	NodeID string `json:"nodeId"`
	Label  string `json:"label"`
}

// AuditFacets holds Run / node / resource options for the dual-mode audit UI.
// Action-namespace cascade is intentionally removed.
type AuditFacets struct {
	Runs      []AuditFacetRun      `json:"runs"`
	Nodes     []AuditFacetNode     `json:"nodes"`
	Resources []AuditFacetResource `json:"resources"`
	// Actors kept for backward compatibility; dual-mode UI uses callerKind instead.
	Actors []string `json:"actors"`
}

// ListFacets returns Run list (time window), nodes/resources for an optional Run,
// and distinct actors. Action cascade narrowing is no longer applied.
func (s *ProjectAuditService) ListFacets(f AuditListFilter) (AuditFacets, error) {
	empty := AuditFacets{
		Runs: []AuditFacetRun{}, Nodes: []AuditFacetNode{},
		Resources: []AuditFacetResource{}, Actors: []string{},
	}
	if s == nil || s.db == nil {
		return empty, fmt.Errorf("audit unavailable")
	}
	base := AuditListFilter{
		ProjectID: f.ProjectID,
		From:      f.From,
		To:        f.To,
	}

	var actors []string
	actorQ := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), base).
		Where("actor <> ''").
		Select("actor").
		Group("actor").
		Order("actor asc")
	if err := actorQ.Pluck("actor", &actors).Error; err != nil {
		return empty, err
	}
	if actors == nil {
		actors = []string{}
	}

	runs, err := s.listFacetRuns(base)
	if err != nil {
		return empty, err
	}

	scope := base
	scope.RunID = strings.TrimSpace(f.RunID)
	nodes, err := s.listFacetNodes(scope)
	if err != nil {
		return empty, err
	}
	resources, err := s.listFacetResources(scope)
	if err != nil {
		return empty, err
	}

	return AuditFacets{
		Runs: runs, Nodes: nodes, Resources: resources, Actors: actors,
	}, nil
}

func (s *ProjectAuditService) listFacetRuns(base AuditListFilter) ([]AuditFacetRun, error) {
	var events []models.ProjectAuditEvent
	// Newest-first scan then de-dupe: avoids dialect issues with MAX()+Scan into time.Time.
	err := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), base).
		Where("run_id <> ''").
		Select("run_id, occurred_at").
		Order("occurred_at desc").
		Limit(3000).
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]AuditFacetRun, 0)
	for _, ev := range events {
		id := strings.TrimSpace(ev.RunID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, AuditFacetRun{
			RunID: id,
			Label: ev.OccurredAt.Local().Format("2006-01-02 15:04") + " · " + shortRunID(id),
			Sub:   s.runFacetSub(base, id),
		})
	}
	return out, nil
}

func shortRunID(id string) string {
	short := id
	if strings.HasPrefix(short, "run-") && len(short) > 4 {
		short = short[4:]
	}
	if len(short) > 8 {
		short = short[:8]
	}
	return short
}

func (s *ProjectAuditService) runFacetSub(base AuditListFilter, runID string) string {
	f := base
	f.RunID = runID
	var mcp, fail int64
	_ = s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Where("action = ? OR action LIKE ?", models.AuditActionMCPCall, "mcp.%").
		Count(&mcp).Error
	_ = s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Where("outcome = ?", models.AuditOutcomeFail).
		Count(&fail).Error
	parts := []string{}
	if fail > 0 {
		parts = append(parts, "失败")
	} else {
		parts = append(parts, "成功")
	}
	if mcp > 0 {
		parts = append(parts, "含 MCP")
	}
	return strings.Join(parts, " · ")
}

func (s *ProjectAuditService) listFacetNodes(f AuditListFilter) ([]AuditFacetNode, error) {
	if strings.TrimSpace(f.RunID) == "" {
		return []AuditFacetNode{}, nil
	}
	var ids []string
	err := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Where("node_id <> '' AND node_id <> ?", "run").
		Select("node_id").
		Group("node_id").
		Order("node_id asc").
		Pluck("node_id", &ids).Error
	if err != nil {
		return nil, err
	}
	out := make([]AuditFacetNode, 0, len(ids))
	for _, id := range ids {
		out = append(out, AuditFacetNode{NodeID: id, Label: id})
	}
	return out, nil
}

func (s *ProjectAuditService) listFacetResources(f AuditListFilter) ([]AuditFacetResource, error) {
	type resRow struct {
		ResourceType string
		ResourceID   string
	}
	var rows []resRow
	resQ := s.applyFilter(s.db.Model(&models.ProjectAuditEvent{}), f).
		Where("resource_type <> '' OR resource_id <> ''").
		Select("resource_type, resource_id").
		Group("resource_type, resource_id").
		Order("resource_type asc, resource_id asc")
	if err := resQ.Find(&rows).Error; err != nil {
		return nil, err
	}
	resources := make([]AuditFacetResource, 0, len(rows))
	for _, r := range rows {
		label := r.ResourceType
		if r.ResourceID != "" {
			if label != "" {
				label += "/" + r.ResourceID
			} else {
				label = r.ResourceID
			}
		}
		resources = append(resources, AuditFacetResource{
			ResourceType: r.ResourceType,
			ResourceID:   r.ResourceID,
			Resource:     label,
		})
	}
	return resources, nil
}

func (s *ProjectAuditService) applyFilter(q *gorm.DB, f AuditListFilter) *gorm.DB {
	q = q.Where("project_id = ?", f.ProjectID)
	if f.From != nil {
		q = q.Where("occurred_at >= ?", *f.From)
	}
	if f.To != nil {
		q = q.Where("occurred_at <= ?", *f.To)
	}
	if a := strings.TrimSpace(f.Actor); a != "" {
		q = q.Where("actor = ?", a)
	}
	if ck := strings.TrimSpace(f.CallerKind); ck != "" {
		q = q.Where("caller_kind = ?", ck)
	}
	if act := strings.TrimSpace(f.Action); act != "" {
		if strings.Contains(act, ".") {
			q = q.Where("action = ?", act)
		} else {
			q = q.Where("action = ? OR action LIKE ?", act, act+".%")
		}
	}
	if runID := strings.TrimSpace(f.RunID); runID != "" {
		q = q.Where("run_id = ?", runID)
	}
	if nodeID := strings.TrimSpace(f.NodeID); nodeID != "" {
		q = q.Where("node_id = ?", nodeID)
	}
	if res := strings.TrimSpace(f.Resource); res != "" {
		if i := strings.Index(res, "/"); i > 0 {
			typ, id := res[:i], res[i+1:]
			like := "%" + res + "%"
			q = q.Where(
				"(resource_type = ? AND resource_id = ?) OR resource_type LIKE ? OR resource_id LIKE ? OR summary LIKE ?",
				typ, id, like, like, like,
			)
		} else {
			like := "%" + res + "%"
			q = q.Where(
				"resource_type LIKE ? OR resource_id LIKE ? OR summary LIKE ?",
				like, like, like,
			)
		}
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		like := "%" + search + "%"
		q = q.Where(
			"summary LIKE ? OR resource_type LIKE ? OR resource_id LIKE ? OR action LIKE ?",
			like, like, like, like,
		)
	}
	return q
}

// BackfillAuditElevatedFields lifts runId/nodeId/callerKind from legacy rows.
// Safe to call repeatedly; only fills empty first-class columns. Never fabricates
// run association when payload/resource lack it.
func BackfillAuditElevatedFields(db *gorm.DB) {
	if db == nil {
		return
	}
	var events []models.ProjectAuditEvent
	// Cap per boot to avoid long startup on huge histories.
	if err := db.Where("run_id = '' OR run_id IS NULL OR node_id = '' OR node_id IS NULL OR caller_kind = '' OR caller_kind IS NULL").
		Order("occurred_at desc").
		Limit(5000).
		Find(&events).Error; err != nil {
		log.Warn().Err(err).Msg("audit elevated-field backfill query failed")
		return
	}
	updated := 0
	for _, ev := range events {
		runID, nodeID := elevateRunNode(ev.RunID, ev.NodeID, ev.ResourceType, ev.ResourceID, ev.Payload)
		caller := strings.TrimSpace(ev.CallerKind)
		if caller == "" {
			caller = resolveCallerKind("", AuditActor{Username: ev.Actor, Unattributable: ev.Unattributable})
		}
		changes := map[string]any{}
		if strings.TrimSpace(ev.RunID) == "" && runID != "" {
			changes["run_id"] = runID
		}
		if strings.TrimSpace(ev.NodeID) == "" && nodeID != "" {
			changes["node_id"] = nodeID
		}
		if strings.TrimSpace(ev.CallerKind) == "" && caller != "" {
			changes["caller_kind"] = caller
		}
		if len(changes) == 0 {
			continue
		}
		if err := db.Model(&models.ProjectAuditEvent{}).Where("id = ?", ev.ID).Updates(changes).Error; err != nil {
			log.Warn().Err(err).Str("id", ev.ID).Msg("audit elevated-field backfill update failed")
			continue
		}
		updated++
	}
	if updated > 0 {
		log.Info().Int("updated", updated).Msg("audit elevated-field backfill complete")
	}
}

// FormatAuditText renders events as human-readable plain text.
func FormatAuditText(events []models.ProjectAuditEvent) string {
	var b strings.Builder
	b.WriteString("=== Grasp Project Audit Export ===\n")
	b.WriteString(fmt.Sprintf("Exported: %s\n", time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("Count: %d\n\n", len(events)))
	for i, ev := range events {
		actor := ev.Actor
		if ev.Unattributable {
			actor = ev.Actor + " (unattributable)"
		}
		b.WriteString(fmt.Sprintf("--- #%d ---\n", i+1))
		b.WriteString(fmt.Sprintf("Time: %s\n", ev.OccurredAt.Format(time.RFC3339)))
		b.WriteString(fmt.Sprintf("Actor: %s\n", actor))
		b.WriteString(fmt.Sprintf("Action: %s\n", ev.Action))
		b.WriteString(fmt.Sprintf("Resource: %s/%s\n", ev.ResourceType, ev.ResourceID))
		b.WriteString(fmt.Sprintf("Outcome: %s\n", ev.Outcome))
		if ev.Summary != "" {
			b.WriteString(fmt.Sprintf("Summary: %s\n", ev.Summary))
		}
		if len(ev.Payload) > 0 {
			raw, _ := json.MarshalIndent(ev.Payload, "", "  ")
			b.WriteString("Payload:\n")
			b.Write(raw)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// sensitiveKeyHints are case-insensitive substrings that mark secret-like keys.
var sensitiveKeyHints = []string{
	"password", "passwd", "secret", "token", "apikey", "api_key",
	"access_key", "private_key", "credential", "authorization", "auth",
	"sandboxenv", "app_secret", "appsecret",
}

// valueScanParentKeys trigger extra string-value heuristics under high-risk parents
// (gate form / MCP arguments/result / free-text fields).
var valueScanParentKeys = map[string]bool{
	"form": true, "arguments": true, "args": true, "result": true,
	"content": true, "prompt": true, "text": true, "body": true,
	"message": true, "messages": true, "input": true, "inputs": true,
}

// MaskAuditPayload deeply redacts sensitive fields and truncates oversized trees.
func MaskAuditPayload(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := maskValue(in, 0, false).(map[string]any)
	raw, err := json.Marshal(out)
	if err != nil {
		return map[string]any{"_error": "payload marshal failed"}
	}
	if len(raw) <= MaxAuditPayloadBytes {
		return out
	}
	return map[string]any{
		"_truncated": true,
		"_note":      "payload exceeded size cap; structure summarized",
		"keys":       mapKeys(out),
		"preview":    string(raw[:MaxAuditPayloadBytes/4]) + "…",
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func maskValue(v any, depth int, scanValues bool) any {
	if depth > 12 {
		return "[max-depth]"
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSensitiveKey(k) {
				out[k] = SecretMask
				continue
			}
			childScan := scanValues || valueScanParentKeys[strings.ToLower(k)]
			out[k] = maskValue(val, depth+1, childScan)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = maskValue(val, depth+1, scanValues)
		}
		return out
	case string:
		s := t
		if scanValues {
			s = textutil.RedactSecrets(s)
		}
		if len(s) > 4000 {
			return s[:4000] + "…"
		}
		return s
	default:
		return v
	}
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	// Bare "value" is not treated as sensitive: project vars nest {name,value,secret}
	// and are masked via MaskProjectVarsForAudit before entering the payload tree.
	for _, h := range sensitiveKeyHints {
		if strings.Contains(k, h) {
			return true
		}
	}
	return false
}

// MaskProjectVarsForAudit returns variables with secret values replaced.
func MaskProjectVarsForAudit(vars []models.ProjectVariable) []map[string]any {
	out := make([]map[string]any, 0, len(vars))
	for _, v := range vars {
		val := v.Value
		if v.Secret {
			val = SecretMask
		}
		out = append(out, map[string]any{
			"name": v.Name, "type": v.Type, "value": val, "secret": v.Secret,
		})
	}
	return out
}

// MaskSandboxEnvForAudit returns env entries with secret values replaced.
func MaskSandboxEnvForAudit(env []models.EnvEntry) []map[string]any {
	out := make([]map[string]any, 0, len(env))
	for _, e := range env {
		val := e.Value
		if e.Secret {
			val = SecretMask
		}
		out = append(out, map[string]any{
			"key": e.Key, "value": val, "secret": e.Secret,
		})
	}
	return out
}

// ResolveProjectIDForWorkflow looks up project ownership for a workflow.
func ResolveProjectIDForWorkflow(db *gorm.DB, workflowID string) string {
	if db == nil || workflowID == "" {
		return ""
	}
	var wf models.WorkflowDef
	if err := db.Select("project_id").First(&wf, "id = ?", workflowID).Error; err != nil {
		return ""
	}
	return wf.ProjectID
}

// ResolveProjectIDForRun resolves project via run → workflow.
func ResolveProjectIDForRun(db *gorm.DB, runID string) string {
	if db == nil || runID == "" {
		return ""
	}
	var run models.Run
	if err := db.Select("workflow_id").First(&run, "id = ?", runID).Error; err != nil {
		return ""
	}
	return ResolveProjectIDForWorkflow(db, run.WorkflowID)
}
