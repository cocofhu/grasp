package models

import "time"

// RequirementDraftStatus values for project-scoped requirement drafts.
const (
	RequirementDraftStatusOpen = "open"
	RequirementDraftStatusDone = "done"
)

// RequirementDraftKind values: requirement (task bar) or milestone (single-day diamond).
const (
	RequirementDraftKindRequirement = "requirement"
	RequirementDraftKindMilestone   = "milestone"
)

// RequirementDraft is a project-scoped requirement memo (「需求草稿」).
// Independent of Run / Memory / PreviewIssue / Board / plan todos.
// Status open|done; done means archived, not deleted.
// Kind requirement|milestone; schedule fields are date-only YYYY-MM-DD (empty = unscheduled).
type RequirementDraft struct {
	ID           string `gorm:"primaryKey" json:"id"`
	ProjectID    string `gorm:"index:idx_req_draft_proj_status_updated,priority:1;index" json:"projectId"`
	Title        string `json:"title"`
	BodyMarkdown string `gorm:"type:text" json:"bodyMarkdown"`
	// Status is open (未完成) or done (已完成/归档).
	Status string `gorm:"index:idx_req_draft_proj_status_updated,priority:2" json:"status"`
	// Kind is requirement|milestone (default requirement).
	Kind string `gorm:"index;default:requirement" json:"kind"`
	// StartAt is optional begin date YYYY-MM-DD (requirements only; milestones ignore).
	StartAt string `json:"startAt"`
	// DueAt is optional end date (requirement) or required milestone date YYYY-MM-DD.
	DueAt string `json:"dueAt"`
	// Progress is 0–100; independent of status.
	Progress int `json:"progress"`
	// ParentID optionally points at a top-level requirement in the same project (one level only).
	ParentID  *string   `gorm:"index" json:"parentId"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `gorm:"index:idx_req_draft_proj_status_updated,priority:3" json:"updatedAt"`
}
