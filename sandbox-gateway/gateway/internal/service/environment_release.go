package service

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
)

var (
	ErrReleaseInvalid  = errors.New("invalid environment release request")
	ErrReleaseConflict = errors.New("environment release identity conflict")
	ErrReleaseOwner    = errors.New("environment release owner lost")
)
var releaseIdentity = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

type EnvironmentReleaseOptions struct{ LeaseTTL, MaxDuration, PollInterval time.Duration }
type EnvironmentReleaseCapabilities struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
	Version   string `json:"version,omitempty"`
}
type EnvironmentReleasePrepare struct {
	ID      string `json:"id"`
	Lineage string `json:"lineage"`
}
type EnvironmentReleaseConfirm struct {
	PlanSHA256   string    `json:"planSha256"`
	Nonce        string    `json:"nonce"`
	Confirmed    bool      `json:"confirmed"`
	LossAccepted bool      `json:"lossAccepted"`
	Owner        string    `json:"owner"`
	Generation   uint64    `json:"generation"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Deadline     time.Time `json:"deadline"`
}
type EnvironmentReleaseRenew struct {
	Owner      string    `json:"owner"`
	Generation uint64    `json:"generation"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type EnvironmentReleaseReceipt struct {
	ID         string                            `json:"id"`
	Resource   string                            `json:"resource"`
	Lineage    string                            `json:"lineage"`
	PlanSHA256 string                            `json:"planSha256"`
	Objects    []driver.EnvironmentReleaseObject `json:"objects"`
	VerifiedAt time.Time                         `json:"verifiedAt"`
}
type EnvironmentReleaseView struct {
	ID       string                        `json:"id"`
	State    string                        `json:"state"`
	Plan     driver.EnvironmentReleasePlan `json:"plan"`
	Receipt  *EnvironmentReleaseReceipt    `json:"receipt,omitempty"`
	Error    string                        `json:"error,omitempty"`
	Deadline time.Time                     `json:"deadline"`
}
type EnvironmentReleases struct {
	db      *gorm.DB
	sandbox *SandboxService
	native  driver.VerifiedEnvironmentReleaser
	options EnvironmentReleaseOptions
	wake    chan struct{}
}

func NewEnvironmentReleases(db *gorm.DB, sandbox *SandboxService, options EnvironmentReleaseOptions) *EnvironmentReleases {
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 45 * time.Second
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 5 * time.Minute
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	r := &EnvironmentReleases{db: db, sandbox: sandbox, options: options, wake: make(chan struct{}, 1)}
	if sandbox != nil {
		r.native, _ = sandbox.drv.(driver.VerifiedEnvironmentReleaser)
		sandbox.releases = r
	}
	return r
}
func (s *SandboxService) EnvironmentReleases() *EnvironmentReleases { return s.releases }
func (r *EnvironmentReleases) Capabilities(ctx context.Context, resource string) EnvironmentReleaseCapabilities {
	if r == nil || r.db == nil || r.native == nil {
		return EnvironmentReleaseCapabilities{Reason: "driver_not_supported"}
	}
	row, err := r.sandbox.store.Get(ctx, resource)
	if err != nil {
		return EnvironmentReleaseCapabilities{Reason: "resource_metadata_unavailable"}
	}
	if !releaseIdentity.MatchString(row.Env()["GRASP_EXECUTION_LINEAGE"]) {
		return EnvironmentReleaseCapabilities{Reason: "execution_lineage_unavailable"}
	}
	return EnvironmentReleaseCapabilities{Supported: true, Version: driver.EnvironmentReleaseVersion}
}
func (r *EnvironmentReleases) lifecycleGuard(ctx context.Context, resource string) error {
	if r == nil || r.native == nil {
		return nil
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.EnvironmentReleaseFence{}).Where("resource = ? AND active_release_id <> ''", resource).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return driver.ErrEnvironmentReleasePending
	}
	return nil
}
func (s *SandboxService) guardEnvironmentRelease(ctx context.Context, resource string) error {
	if s.releases == nil {
		return nil
	}
	return s.releases.lifecycleGuard(ctx, resource)
}
func (r *EnvironmentReleases) record(ctx context.Context, resource, id string) (models.EnvironmentRelease, error) {
	var row models.EnvironmentRelease
	if !releaseIdentity.MatchString(resource) || !releaseIdentity.MatchString(id) {
		return row, ErrReleaseInvalid
	}
	err := r.db.WithContext(ctx).Where("id = ? AND resource = ?", id, resource).Take(&row).Error
	return row, err
}
func releaseView(row models.EnvironmentRelease) (EnvironmentReleaseView, error) {
	v := EnvironmentReleaseView{ID: row.ID, State: row.State, Error: row.Error, Deadline: row.Deadline}
	if json.Unmarshal([]byte(row.Plan), &v.Plan) != nil || !v.Plan.Valid() || v.Plan.SHA256 != row.PlanSHA256 {
		return v, ErrReleaseConflict
	}
	if row.State == "released" {
		var receipt EnvironmentReleaseReceipt
		if json.Unmarshal([]byte(row.Result), &receipt) != nil || receipt.ID != row.ID || receipt.PlanSHA256 != row.PlanSHA256 || receipt.Resource != row.Resource || receipt.Lineage != row.Lineage {
			return v, ErrReleaseConflict
		}
		v.Receipt = &receipt
	}
	return v, nil
}
func (r *EnvironmentReleases) Preview(ctx context.Context, resource string, in EnvironmentReleasePrepare) (EnvironmentReleaseView, error) {
	if r == nil || r.db == nil || r.native == nil {
		return EnvironmentReleaseView{}, driver.ErrEnvironmentReleaseUnsupported
	}
	if !releaseIdentity.MatchString(resource) || !releaseIdentity.MatchString(in.ID) || !releaseIdentity.MatchString(in.Lineage) {
		return EnvironmentReleaseView{}, ErrReleaseInvalid
	}
	old, err := r.record(ctx, resource, in.ID)
	if err == nil {
		if old.Lineage != in.Lineage {
			return EnvironmentReleaseView{}, ErrReleaseConflict
		}
		return releaseView(old)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return EnvironmentReleaseView{}, err
	}
	if !r.Capabilities(ctx, resource).Supported {
		return EnvironmentReleaseView{}, driver.ErrEnvironmentReleaseUnsupported
	}
	sb, err := r.sandbox.store.Get(ctx, resource)
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	if sb.Env()["GRASP_EXECUTION_LINEAGE"] != in.Lineage {
		return EnvironmentReleaseView{}, ErrReleaseConflict
	}
	if err := r.lifecycleGuard(ctx, resource); err != nil {
		return EnvironmentReleaseView{}, err
	}
	plan, err := r.native.PreviewEnvironmentRelease(ctx, resource)
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	if !plan.Valid() || plan.Resource != resource {
		return EnvironmentReleaseView{}, ErrReleaseConflict
	}
	body, _ := json.Marshal(plan)
	row := models.EnvironmentRelease{ID: in.ID, Resource: resource, Lineage: in.Lineage, Plan: string(body), PlanSHA256: plan.SHA256, State: "prepared", Deadline: time.Now().UTC().Add(10 * time.Minute)}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fence := models.EnvironmentReleaseFence{Resource: resource, Lineage: in.Lineage}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&fence).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&fence, "resource = ?", resource).Error; err != nil {
			return err
		}
		if fence.Lineage != in.Lineage || fence.ActiveReleaseID != "" {
			return ErrReleaseConflict
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		return result.Error
	})
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	saved, err := r.record(ctx, resource, in.ID)
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	if saved.Lineage != in.Lineage || saved.PlanSHA256 != plan.SHA256 {
		return EnvironmentReleaseView{}, ErrReleaseConflict
	}
	return releaseView(saved)
}
func (r *EnvironmentReleases) Confirm(ctx context.Context, resource, id string, in EnvironmentReleaseConfirm) (EnvironmentReleaseView, error) {
	if r == nil || r.db == nil || r.native == nil {
		return EnvironmentReleaseView{}, driver.ErrEnvironmentReleaseUnsupported
	}
	if !in.Confirmed || !in.LossAccepted || !releaseIdentity.MatchString(in.Nonce) || !releaseIdentity.MatchString(in.Owner) || in.Generation == 0 {
		return EnvironmentReleaseView{}, ErrReleaseInvalid
	}
	row, err := r.record(ctx, resource, id)
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	if row.PlanSHA256 != in.PlanSHA256 {
		return EnvironmentReleaseView{}, ErrReleaseConflict
	}
	if row.ConfirmNonce != "" {
		if row.ConfirmNonce != in.Nonce {
			return EnvironmentReleaseView{}, ErrReleaseConflict
		}
		return releaseView(row)
	}
	now := time.Now().UTC()
	if !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(r.options.LeaseTTL+time.Second)) || !in.Deadline.After(now) || in.Deadline.After(now.Add(r.options.MaxDuration+time.Second)) {
		return EnvironmentReleaseView{}, ErrReleaseInvalid
	}
	var plan driver.EnvironmentReleasePlan
	if json.Unmarshal([]byte(row.Plan), &plan) != nil || !plan.Valid() {
		return EnvironmentReleaseView{}, ErrReleaseConflict
	}
	if err := r.native.CheckEnvironmentRelease(ctx, plan); err != nil {
		return EnvironmentReleaseView{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fence models.EnvironmentReleaseFence
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&fence, "resource = ?", resource).Error; err != nil {
			return err
		}
		var sb models.Sandbox
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&sb, "id = ?", resource).Error; err != nil {
			return err
		}
		if sb.Env()["GRASP_EXECUTION_LINEAGE"] != row.Lineage {
			return ErrReleaseConflict
		}
		if fence.ActiveReleaseID == row.ID {
			var accepted models.EnvironmentRelease
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&accepted, "id = ?", row.ID).Error; err != nil {
				return err
			}
			if accepted.ConfirmNonce == in.Nonce && accepted.PlanSHA256 == in.PlanSHA256 && accepted.Lineage == row.Lineage {
				return nil
			}
			return ErrReleaseConflict
		}
		if sb.Status != models.StatusStopped {
			return ErrReleaseConflict
		}
		if fence.Lineage != row.Lineage || fence.HighWater >= in.Generation || fence.ActiveReleaseID != "" {
			return ErrReleaseConflict
		}
		expires := in.ExpiresAt.UTC()
		if in.Deadline.Before(expires) {
			expires = in.Deadline.UTC()
		}
		result := tx.Model(&models.EnvironmentRelease{}).Where("id = ? AND state = 'prepared' AND confirm_nonce = '' AND deadline > ?", row.ID, now).Updates(map[string]any{"state": "confirmed", "confirm_nonce": in.Nonce, "owner": in.Owner, "generation": in.Generation, "expires_at": expires, "deadline": in.Deadline.UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrReleaseConflict
		}
		return tx.Model(&fence).Updates(map[string]any{"high_water": in.Generation, "active_release_id": row.ID}).Error
	})
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return r.Get(ctx, resource, id)
}
func (r *EnvironmentReleases) Renew(ctx context.Context, resource, id string, in EnvironmentReleaseRenew) (EnvironmentReleaseView, error) {
	now := time.Now().UTC()
	if !releaseIdentity.MatchString(in.Owner) || in.Generation == 0 || !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(r.options.LeaseTTL+time.Second)) {
		return EnvironmentReleaseView{}, ErrReleaseInvalid
	}
	row, err := r.record(ctx, resource, id)
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row, "id = ? AND resource = ?", id, resource).Error; err != nil {
			return err
		}
		if row.Owner != in.Owner || row.Generation != in.Generation || (row.State != "confirmed" && row.State != "dispatching" && row.State != "observing") || row.ExpiresAt == nil || !row.ExpiresAt.After(now) || !row.Deadline.After(now) {
			return ErrReleaseOwner
		}
		until := in.ExpiresAt.UTC()
		if row.Deadline.Before(until) {
			until = row.Deadline
		}
		if !until.After(*row.ExpiresAt) {
			return nil
		}
		result := tx.Model(&models.EnvironmentRelease{}).Where("id = ? AND owner = ? AND generation = ? AND expires_at > ? AND deadline > ?", id, in.Owner, in.Generation, now, now).Update("expires_at", until)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrReleaseOwner
		}
		row.ExpiresAt = &until
		return nil
	})
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	return releaseView(row)
}

// All ordinary lifecycle publications share the same Fence -> Sandbox locks
// as release. A late start/finalize reply cannot overwrite the retained proof
// or resurrect a released control row after the destructive job completes.
func (r *EnvironmentReleases) persistSandbox(ctx context.Context, sb *models.Sandbox) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var fence models.EnvironmentReleaseFence
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Find(&fence, "resource = ?", sb.ID)
		if result.Error != nil {
			return result.Error
		}
		if fence.ActiveReleaseID != "" {
			return driver.ErrEnvironmentReleasePending
		}
		var current models.Sandbox
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current, "id = ?", sb.ID).Error; err != nil {
			return err
		}
		if current.Status == "released" {
			return driver.ErrEnvironmentReleasePending
		}
		return tx.Save(sb).Error
	})
}
