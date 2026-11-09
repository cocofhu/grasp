package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
)

func (r *EnvironmentReleases) guard(ctx context.Context, row models.EnvironmentRelease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now().UTC()
	var count int64
	err := r.db.WithContext(ctx).Model(&models.EnvironmentRelease{}).Where("id = ? AND resource = ? AND owner = ? AND generation = ? AND state IN ? AND expires_at > ? AND deadline > ?", row.ID, row.Resource, row.Owner, row.Generation, []string{"dispatching", "observing"}, now, now).Count(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrReleaseOwner
	}
	var fence models.EnvironmentReleaseFence
	if err := r.db.WithContext(ctx).Take(&fence, "resource = ?", row.Resource).Error; err != nil {
		return err
	}
	if fence.Lineage != row.Lineage || fence.ActiveReleaseID != row.ID || fence.HighWater != row.Generation {
		return ErrReleaseOwner
	}
	return nil
}

// leaseContext stops at the last confirmed deadline even during a metadata
// outage. Successful remote renewals may extend it up to the original deadline.
func (r *EnvironmentReleases) leaseContext(ctx context.Context, row models.EnvironmentRelease) (context.Context, context.CancelFunc) {
	work, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if row.ExpiresAt == nil {
			cancel()
			return
		}
		known := *row.ExpiresAt
		interval := time.Second
		if r.options.LeaseTTL/4 < interval {
			interval = r.options.LeaseTTL / 4
		}
		if interval < time.Millisecond {
			interval = time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			until := time.Until(known)
			if row.Deadline.Before(known) {
				until = time.Until(row.Deadline)
			}
			if until <= 0 {
				cancel()
				return
			}
			timer := time.NewTimer(until)
			select {
			case <-work.Done():
				timer.Stop()
				return
			case <-timer.C:
				cancel()
				return
			case <-ticker.C:
				timer.Stop()
			}
			query, stop := context.WithDeadline(work, known)
			current, err := r.record(query, row.Resource, row.ID)
			stop()
			if err != nil {
				continue
			}
			if current.Owner != row.Owner || current.Generation != row.Generation || (current.State != "dispatching" && current.State != "observing") || current.ExpiresAt == nil {
				cancel()
				return
			}
			check, done := context.WithDeadline(work, known)
			ownership := r.guard(check, row)
			done()
			if errors.Is(ownership, ErrReleaseOwner) || errors.Is(ownership, gorm.ErrRecordNotFound) {
				cancel()
				return
			}
			if ownership != nil {
				continue
			}
			known = *current.ExpiresAt
		}
	}()
	return work, func() { cancel(); <-done }
}
func (r *EnvironmentReleases) uncertain(row models.EnvironmentRelease, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = r.db.WithContext(ctx).Model(&models.EnvironmentRelease{}).Where("id = ? AND owner = ? AND generation = ? AND state IN ?", row.ID, row.Owner, row.Generation, []string{"dispatching", "observing"}).Updates(map[string]any{"state": "unknown", "error": reason}).Error
}

// RunOne dispatches at most once. A crash after this SQL marker must be
// reconciled through Get; no worker can turn it back into a confirmed job.
func (r *EnvironmentReleases) RunOne(ctx context.Context, id string) error {
	if r == nil || r.native == nil {
		return driver.ErrEnvironmentReleaseUnsupported
	}
	var row models.EnvironmentRelease
	if err := r.db.WithContext(ctx).Take(&row, "id = ?", id).Error; err != nil {
		return err
	}
	if row.State != "confirmed" {
		return ErrReleaseConflict
	}
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&models.EnvironmentRelease{}).Where("id = ? AND state = 'confirmed' AND dispatched_at IS NULL AND expires_at > ? AND deadline > ?", id, now, now).Updates(map[string]any{"state": "dispatching", "dispatched_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrReleaseOwner
	}
	row.State, row.DispatchedAt = "dispatching", &now
	work, stop := r.leaseContext(ctx, row)
	defer stop()
	var plan driver.EnvironmentReleasePlan
	if json.Unmarshal([]byte(row.Plan), &plan) != nil || !plan.Valid() || plan.SHA256 != row.PlanSHA256 {
		r.uncertain(row, "release_plan_invalid")
		return ErrReleaseConflict
	}
	if err := r.native.DeleteEnvironmentRelease(work, plan, func(check context.Context) error { return r.guard(check, row) }); err != nil {
		r.uncertain(row, "physical_release_unconfirmed")
		return err
	}
	if err := r.guard(work, row); err != nil {
		r.uncertain(row, "release_owner_lost")
		return err
	}
	result = r.db.WithContext(work).Model(&models.EnvironmentRelease{}).Where("id = ? AND state = 'dispatching' AND owner = ? AND generation = ? AND expires_at > ? AND deadline > ?", row.ID, row.Owner, row.Generation, time.Now().UTC(), time.Now().UTC()).Update("state", "observing")
	if result.Error != nil || result.RowsAffected != 1 {
		r.uncertain(row, "release_publication_unconfirmed")
		if result.Error != nil {
			return result.Error
		}
		return ErrReleaseOwner
	}
	_, err := r.Get(work, row.Resource, row.ID)
	return err
}

// Get may persist a strictly read-only physical observation. It never sends a
// Delete, renews an expired owner, or invents authorization from gateway 404.
func (r *EnvironmentReleases) Get(ctx context.Context, resource, id string) (EnvironmentReleaseView, error) {
	row, err := r.record(ctx, resource, id)
	if err != nil {
		return EnvironmentReleaseView{}, err
	}
	view, err := releaseView(row)
	if err != nil || row.State == "released" || row.DispatchedAt == nil {
		return view, err
	}
	if r.native == nil {
		return view, driver.ErrEnvironmentReleaseUnsupported
	}
	gone, err := r.native.EnvironmentReleaseGone(ctx, view.Plan)
	if err != nil {
		return view, err
	}
	if !gone {
		return view, nil
	}
	receipt := EnvironmentReleaseReceipt{ID: row.ID, Resource: row.Resource, Lineage: row.Lineage, PlanSHA256: row.PlanSHA256, Objects: view.Plan.Objects, VerifiedAt: time.Now().UTC()}
	body, _ := json.Marshal(receipt)
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Match confirmation's Fence -> Sandbox -> receipt lock order. This
		// also rejects a receipt copied from another physical generation.
		var fence models.EnvironmentReleaseFence
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&fence, "resource = ?", resource).Error; err != nil {
			return err
		}
		if fence.ActiveReleaseID != row.ID || fence.Lineage != row.Lineage || fence.HighWater != row.Generation {
			return ErrReleaseConflict
		}
		var sb models.Sandbox
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&sb, "id = ?", resource).Error; err != nil {
			return err
		}
		if sb.Env()["GRASP_EXECUTION_LINEAGE"] != row.Lineage {
			return ErrReleaseConflict
		}
		result := tx.Model(&models.EnvironmentRelease{}).Where("id = ? AND resource = ? AND plan_sha256 = ? AND confirm_nonce = ? AND dispatched_at IS NOT NULL AND state IN ?", row.ID, row.Resource, row.PlanSHA256, row.ConfirmNonce, []string{"dispatching", "observing", "unknown"}).Updates(map[string]any{"state": "released", "result": string(body), "error": ""})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			var current models.EnvironmentRelease
			if err := tx.Take(&current, "id = ?", row.ID).Error; err != nil {
				return err
			}
			if current.State != "released" || current.PlanSHA256 != row.PlanSHA256 {
				return ErrReleaseConflict
			}
			return nil
		}
		// Keep the immutable ID/lineage tombstone and all release proof. Dropping
		// the control row would permit a later 404 to lose this attribution.
		return tx.Model(&models.Sandbox{}).Where("id = ?", resource).Updates(map[string]any{"status": "released", "endpoints": "{}", "error": ""}).Error
	})
	if err != nil {
		return view, err
	}
	row, err = r.record(ctx, resource, id)
	if err != nil {
		return view, err
	}
	return releaseView(row)
}
func (r *EnvironmentReleases) Run(ctx context.Context) {
	if r == nil || r.native == nil {
		return
	}
	ticker := time.NewTicker(r.options.PollInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now().UTC()
		_ = r.db.WithContext(ctx).Model(&models.EnvironmentRelease{}).Where("state IN ? AND (expires_at IS NULL OR expires_at <= ? OR deadline <= ?)", []string{"confirmed", "dispatching", "observing"}, now, now).Updates(map[string]any{"state": "unknown", "error": "release_lease_expired"}).Error
		var rows []models.EnvironmentRelease
		if err := r.db.WithContext(ctx).Where("state IN ?", []string{"confirmed", "observing"}).Order("created_at,id").Limit(32).Find(&rows).Error; err == nil {
			for _, row := range rows {
				if ctx.Err() != nil {
					return
				}
				now := time.Now().UTC()
				if row.ExpiresAt == nil || !row.ExpiresAt.After(now) || !row.Deadline.After(now) {
					_ = r.db.WithContext(ctx).Model(&models.EnvironmentRelease{}).Where("id = ? AND state IN ? AND (expires_at IS NULL OR expires_at <= ? OR deadline <= ?)", row.ID, []string{"confirmed", "observing"}, now, now).Updates(map[string]any{"state": "unknown", "error": "release_lease_expired"}).Error
					continue
				}
				if row.State == "confirmed" {
					_ = r.RunOne(ctx, row.ID)
				} else {
					_, _ = r.Get(ctx, row.Resource, row.ID)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
	}
}
