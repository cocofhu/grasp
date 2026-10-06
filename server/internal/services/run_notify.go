package services

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// RunNotifyDeliverer pushes a formatted Run lifecycle message to explicit
// channel targets without requiring CronDeliver. Implementations must treat
// missing/invalid targets as a soft no-op (return nil or a sentinel) and must
// not abort the whole fan-out when one target fails.
type RunNotifyDeliverer interface {
	DeliverRunNotify(projectID, text string, channelIDs []string) error
}

// ErrRunNotifyNoTarget is returned (and swallowed) when no QQ target is bound.
var ErrRunNotifyNoTarget = errors.New("no run-notify delivery target")

// RunNotifyEvent is the Engine → service payload for a lifecycle side-effect.
type RunNotifyEvent struct {
	ProjectID    string
	RunID        string
	WorkflowID   string
	WorkflowName string
	ProjectName  string
	NodeID       string
	NodeLabel    string
	Iteration    int
	Kind         string // waiting_human | failed | completed
	DeepLinkBase string // PublicAdvertise; empty → relative /runs/{id}
}

// RunNotifyService resolves policy, claims the receipt key, then delivers.
type RunNotifyService struct {
	db       *gorm.DB
	deliver  RunNotifyDeliverer
	deepLink string
}

// NewRunNotifyService builds the service. deliver may be nil (always no-op send).
func NewRunNotifyService(db *gorm.DB, deliver RunNotifyDeliverer, publicAdvertise string) *RunNotifyService {
	return &RunNotifyService{
		db:       db,
		deliver:  deliver,
		deepLink: strings.TrimRight(strings.TrimSpace(publicAdvertise), "/"),
	}
}

// SetDeliverer hot-swaps the QQ egress (used after channel manager boots).
func (s *RunNotifyService) SetDeliverer(d RunNotifyDeliverer) {
	if s == nil {
		return
	}
	s.deliver = d
}

// AttemptDeliver runs the Demo attemptDeliver sequence:
// policy miss → no claim, no send; claim conflict → skip; no channel / send
// fail → still considered processed (receipt kept); never panics the caller.
func (s *RunNotifyService) AttemptDeliver(ev RunNotifyEvent) {
	if s == nil || s.db == nil {
		return
	}
	kind := strings.TrimSpace(ev.Kind)
	if kind != models.NotifyKindWaitingHuman && kind != models.NotifyKindFailed && kind != models.NotifyKindCompleted {
		return
	}
	if kind == models.NotifyKindCompleted {
		if strings.TrimSpace(ev.NodeID) == "" {
			ev.NodeID = models.NotifyCompletedSentinelNodeID
		}
		if ev.Iteration < 1 {
			ev.Iteration = 1
		}
		if strings.TrimSpace(ev.NodeLabel) == "" && ev.NodeID == models.NotifyCompletedSentinelNodeID {
			ev.NodeLabel = models.NotifyCompletedFallbackLabel
		}
	}
	if strings.TrimSpace(ev.RunID) == "" || strings.TrimSpace(ev.NodeID) == "" || ev.Iteration < 1 {
		// failed without node context (and malformed waiting_human) → skip, no claim
		log.Debug().Str("run_id", ev.RunID).Str("kind", kind).
			Msg("run-notify: skip — missing node context")
		return
	}

	project, workflow, ok := s.loadPolicy(ev)
	if !ok {
		return
	}
	events := ResolveNotifyEvents(project.NotifyPolicy, workflow.NotifyPolicy)
	if !NotifyEventAllowed(events, kind) {
		log.Debug().Str("run_id", ev.RunID).Str("kind", kind).
			Strs("resolved", events).Msg("run-notify: policy miss")
		return
	}

	// Empty ChannelIDs ⇒ do not deliver (and do not claim). Distinct from
	// Enabled=false which already returned via ResolveNotifyEvents.
	channelIDs := NormalizeNotifyChannelIDs(project.NotifyPolicy.ChannelIDs)
	if len(channelIDs) == 0 {
		log.Debug().Str("run_id", ev.RunID).Str("project", project.ID).
			Msg("run-notify: empty channelIds — no-op before claim")
		return
	}

	claimed, err := s.claimReceipt(ev.RunID, ev.NodeID, ev.Iteration, kind)
	if err != nil {
		log.Warn().Err(err).Str("run_id", ev.RunID).Str("kind", kind).
			Msg("run-notify: claim failed")
		return
	}
	if !claimed {
		log.Debug().Str("run_id", ev.RunID).Str("node_id", ev.NodeID).
			Int("iteration", ev.Iteration).Str("kind", kind).
			Msg("run-notify: duplicate key — skip")
		return
	}

	if ev.ProjectName == "" {
		ev.ProjectName = project.Name
	}
	if ev.WorkflowName == "" {
		ev.WorkflowName = workflow.Name
	}
	base := s.deepLinkBase(ev)
	if strings.TrimSpace(base) == "" {
		// QQ clients cannot open relative /runs/{id}; ops must set PublicAdvertise.
		log.Warn().
			Str("run_id", ev.RunID).
			Str("project", project.ID).
			Msg("run-notify: PublicAdvertise empty — deep link will be relative /runs/{id}")
	}
	text := RenderRunNotifyMessage(ev, base, templateForKind(project.NotifyPolicy, kind))
	if s.deliver == nil {
		log.Info().Str("run_id", ev.RunID).Str("kind", kind).
			Msg("run-notify: no deliverer — no-op after claim")
		return
	}
	if err := s.deliver.DeliverRunNotify(project.ID, text, channelIDs); err != nil {
		// P0: claim already held; persist visible reason and do not retry.
		s.markReceipt(ev.RunID, ev.NodeID, ev.Iteration, kind, "failed", err.Error())
		if errors.Is(err, ErrRunNotifyNoTarget) {
			log.Info().Str("run_id", ev.RunID).Str("project", project.ID).
				Msg("run-notify: no channel target — no-op after claim")
			return
		}
		log.Warn().Err(err).Str("run_id", ev.RunID).Str("project", project.ID).
			Msg("run-notify: send failed after claim (no retry)")
		return
	}
	s.markReceipt(ev.RunID, ev.NodeID, ev.Iteration, kind, "ok", "")
}

func (s *RunNotifyService) markReceipt(runID, nodeID string, iteration int, kind, status, errText string) {
	if s == nil || s.db == nil {
		return
	}
	_ = s.db.Model(&models.NotifyDeliveryReceipt{}).
		Where("run_id = ? AND node_id = ? AND iteration = ? AND kind = ?", runID, nodeID, iteration, kind).
		Updates(map[string]any{"status": status, "error": errText}).Error
}

// ListReceipts returns recent run-notify delivery records for a project
// (matched via runs.project_id). Newest first, capped at 50.
func (s *RunNotifyService) ListReceipts(projectID string) ([]models.NotifyDeliveryReceipt, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var rows []models.NotifyDeliveryReceipt
	err := s.db.Joins("JOIN runs ON runs.id = notify_delivery_receipts.run_id").
		Joins("JOIN workflow_defs ON workflow_defs.id = runs.workflow_id").
		Where("workflow_defs.project_id = ?", strings.TrimSpace(projectID)).
		Order("notify_delivery_receipts.created_at desc").Limit(50).Find(&rows).Error
	return rows, err
}

func (s *RunNotifyService) deepLinkBase(ev RunNotifyEvent) string {
	if b := strings.TrimRight(strings.TrimSpace(ev.DeepLinkBase), "/"); b != "" {
		return b
	}
	return s.deepLink
}

func (s *RunNotifyService) loadPolicy(ev RunNotifyEvent) (models.Project, models.WorkflowDef, bool) {
	var wf models.WorkflowDef
	wfID := strings.TrimSpace(ev.WorkflowID)
	if wfID == "" {
		var run models.Run
		if err := s.db.Select("workflow_id").First(&run, "id = ?", ev.RunID).Error; err != nil {
			log.Warn().Err(err).Str("run_id", ev.RunID).Msg("run-notify: load run failed")
			return models.Project{}, models.WorkflowDef{}, false
		}
		wfID = run.WorkflowID
	}
	if err := s.db.First(&wf, "id = ?", wfID).Error; err != nil {
		log.Warn().Err(err).Str("workflow", wfID).Msg("run-notify: load workflow failed")
		return models.Project{}, models.WorkflowDef{}, false
	}
	projectID := strings.TrimSpace(ev.ProjectID)
	if projectID == "" {
		projectID = wf.ProjectID
	}
	var project models.Project
	if err := s.db.First(&project, "id = ?", projectID).Error; err != nil {
		log.Warn().Err(err).Str("project", projectID).Msg("run-notify: load project failed")
		return models.Project{}, models.WorkflowDef{}, false
	}
	return project, wf, true
}

// claimReceipt inserts the unique receipt. Returns claimed=false on duplicate.
func (s *RunNotifyService) claimReceipt(runID, nodeID string, iteration int, kind string) (bool, error) {
	row := models.NotifyDeliveryReceipt{
		RunID:     runID,
		NodeID:    nodeID,
		Iteration: iteration,
		Kind:      kind,
		CreatedAt: time.Now(),
	}
	err := s.db.Create(&row).Error
	if err == nil {
		return true, nil
	}
	if isUniqueViolation(err) {
		return false, nil
	}
	return false, err
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

// Run notify template placeholders (locked; UI chips + frontend preview share this set).
const (
	NotifyPlaceholderProject  = "{project}"
	NotifyPlaceholderWorkflow = "{workflow}"
	NotifyPlaceholderRunID    = "{run_id}"
	NotifyPlaceholderNode     = "{node}"
	NotifyPlaceholderLink     = "{link}"
	NotifyPlaceholderTitle    = "{title}"
)

func templateForKind(p models.ProjectNotifyPolicy, kind string) string {
	switch kind {
	case models.NotifyKindWaitingHuman:
		return p.WaitingHumanTemplate
	case models.NotifyKindFailed:
		return p.FailedTemplate
	case models.NotifyKindCompleted:
		return p.CompletedTemplate
	default:
		return ""
	}
}

// RunNotifyTitle returns the event title used by {title} and the default formatter.
func RunNotifyTitle(kind string) string {
	switch kind {
	case models.NotifyKindWaitingHuman:
		return "等待人工处理"
	case models.NotifyKindCompleted:
		return "运行完成"
	default:
		return "运行失败"
	}
}

// RunNotifyNodeDisplay returns Label if set, otherwise NodeID (may be empty).
// The completed sentinel "_run" is not shown; callers fall back to「输出」.
func RunNotifyNodeDisplay(ev RunNotifyEvent) string {
	if n := strings.TrimSpace(ev.NodeLabel); n != "" {
		return n
	}
	id := strings.TrimSpace(ev.NodeID)
	if id == models.NotifyCompletedSentinelNodeID {
		return ""
	}
	return id
}

// ReplaceRunNotifyPlaceholders performs literal six-key replacement.
// Unknown placeholders are left as-is; never panics.
func ReplaceRunNotifyPlaceholders(tmpl string, project, workflow, runID, node, link, title string) string {
	replacer := strings.NewReplacer(
		NotifyPlaceholderProject, project,
		NotifyPlaceholderWorkflow, workflow,
		NotifyPlaceholderRunID, runID,
		NotifyPlaceholderNode, node,
		NotifyPlaceholderLink, link,
		NotifyPlaceholderTitle, title,
	)
	return replacer.Replace(tmpl)
}

// RenderRunNotifyMessage uses a custom template when trim-non-empty; otherwise
// uses the built-in FormatRunNotifyMessage.
func RenderRunNotifyMessage(ev RunNotifyEvent, base, template string) string {
	if strings.TrimSpace(template) == "" {
		return FormatRunNotifyMessage(ev, base)
	}
	projectName := strings.TrimSpace(ev.ProjectName)
	if projectName == "" {
		projectName = "—"
	}
	wfName := strings.TrimSpace(ev.WorkflowName)
	if wfName == "" {
		wfName = "—"
	}
	node := RunNotifyNodeDisplay(ev) // custom path: empty stays empty (no line delete)
	link := runDeepLink(base, ev.RunID, ev.Kind, ev.NodeID)
	title := RunNotifyTitle(ev.Kind)
	return ReplaceRunNotifyPlaceholders(template, projectName, wfName, ev.RunID, node, link, title)
}

// FormatRunNotifyMessage builds the QQ body (independent of FormatCronPush).
// Empty/missing project templates must call this path for zero regression
// (including omitting the whole「节点：」line when node display is empty).
func FormatRunNotifyMessage(ev RunNotifyEvent, base string) string {
	title := RunNotifyTitle(ev.Kind)
	projectName := strings.TrimSpace(ev.ProjectName)
	if projectName == "" {
		projectName = "—"
	}
	wfName := strings.TrimSpace(ev.WorkflowName)
	if wfName == "" {
		wfName = "—"
	}
	node := RunNotifyNodeDisplay(ev)
	if kind := strings.TrimSpace(ev.Kind); kind == models.NotifyKindCompleted && node == "" {
		node = models.NotifyCompletedFallbackLabel
	}
	link := runDeepLink(base, ev.RunID, ev.Kind, ev.NodeID)
	var b strings.Builder
	b.WriteString("【Grasp】")
	b.WriteString(title)
	b.WriteByte('\n')
	b.WriteString("项目：")
	b.WriteString(projectName)
	b.WriteByte('\n')
	b.WriteString("工作流：")
	b.WriteString(wfName)
	b.WriteByte('\n')
	b.WriteString("Run：")
	b.WriteString(ev.RunID)
	if node != "" {
		b.WriteByte('\n')
		b.WriteString("节点：")
		b.WriteString(node)
	}
	b.WriteByte('\n')
	b.WriteString("打开：")
	b.WriteString(link)
	return b.String()
}

func runDeepLink(base, runID, kind, nodeID string) string {
	path := "/runs/" + runID
	if strings.TrimSpace(kind) == models.NotifyKindCompleted {
		q := url.Values{}
		q.Set("tab", "output")
		if n := strings.TrimSpace(nodeID); n != "" && n != models.NotifyCompletedSentinelNodeID {
			q.Set("node", n)
		}
		path += "?" + q.Encode()
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return path
	}
	return base + path
}

// ClaimReceiptForTest exposes claimReceipt for unit tests.
func (s *RunNotifyService) ClaimReceiptForTest(runID, nodeID string, iteration int, kind string) (bool, error) {
	return s.claimReceipt(runID, nodeID, iteration, kind)
}

// HasClaimedForTest reports whether a receipt exists.
func (s *RunNotifyService) HasClaimedForTest(runID, nodeID string, iteration int, kind string) bool {
	var n int64
	s.db.Model(&models.NotifyDeliveryReceipt{}).
		Where("run_id = ? AND node_id = ? AND iteration = ? AND kind = ?", runID, nodeID, iteration, kind).
		Count(&n)
	return n > 0
}
