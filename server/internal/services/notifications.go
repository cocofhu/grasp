package services

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const DefaultNotificationPageSize = 20

var (
	ErrNotificationUsernameRequired = errors.New("username required")
	ErrNotificationRunIDRequired    = errors.New("runId required")
)

// Progress / in-progress wording that must not appear as notification titles.
var noisyNotificationTitle = regexp.MustCompile(
	`(?i)运行中|排队中?|等待人工|等待中|(?:^|[\s·\-_/，,])(queued|running|waiting)(?:[\s·\-_/，,]|$)|waiting[_ ]human|in\s*progress`,
)

// NotificationService lists terminal-run notifications and records per-run reads.
type NotificationService struct {
	db   *gorm.DB
	runs *RunService
}

// NewNotificationService builds the service. runs may be nil (a RunService is created).
func NewNotificationService(db *gorm.DB, runs *RunService) *NotificationService {
	if runs == nil {
		runs = NewRunService(db)
	}
	return &NotificationService{db: db, runs: runs}
}

// NotificationItemDTO is one inbox row. Unread is computed server-side; no id array.
type NotificationItemDTO struct {
	RunID          string `json:"runId"`
	Status         string `json:"status"`
	Title          string `json:"title"`
	TitleNeutral   bool   `json:"titleNeutral"`
	WorkflowName   string `json:"workflowName"`
	StartedAt      string `json:"startedAt"`
	FinishedApprox string `json:"finishedApprox"`
	Unread         bool   `json:"unread"`
	BeforeBaseline bool   `json:"beforeBaseline"`
}

// NotificationListResult is a paginated inbox slice plus global counts.
type NotificationListResult struct {
	Items       []NotificationItemDTO `json:"items"`
	Page        int                   `json:"page"`
	PageSize    int                   `json:"pageSize"`
	Total       int64                 `json:"total"`
	AllCount    int64                 `json:"allCount"`
	UnreadCount int64                 `json:"unreadCount"`
	ReadCount   int64                 `json:"readCount"`
}

type notificationEnriched struct {
	item NotificationItemDTO
}

// ListPage returns one page of terminal-run notifications for filter=all|unread|read,
// plus true totals for all/unread/read across the full inbox (not just the page).
func (s *NotificationService) ListPage(username, filter string, page, pageSize int) (NotificationListResult, error) {
	username, err := requireUsername(username)
	if err != nil {
		return NotificationListResult{}, err
	}
	baseline, err := s.ensureBaseline(username)
	if err != nil {
		return NotificationListResult{}, err
	}

	filter = strings.TrimSpace(strings.ToLower(filter))
	if filter == "" {
		filter = "all"
	}
	switch filter {
	case "all", "unread", "read":
	default:
		filter = "all"
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultNotificationPageSize
	}

	enriched, err := s.buildEnrichedList(username, baseline.EnabledAt)
	if err != nil {
		return NotificationListResult{}, err
	}

	var allCount, unreadCount, readCount int64
	filtered := make([]notificationEnriched, 0, len(enriched))
	for _, e := range enriched {
		allCount++
		if e.item.Unread {
			unreadCount++
		} else {
			readCount++
		}
		switch filter {
		case "unread":
			if e.item.Unread {
				filtered = append(filtered, e)
			}
		case "read":
			if !e.item.Unread {
				filtered = append(filtered, e)
			}
		default:
			filtered = append(filtered, e)
		}
	}

	total := int64(len(filtered))
	start := (page - 1) * pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}

	items := make([]NotificationItemDTO, 0, end-start)
	for _, e := range filtered[start:end] {
		items = append(items, e.item)
	}

	return NotificationListResult{
		Items:       items,
		Page:        page,
		PageSize:    pageSize,
		Total:       total,
		AllCount:    allCount,
		UnreadCount: unreadCount,
		ReadCount:   readCount,
	}, nil
}

// List returns all terminal runs unpaged; handlers use ListPage.
func (s *NotificationService) List(username string) ([]NotificationItemDTO, error) {
	res, err := s.ListPage(username, "all", 1, 1<<31-1)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

// MarkRead inserts an ignore row for (username, runID). Idempotent.
func (s *NotificationService) MarkRead(username, runID string) error {
	username, err := requireUsername(username)
	if err != nil {
		return err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return ErrNotificationRunIDRequired
	}
	if _, err := s.ensureBaseline(username); err != nil {
		return err
	}
	return s.insertRead(username, runID)
}

// MarkAllRead inserts a read row for every unread terminal run for the user.
// The client does not send an id list. Existing rows are left in place.
func (s *NotificationService) MarkAllRead(username string) error {
	username, err := requireUsername(username)
	if err != nil {
		return err
	}
	baseline, err := s.ensureBaseline(username)
	if err != nil {
		return err
	}
	enriched, err := s.buildEnrichedList(username, baseline.EnabledAt)
	if err != nil {
		return err
	}
	for _, e := range enriched {
		if !e.item.Unread {
			continue
		}
		if err := s.insertRead(username, e.item.RunID); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) buildEnrichedList(username string, enabledAt time.Time) ([]notificationEnriched, error) {
	runs := s.runs.List([]string{"completed", "failed"}, "", "", "started_at", "desc")
	ids := make([]string, 0, len(runs))
	enriched := make([]notificationEnriched, 0, len(runs))
	for _, r := range runs {
		item, ok := MapRunToNotification(r)
		if !ok {
			continue
		}
		ids = append(ids, item.RunID)
		enriched = append(enriched, notificationEnriched{item: item})
	}
	sort.SliceStable(enriched, func(i, j int) bool {
		ti := parseMillis(enriched[i].item.FinishedApprox, enriched[i].item.StartedAt)
		tj := parseMillis(enriched[j].item.FinishedApprox, enriched[j].item.StartedAt)
		if ti != tj {
			return ti > tj
		}
		return enriched[i].item.RunID > enriched[j].item.RunID
	})
	read, err := s.readSet(username, ids)
	if err != nil {
		return nil, err
	}
	for i := range enriched {
		finished := parseTime(enriched[i].item.FinishedApprox, enriched[i].item.StartedAt)
		before := !finished.IsZero() && !enabledAt.IsZero() && !finished.After(enabledAt)
		enriched[i].item.BeforeBaseline = before
		_, marked := read[enriched[i].item.RunID]
		enriched[i].item.Unread = !before && !marked && !finished.IsZero() && finished.After(enabledAt)
	}
	return enriched, nil
}

func (s *NotificationService) insertRead(username, runID string) error {
	row := models.NotificationRead{
		Username: username,
		RunID:    runID,
		ReadAt:   time.Now().UTC(),
	}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "username"}, {Name: "run_id"}},
		DoNothing: true,
	}).Create(&row).Error
}

func (s *NotificationService) ensureBaseline(username string) (models.NotificationBaseline, error) {
	var row models.NotificationBaseline
	err := s.db.Where("username = ?", username).First(&row).Error
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return models.NotificationBaseline{}, err
	}
	now := time.Now().UTC()
	row = models.NotificationBaseline{
		Username:  username,
		EnabledAt: now,
		CreatedAt: now,
	}
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		return models.NotificationBaseline{}, err
	}
	if err := s.db.Where("username = ?", username).First(&row).Error; err != nil {
		return models.NotificationBaseline{}, err
	}
	return row, nil
}

func (s *NotificationService) readSet(username string, runIDs []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(runIDs))
	if len(runIDs) == 0 {
		return out, nil
	}
	var rows []models.NotificationRead
	if err := s.db.Where("username = ? AND run_id IN ?", username, runIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.RunID] = struct{}{}
	}
	return out, nil
}

func requireUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", ErrNotificationUsernameRequired
	}
	return username, nil
}

// IsNoisyNotificationTitle reports progress-noise wording that must not be shown.
func IsNoisyNotificationTitle(title string) bool {
	s := strings.TrimSpace(title)
	if s == "" {
		return false
	}
	return noisyNotificationTitle.MatchString(s)
}

// MapRunToNotification projects a terminal run into an inbox item (no unread flags).
func MapRunToNotification(r models.Run) (NotificationItemDTO, bool) {
	if r.Status != "completed" && r.Status != "failed" {
		return NotificationItemDTO{}, false
	}
	raw := strings.TrimSpace(r.Title)
	workflowName := strings.TrimSpace(r.WorkflowName)
	finished := finishedApprox(r)
	started := r.StartedAt
	if started.IsZero() {
		started = r.CreatedAt
	}

	title := raw
	if title == "" {
		title = workflowName
	}
	if title == "" {
		title = r.ID
	}
	titleNeutral := false
	if IsNoisyNotificationTitle(title) {
		name := workflowName
		if name == "" {
			name = r.ID
		}
		title = name + " · " + r.Status
		titleNeutral = true
	}

	return NotificationItemDTO{
		RunID:          r.ID,
		Status:         r.Status,
		Title:          title,
		TitleNeutral:   titleNeutral,
		WorkflowName:   workflowName,
		StartedAt:      formatTimeUTC(started),
		FinishedApprox: formatTimeUTC(finished),
	}, true
}

func finishedApprox(r models.Run) time.Time {
	start := r.StartedAt
	if start.IsZero() {
		start = r.CreatedAt
	}
	if r.DurationSec > 0 && !start.IsZero() {
		return start.Add(time.Duration(r.DurationSec) * time.Second)
	}
	return start
}

func formatTimeUTC(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(finishedApprox, startedAt string) time.Time {
	for _, s := range []string{finishedApprox, startedAt} {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseMillis(finishedApprox, startedAt string) int64 {
	t := parseTime(finishedApprox, startedAt)
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
