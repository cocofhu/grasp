package services

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

// WorkspaceSnapshotCatalog reads published metadata only. Viewing history does
// not refresh a source environment, revalidate its old execution grant, or read
// object bytes. Every request still needs the current account and scope policy.
type WorkspaceSnapshotCatalog struct {
	db        *gorm.DB
	authorize func(context.Context, *gorm.DB, string, string, string) error
}

func NewWorkspaceSnapshotCatalog(db *gorm.DB, authorize func(context.Context, *gorm.DB, string, string, string) error) (*WorkspaceSnapshotCatalog, error) {
	if db == nil || authorize == nil || messageOutsideTransaction(db) != nil {
		return nil, ErrWorkspaceJobAuthority
	}
	return &WorkspaceSnapshotCatalog{db: db, authorize: authorize}, nil
}

// WorkspaceSnapshotMetadata deliberately has no payload reference, native
// path, owner capability or credential. Ready records completed verification
// at VerifiedAt; it is not a claim that an object was probed during this read.
type WorkspaceSnapshotMetadata struct {
	ID              string     `json:"id"`
	ScopeKind       string     `json:"scopeKind"`
	ScopeID         string     `json:"scopeId"`
	RunID           string     `json:"runId,omitempty"`
	StateRunID      uint       `json:"stateRunId,omitempty"`
	NodeID          string     `json:"nodeId,omitempty"`
	Iteration       int        `json:"iteration,omitempty"`
	RunAttempt      int        `json:"runAttempt,omitempty"`
	ProviderAttempt int        `json:"providerAttempt,omitempty"`
	State           string     `json:"state"`
	Bytes           int64      `json:"bytes"`
	Entries         int64      `json:"entries"`
	VerifiedAt      *time.Time `json:"verifiedAt,omitempty"`
	RetainUntil     *time.Time `json:"retainUntil,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type WorkspaceSnapshotPage struct {
	Items      []WorkspaceSnapshotMetadata `json:"items"`
	NextCursor string                      `json:"nextCursor,omitempty"`
	HasMore    bool                        `json:"hasMore"`
}

type workspaceSnapshotCatalogRow struct {
	WorkspaceSnapshotMetadata `gorm:"embedded"`
	CaptureState              string
	PublicationState          string
}

func (r workspaceSnapshotCatalogRow) metadata() WorkspaceSnapshotMetadata {
	row := r.WorkspaceSnapshotMetadata
	if row.State == "ready" {
		return row
	}
	for _, state := range []string{r.CaptureState, r.PublicationState} {
		switch state {
		case "unknown", "failed", "cancelled", "expired":
			row.State = state
			return row
		}
	}
	return row
}

func (s *WorkspaceSnapshotCatalog) query(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Table("workspace_snapshots AS snapshot").
		Select("snapshot.id", "snapshot.scope_kind", "snapshot.scope_id", "snapshot.run_id", "snapshot.state_run_id", "snapshot.node_id", "snapshot.iteration", "snapshot.run_attempt", "snapshot.provider_attempt", "snapshot.state", "snapshot.bytes", "snapshot.entries", "snapshot.verified_at", "snapshot.retain_until", "snapshot.created_at", "capture.state AS capture_state", "publication.state AS publication_state").
		Joins("LEFT JOIN workspace_snapshot_jobs AS capture ON capture.id = snapshot.job_id").
		Joins("LEFT JOIN workspace_snapshot_jobs AS publication ON publication.id = snapshot.publication_job_id")
}

func snapshotCatalogCoordinate(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value && !strings.ContainsRune(value, 0)
}

func (s *WorkspaceSnapshotCatalog) scope(ctx context.Context, actor, kind, id string) error {
	if s == nil || s.db == nil || s.authorize == nil {
		return ErrWorkspaceJobAuthority
	}
	if !snapshotCatalogCoordinate(actor, 191) || (kind != "solo" && kind != "run") || !snapshotCatalogCoordinate(id, 191) {
		return gorm.ErrRecordNotFound
	}
	return s.authorize(ctx, s.db.WithContext(ctx), actor, kind, id)
}

// AuthorizeScope is also the read gate for durable operation receipts. It does
// not grant permission to execute or reconcile their native side effects.
func (s *WorkspaceSnapshotCatalog) AuthorizeScope(ctx context.Context, actor, kind, id string) error {
	return s.scope(ctx, actor, kind, id)
}

func (s *WorkspaceSnapshotCatalog) Get(ctx context.Context, actor, kind, scopeID, id string) (WorkspaceSnapshotMetadata, error) {
	if err := s.scope(ctx, actor, kind, scopeID); err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	if !snapshotCatalogCoordinate(id, 64) {
		return WorkspaceSnapshotMetadata{}, gorm.ErrRecordNotFound
	}
	var row workspaceSnapshotCatalogRow
	if err := s.query(ctx).Where("snapshot.id = ? AND snapshot.scope_kind = ? AND snapshot.scope_id = ?", id, kind, scopeID).Take(&row).Error; err != nil {
		return WorkspaceSnapshotMetadata{}, err
	}
	return row.metadata(), nil
}

func (s *WorkspaceSnapshotCatalog) List(ctx context.Context, actor, kind, scopeID, before string, limit int) (WorkspaceSnapshotPage, error) {
	if err := s.scope(ctx, actor, kind, scopeID); err != nil {
		return WorkspaceSnapshotPage{}, err
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 || (before != "" && !snapshotCatalogCoordinate(before, 64)) {
		return WorkspaceSnapshotPage{}, ErrWorkspaceJobInvalid
	}
	query := s.query(ctx).Where("snapshot.scope_kind = ? AND snapshot.scope_id = ?", kind, scopeID)
	if before != "" {
		var cursor struct {
			ID        string
			CreatedAt time.Time
		}
		if err := s.db.WithContext(ctx).Table("workspace_snapshots").Select("id", "created_at").Where("id = ? AND scope_kind = ? AND scope_id = ?", before, kind, scopeID).Take(&cursor).Error; err != nil {
			return WorkspaceSnapshotPage{}, err
		}
		query = query.Where("snapshot.created_at < ? OR (snapshot.created_at = ? AND snapshot.id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	var rows []workspaceSnapshotCatalogRow
	if err := query.Order("snapshot.created_at DESC, snapshot.id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return WorkspaceSnapshotPage{}, err
	}
	page := WorkspaceSnapshotPage{Items: make([]WorkspaceSnapshotMetadata, 0, min(limit, len(rows))), HasMore: len(rows) > limit}
	if page.HasMore {
		rows = rows[:limit]
	}
	for _, row := range rows {
		page.Items = append(page.Items, row.metadata())
	}
	if page.HasMore {
		page.NextCursor = page.Items[len(page.Items)-1].ID
	}
	return page, nil
}
