package services

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// DefaultRequirementDraftTitle is created on「新建」before the user edits.
	DefaultRequirementDraftTitle = "未命名需求"
	// DefaultRequirementDraftMilestoneTitle is the default title for new milestones.
	DefaultRequirementDraftMilestoneTitle = "未命名里程碑"
	// MaxRequirementDraftTitleRunes caps the trimmed title length.
	MaxRequirementDraftTitleRunes = 200
	// MaxRequirementDraftBodyRunes caps Markdown body size (reasonable TEXT upper bound).
	MaxRequirementDraftBodyRunes = 200_000
)

var (
	// ErrRequirementDraftNotFound is returned when a draft id is missing.
	ErrRequirementDraftNotFound = errors.New("requirement draft not found")
	// ErrRequirementDraftEmptyTitle is returned when save title trims to empty.
	ErrRequirementDraftEmptyTitle = errors.New("title must not be empty")
	// ErrRequirementDraftInvalidStatus is returned for status outside open|done.
	ErrRequirementDraftInvalidStatus = errors.New("invalid status")
	// ErrRequirementDraftTitleTooLong is returned when title exceeds the rune cap.
	ErrRequirementDraftTitleTooLong = errors.New("title too long")
	// ErrRequirementDraftBodyTooLong is returned when body exceeds the rune cap.
	ErrRequirementDraftBodyTooLong = errors.New("body too long")
	// ErrRequirementDraftInvalidKind is returned for kind outside requirement|milestone.
	ErrRequirementDraftInvalidKind = errors.New("invalid kind")
	// ErrRequirementDraftInvalidDate is returned for non YYYY-MM-DD date strings.
	ErrRequirementDraftInvalidDate = errors.New("invalid date")
	// ErrRequirementDraftDueBeforeStart is returned when dueAt < startAt.
	ErrRequirementDraftDueBeforeStart = errors.New("due date must not be before start date")
	// ErrRequirementDraftMilestoneDueRequired is returned when milestone lacks dueAt.
	ErrRequirementDraftMilestoneDueRequired = errors.New("milestone due date required")
	// ErrRequirementDraftInvalidProgress is returned when progress is outside 0–100.
	ErrRequirementDraftInvalidProgress = errors.New("progress must be 0-100")
	// ErrRequirementDraftInvalidParent is returned when parentId violates one-level rules.
	ErrRequirementDraftInvalidParent = errors.New("invalid parent")
	// ErrRequirementDraftHasChildren is returned when an operation is blocked by children.
	ErrRequirementDraftHasChildren = errors.New("draft has children")
	// ErrRequirementDraftKindNeedsDate is returned when converting to milestone without a date.
	ErrRequirementDraftKindNeedsDate = errors.New("milestone conversion requires a date")
)

var dateOnlyRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// RequirementDraftService persists requirement drafts.
type RequirementDraftService struct {
	db *gorm.DB
}

// NewRequirementDraftService builds the service.
func NewRequirementDraftService(db *gorm.DB) *RequirementDraftService {
	return &RequirementDraftService{db: db}
}

// ListFilter controls list status + title search.
type RequirementDraftListFilter struct {
	// Status is open|done|all (empty treated as all).
	Status string
	// Query is a case-insensitive title substring filter (empty = no filter).
	Query string
}

// RequirementDraftCreateInput optionally sets kind and schedule on create.
type RequirementDraftCreateInput struct {
	Kind     string
	Title    string
	StartAt  string
	DueAt    string
	Progress *int
	ParentID *string
}

// RequirementDraftScheduleInput updates schedule metadata (independent of title/body).
// Pointer nil = leave unchanged; empty string / "" for dates clears (when allowed).
type RequirementDraftScheduleInput struct {
	Kind     *string
	StartAt  *string
	DueAt    *string
	Progress *int
	ParentID **string // nil = unchanged; *nil = clear; *id = set
}

// List returns drafts ordered by updated_at DESC.
func (s *RequirementDraftService) List(f RequirementDraftListFilter) ([]models.RequirementDraft, error) {
	q := s.db
	switch strings.TrimSpace(f.Status) {
	case "", "all":
		// no status filter
	case models.RequirementDraftStatusOpen, models.RequirementDraftStatusDone:
		q = q.Where("status = ?", strings.TrimSpace(f.Status))
	default:
		return nil, ErrRequirementDraftInvalidStatus
	}
	if qq := strings.TrimSpace(f.Query); qq != "" {
		like := "%" + escapeLikePattern(qq) + "%"
		q = q.Where("title LIKE ? ESCAPE ?", like, `\`)
	}
	var rows []models.RequirementDraft
	if err := q.Order("updated_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []models.RequirementDraft{}
	}
	normalizeDraftRows(rows)
	return rows, nil
}

// Get returns one draft.
func (s *RequirementDraftService) Get(id string) (models.RequirementDraft, error) {
	var row models.RequirementDraft
	err := s.db.Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.RequirementDraft{}, ErrRequirementDraftNotFound
	}
	if err != nil {
		return models.RequirementDraft{}, err
	}
	normalizeDraft(&row)
	return row, nil
}

// Create inserts a new open draft. Empty input creates an unscheduled requirement.
func (s *RequirementDraftService) Create(in RequirementDraftCreateInput) (models.RequirementDraft, error) {
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = models.RequirementDraftKindRequirement
	}
	if kind != models.RequirementDraftKindRequirement && kind != models.RequirementDraftKindMilestone {
		return models.RequirementDraft{}, ErrRequirementDraftInvalidKind
	}

	startAt := strings.TrimSpace(in.StartAt)
	dueAt := strings.TrimSpace(in.DueAt)
	if kind == models.RequirementDraftKindMilestone {
		if dueAt == "" {
			return models.RequirementDraft{}, ErrRequirementDraftMilestoneDueRequired
		}
		startAt = "" // milestones only use dueAt
	}
	if err := validateDateOptional(startAt); err != nil {
		return models.RequirementDraft{}, err
	}
	if err := validateDateOptional(dueAt); err != nil {
		return models.RequirementDraft{}, err
	}
	if startAt != "" && dueAt != "" && dueAt < startAt {
		return models.RequirementDraft{}, ErrRequirementDraftDueBeforeStart
	}

	progress := 0
	if in.Progress != nil {
		if *in.Progress < 0 || *in.Progress > 100 {
			return models.RequirementDraft{}, ErrRequirementDraftInvalidProgress
		}
		progress = *in.Progress
	}

	var parentID *string
	if in.ParentID != nil && strings.TrimSpace(*in.ParentID) != "" {
		pid := strings.TrimSpace(*in.ParentID)
		parentID = &pid
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		if kind == models.RequirementDraftKindMilestone {
			title = DefaultRequirementDraftMilestoneTitle
		} else {
			title = DefaultRequirementDraftTitle
		}
	}
	if utf8.RuneCountInString(title) > MaxRequirementDraftTitleRunes {
		return models.RequirementDraft{}, ErrRequirementDraftTitleTooLong
	}

	now := time.Now()
	row := models.RequirementDraft{
		ID:           "rd-" + uuid.NewString()[:8],
		Title:        title,
		BodyMarkdown: "",
		Status:       models.RequirementDraftStatusOpen,
		Kind:         kind,
		StartAt:      startAt,
		DueAt:        dueAt,
		Progress:     progress,
		ParentID:     parentID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.validateParentAssignment(row.ID, parentID, false); err != nil {
		return models.RequirementDraft{}, err
	}
	if err := s.db.Create(&row).Error; err != nil {
		return models.RequirementDraft{}, err
	}
	normalizeDraft(&row)
	return row, nil
}

// UpdateContent updates title + body (explicit save). Title must trim non-empty.
func (s *RequirementDraftService) UpdateContent(id, title, body string) (models.RequirementDraft, error) {
	row, err := s.Get(id)
	if err != nil {
		return models.RequirementDraft{}, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return models.RequirementDraft{}, ErrRequirementDraftEmptyTitle
	}
	if utf8.RuneCountInString(title) > MaxRequirementDraftTitleRunes {
		return models.RequirementDraft{}, ErrRequirementDraftTitleTooLong
	}
	if utf8.RuneCountInString(body) > MaxRequirementDraftBodyRunes {
		return models.RequirementDraft{}, ErrRequirementDraftBodyTooLong
	}
	row.Title = title
	row.BodyMarkdown = body // empty body allowed
	row.UpdatedAt = time.Now()
	if err := s.db.Save(&row).Error; err != nil {
		return models.RequirementDraft{}, err
	}
	normalizeDraft(&row)
	return row, nil
}

// UpdateStatus toggles or sets status to open|done and refreshes updatedAt.
// Does not change progress.
func (s *RequirementDraftService) UpdateStatus(id, status string) (models.RequirementDraft, error) {
	row, err := s.Get(id)
	if err != nil {
		return models.RequirementDraft{}, err
	}
	status = strings.TrimSpace(status)
	if status != models.RequirementDraftStatusOpen && status != models.RequirementDraftStatusDone {
		return models.RequirementDraft{}, ErrRequirementDraftInvalidStatus
	}
	row.Status = status
	row.UpdatedAt = time.Now()
	if err := s.db.Save(&row).Error; err != nil {
		return models.RequirementDraft{}, err
	}
	normalizeDraft(&row)
	return row, nil
}

// UpdateSchedule writes kind / dates / progress / parent without touching title/body/status.
func (s *RequirementDraftService) UpdateSchedule(id string, in RequirementDraftScheduleInput) (models.RequirementDraft, error) {
	row, err := s.Get(id)
	if err != nil {
		return models.RequirementDraft{}, err
	}

	kind := row.Kind
	if in.Kind != nil {
		k := strings.TrimSpace(*in.Kind)
		if k != models.RequirementDraftKindRequirement && k != models.RequirementDraftKindMilestone {
			return models.RequirementDraft{}, ErrRequirementDraftInvalidKind
		}
		kind = k
	}

	startAt := row.StartAt
	dueAt := row.DueAt
	if in.StartAt != nil {
		startAt = strings.TrimSpace(*in.StartAt)
	}
	if in.DueAt != nil {
		dueAt = strings.TrimSpace(*in.DueAt)
	}

	progress := row.Progress
	if in.Progress != nil {
		if *in.Progress < 0 || *in.Progress > 100 {
			return models.RequirementDraft{}, ErrRequirementDraftInvalidProgress
		}
		progress = *in.Progress
	}

	parentID := row.ParentID
	if in.ParentID != nil {
		if *in.ParentID == nil || strings.TrimSpace(**in.ParentID) == "" {
			parentID = nil
		} else {
			pid := strings.TrimSpace(**in.ParentID)
			parentID = &pid
		}
	}

	// Kind conversion date mapping (clarified rules).
	if kind != row.Kind {
		hasChildren, err := s.hasChildren(row.ID)
		if err != nil {
			return models.RequirementDraft{}, err
		}
		if hasChildren && kind == models.RequirementDraftKindMilestone {
			return models.RequirementDraft{}, ErrRequirementDraftHasChildren
		}
		if row.Kind == models.RequirementDraftKindRequirement && kind == models.RequirementDraftKindMilestone {
			// Prefer dueAt; else startAt; else reject.
			mapped := dueAt
			if mapped == "" {
				mapped = startAt
			}
			if mapped == "" {
				return models.RequirementDraft{}, ErrRequirementDraftKindNeedsDate
			}
			dueAt = mapped
			startAt = ""
		} else if row.Kind == models.RequirementDraftKindMilestone && kind == models.RequirementDraftKindRequirement {
			// Milestone date becomes dueAt; startAt empty.
			if dueAt == "" {
				dueAt = row.DueAt
			}
			startAt = ""
		}
	}

	if kind == models.RequirementDraftKindMilestone {
		if dueAt == "" {
			return models.RequirementDraft{}, ErrRequirementDraftMilestoneDueRequired
		}
		startAt = ""
	}
	if err := validateDateOptional(startAt); err != nil {
		return models.RequirementDraft{}, err
	}
	if err := validateDateOptional(dueAt); err != nil {
		return models.RequirementDraft{}, err
	}
	if startAt != "" && dueAt != "" && dueAt < startAt {
		return models.RequirementDraft{}, ErrRequirementDraftDueBeforeStart
	}

	if err := s.validateParentAssignment(row.ID, parentID, true); err != nil {
		return models.RequirementDraft{}, err
	}

	row.Kind = kind
	row.StartAt = startAt
	row.DueAt = dueAt
	row.Progress = progress
	row.ParentID = parentID
	row.UpdatedAt = time.Now()
	if err := s.db.Save(&row).Error; err != nil {
		return models.RequirementDraft{}, err
	}
	normalizeDraft(&row)
	return row, nil
}

// Delete hard-removes a draft; children are promoted to top-level (parent_id cleared).
func (s *RequirementDraftService) Delete(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var row models.RequirementDraft
		err := tx.Where("id = ?", id).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRequirementDraftNotFound
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&models.RequirementDraft{}).
			Where("parent_id = ?", id).
			Update("parent_id", nil).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&models.RequirementDraft{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrRequirementDraftNotFound
		}
		return nil
	})
}

func (s *RequirementDraftService) hasChildren(id string) (bool, error) {
	var n int64
	err := s.db.Model(&models.RequirementDraft{}).
		Where("parent_id = ?", id).
		Count(&n).Error
	return n > 0, err
}

func (s *RequirementDraftService) validateParentAssignment(selfID string, parentID *string, checkSelfChildren bool) error {
	if parentID == nil || strings.TrimSpace(*parentID) == "" {
		return nil
	}
	pid := strings.TrimSpace(*parentID)
	if pid == selfID {
		return ErrRequirementDraftInvalidParent
	}
	if checkSelfChildren {
		hasChildren, err := s.hasChildren(selfID)
		if err != nil {
			return err
		}
		if hasChildren {
			// already a parent → cannot hang under another parent (no grandparent / no depth>1)
			return ErrRequirementDraftHasChildren
		}
	}
	var parent models.RequirementDraft
	err := s.db.Where("id = ?", pid).First(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrRequirementDraftInvalidParent
	}
	if err != nil {
		return err
	}
	normalizeDraft(&parent)
	if parent.Kind != models.RequirementDraftKindRequirement {
		return ErrRequirementDraftInvalidParent
	}
	if parent.ParentID != nil && strings.TrimSpace(*parent.ParentID) != "" {
		return ErrRequirementDraftInvalidParent
	}
	return nil
}

func validateDateOptional(v string) error {
	if v == "" {
		return nil
	}
	if !dateOnlyRe.MatchString(v) {
		return ErrRequirementDraftInvalidDate
	}
	if _, err := time.ParseInLocation("2006-01-02", v, time.UTC); err != nil {
		return ErrRequirementDraftInvalidDate
	}
	return nil
}

func normalizeDraftRows(rows []models.RequirementDraft) {
	for i := range rows {
		normalizeDraft(&rows[i])
	}
}

func normalizeDraft(row *models.RequirementDraft) {
	if row == nil {
		return
	}
	if strings.TrimSpace(row.Kind) == "" {
		row.Kind = models.RequirementDraftKindRequirement
	}
	if row.Progress < 0 {
		row.Progress = 0
	}
	if row.Progress > 100 {
		row.Progress = 100
	}
}
