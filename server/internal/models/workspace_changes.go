package models

import "time"

const (
	WorkspaceChangesSolo = "solo"
	WorkspaceChangesRun  = "run"
)

type WorkspaceChangeBaseline struct {
	OwnerKind  string                         `gorm:"primaryKey;size:8"`
	OwnerID    string                         `gorm:"primaryKey;size:128"`
	Generation int                            `gorm:"primaryKey;autoIncrement:false"`
	ArchiveID  string                         `gorm:"size:36"`
	Trees      map[string]WorkspaceChangeTree `gorm:"serializer:json"`
}

type WorkspaceChangeTree struct {
	Ref    string
	Head   string
	Branch string
	Pack   string `json:"pack,omitempty"`
}

// WorkspaceChangeSnapshot identifies an immutable comparison. Payload bytes
// are stored separately so listing snapshots never loads file content.
type WorkspaceChangeSnapshot struct {
	ID          string                      `gorm:"primaryKey;size:36" json:"id"`
	OwnerKind   string                      `gorm:"index:idx_change_owner,priority:1;size:8" json:"-"`
	OwnerID     string                      `gorm:"index:idx_change_owner,priority:2;size:128" json:"-"`
	Generation  int                         `gorm:"index:idx_change_owner,priority:3" json:"-"`
	Workspace   string                      `gorm:"size:255" json:"workspace"`
	NodeID      string                      `gorm:"size:128" json:"nodeId,omitempty"`
	Iteration   int                         `json:"iteration,omitempty"`
	TurnID      string                      `gorm:"size:128" json:"turnId,omitempty"`
	Interrupted bool                        `json:"interrupted,omitempty"`
	CapturedAt  time.Time                   `json:"capturedAt"`
	Repos       []WorkspaceChangeRepository `gorm:"serializer:filejson" json:"repos"`
}

type WorkspaceChangeRepository struct {
	Name        string                `json:"name"`
	Directory   string                `json:"directory"`
	Branch      string                `json:"branch"`
	BaseBranch  string                `json:"baseBranch"`
	BaseSHA     string                `json:"baseSha"`
	HeadSHA     string                `json:"headSha"`
	From        string                `json:"from"`
	To          string                `json:"to"`
	Unavailable bool                  `json:"unavailable,omitempty"`
	Added       int                   `json:"added"`
	Deleted     int                   `json:"deleted"`
	FileCount   int                   `json:"fileCount"`
	Files       []WorkspaceChangePath `json:"files"`
}

type WorkspaceChangePath struct {
	ID           string `json:"id"`
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath,omitempty"`
	Status       string `json:"status"`
	Added        *int   `json:"added"`
	Deleted      *int   `json:"deleted"`
	HasPatch     bool   `json:"hasPatch"`
	HasBefore    bool   `json:"hasBefore"`
	HasAfter     bool   `json:"hasAfter"`
}

type WorkspaceChangeContent struct {
	SnapshotID string `gorm:"primaryKey;size:36"`
	FileID     string `gorm:"primaryKey;size:36"`
	Patch      []byte `gorm:"serializer:filebytes"`
	Before     []byte `gorm:"serializer:filebytes"`
	After      []byte `gorm:"serializer:filebytes"`
}

// WorkspaceChangeGarbage tracks an unpublished or retired archive directory.
type WorkspaceChangeGarbage struct {
	ID      string    `gorm:"primaryKey;size:36"`
	ReadyAt time.Time `gorm:"index"`
}
