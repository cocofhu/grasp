package executionlineage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const SoloEnvironmentReviewProtocol = "grasp-offline-solo-environment-review/v1"

// SoloEnvironmentReview is an offline, source-specific receipt. It preserves
// the frozen quarantine record and never reviews a Run, queue, or old turn.
// CompletedAt proves stop-only review; it is not an authorization for old RAM.
type SoloEnvironmentReview struct {
	ID                               string `gorm:"primaryKey;size:36"`
	Protocol                         string `gorm:"size:64"`
	DatasetID                        string `gorm:"size:36;index"`
	Lineage                          string `gorm:"size:36"`
	SandboxID                        uint   `gorm:"index"`
	SoloID                           string `gorm:"size:191;index"`
	QuarantineID                     string `gorm:"size:64;uniqueIndex"`
	ProviderID                       string `gorm:"size:255"`
	ProviderFingerprint              string `gorm:"size:64"`
	SourceFingerprint                string `gorm:"size:64"`
	BindingFingerprint               string `gorm:"size:64"`
	OriginalGeneration               uint64
	OriginalAuthorizationFingerprint string `gorm:"size:64"`
	State                            string `gorm:"size:32"`
	DispatchedAt                     *time.Time
	StoppedAt                        *time.Time
	CompletedAt                      *time.Time
	FinalizedAt                      *time.Time
	// The later, separately user-requested cold resume has its own durable
	// dispatch identity. A stop receipt alone never supplies an applied grant.
	ColdRequestID                string `gorm:"size:64"`
	ColdAuthorizationFingerprint string `gorm:"size:64"`
	ColdState                    string `gorm:"size:32"`
	ColdDispatchedAt             *time.Time
	ColdCompletedAt              *time.Time
	CreatedAt                    time.Time
}

type SoloReviewObservation struct {
	// Fingerprint binds the configured gateway, its original opaque ID and
	// the observed provider record. It must not contain credentials.
	Fingerprint string
	Status      string
}

// SoloReviewProvider exposes stop and observation only. In particular it has
// no Create, Start, Delete, or generic execution method.
type SoloReviewProvider interface {
	Observe(context.Context, string) (SoloReviewObservation, error)
	Stop(context.Context, string) error
}

// SoloReviewAuthorization validates the current effective Agent and grants
// outside SQL. It never stamps a missing historical grant onto old processes.
type SoloReviewAuthorization func(context.Context, uint) (string, error)

type soloReviewSource struct {
	SandboxID                                                                         uint
	Name, Profile, Purpose, SandboxStatus, ThreadID, HomeDir                          string
	Generation                                                                        uint64
	DesiredState, OperationOwner                                                      string
	ModelFingerprint, GitFingerprint, AuthorizationFingerprint                        string
	SoloID, OwnerID, AgentName, SoloStatus                                            string
	Epoch                                                                             uint64
	Attempt, Action, InitialConfiguration, CurrentConfiguration, AppliedConfiguration string
	CurrentModelSet, AppliedModelSet                                                  bool
}

func soloReviewSourceTx(tx *gorm.DB, id uint) (soloReviewSource, error) {
	return readSoloReviewSourceTx(tx, id, false)
}

func readSoloReviewSourceTx(tx *gorm.DB, id uint, leased bool) (soloReviewSource, error) {
	var source soloReviewSource
	var sandbox models.Sandbox
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name", "profile", "purpose", "status", "thread_id", "home_dir", "generation", "desired_state", "operation_owner", "model_binding_fingerprint", "git_bindings_fingerprint", "authorization_fingerprint").First(&sandbox, id).Error; err != nil {
		return source, err
	}
	var solo models.SoloSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "user_id", "agent_name", "sandbox_id", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action", "initial_configuration_id", "current_configuration_id", "applied_configuration_id", "current_model_set", "applied_model_set").First(&solo, "id = ? AND sandbox_id = ?", sandbox.ThreadID, id).Error; err != nil {
		return source, err
	}
	if sandbox.Purpose != "solo" || sandbox.Name == "" || sandbox.ThreadID == "" || solo.UserID == "" || solo.AgentName != sandbox.Profile || solo.LifecycleAction != "" || solo.Status == models.SoloStatusClosed || !leased && sandbox.OperationOwner != "" {
		return source, ErrConflict
	}
	source = soloReviewSource{SandboxID: id, Name: sandbox.Name, Profile: sandbox.Profile, Purpose: sandbox.Purpose,
		SandboxStatus: sandbox.Status, ThreadID: sandbox.ThreadID, HomeDir: sandbox.HomeDir, Generation: sandbox.Generation,
		DesiredState: sandbox.DesiredState, OperationOwner: sandbox.OperationOwner,
		ModelFingerprint: sandbox.ModelBindingFingerprint, GitFingerprint: sandbox.GitBindingsFingerprint, AuthorizationFingerprint: sandbox.AuthorizationFingerprint,
		SoloID: solo.ID, OwnerID: solo.UserID, AgentName: solo.AgentName, SoloStatus: solo.Status, Epoch: solo.LifecycleEpoch,
		Attempt: solo.LifecycleAttemptID, Action: solo.LifecycleAction, InitialConfiguration: solo.InitialConfigurationID,
		CurrentConfiguration: solo.CurrentConfigurationID, AppliedConfiguration: solo.AppliedConfigurationID,
		CurrentModelSet: solo.CurrentModelSet, AppliedModelSet: solo.AppliedModelSet}
	return source, nil
}

func soloReviewDigest(source soloReviewSource) string {
	body, _ := json.Marshal(source)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func soloReviewBindingDigest(source soloReviewSource) string {
	source.SandboxStatus, source.SoloStatus, source.DesiredState = "", "", ""
	source.OperationOwner, source.ModelFingerprint, source.GitFingerprint, source.AuthorizationFingerprint = "", "", "", ""
	source.InitialConfiguration, source.CurrentConfiguration, source.AppliedConfiguration = "", "", ""
	source.CurrentModelSet, source.AppliedModelSet = false, false
	source.Generation = 0
	return soloReviewDigest(source)
}

// VerifyReviewedSoloEnvironmentBindingTx is pure metadata validation for the
// explicit cold-resume path. Its caller must hold the current SQL binding and
// physical operation fence; a receipt never supplies either capability.
func VerifyReviewedSoloEnvironmentBindingTx(tx *gorm.DB, row SoloEnvironmentReview) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrTransaction
	}
	if row.Protocol != SoloEnvironmentReviewProtocol || row.State != "finalized" || row.FinalizedAt == nil || row.CompletedAt == nil || row.StoppedAt == nil {
		return ErrConflict
	}
	source, err := readSoloReviewSourceTx(tx, row.SandboxID, true)
	if err != nil {
		return err
	}
	if source.SoloID != row.SoloID || source.Name != row.ProviderID || source.Generation < row.OriginalGeneration+1 || soloReviewBindingDigest(source) != row.BindingFingerprint {
		return ErrConflict
	}
	return nil
}

func (a *SQLAuthority) soloReviewPermit(ctx context.Context, db *gorm.DB, owner string) (Permit, error) {
	if err := outsideSQLTransaction(db); err != nil {
		return Permit{}, err
	}
	if uuid.Validate(owner) != nil {
		return Permit{}, ErrConflict
	}
	if err := storageguard.Check(ctx, db, owner); err != nil {
		return Permit{}, err
	}
	permit, err := a.ReadActive(ctx)
	if err == nil {
		err = a.CheckBinding(ctx, db, permit)
	}
	return permit, err
}

func (a *SQLAuthority) guardSoloReviewTx(tx *gorm.DB, owner string, permit Permit) error {
	var control storageguard.Control
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("name = ? AND owner = ?", "primary", owner).Take(&control).Error; err != nil {
		return errors.Join(ErrInactive, err)
	}
	var active int64
	if err := tx.Model(&storageguard.Runtime{}).Count(&active).Error; err != nil {
		return err
	}
	if active != 0 {
		return ErrInactive
	}
	return a.GuardTx(tx, permit, "", "")
}

func (a *SQLAuthority) guardSoloReviewParentsTx(tx *gorm.DB, permit Permit, source soloReviewSource) error {
	queueHash := sha256.Sum256([]byte("solo\x00" + source.SoloID))
	queueID := hex.EncodeToString(queueHash[:])
	for _, entity := range []Entity{{Kind: EntitySolo, ID: source.SoloID}, {Kind: EntityQueue, ID: queueID}} {
		if err := a.GuardTx(tx, permit, entity.Kind, entity.ID); err != nil {
			return err
		}
	}
	checks := []struct {
		table, where string
		args         []any
	}{
		{"chat_queues", "id = ? AND (COALESCE(owner, '') <> '' OR COALESCE(active_turn_id, '') <> '')", []any{queueID}},
		{"chat_queue_turns", "queue_id = ? AND state IN ('queued','executing','unknown')", []any{queueID}},
		{"chat_pending_turns", "surface = 'solo' AND session_key = ?", []any{source.SoloID}},
		{"workspace_snapshot_jobs", "sandbox_id = ? AND state NOT IN ('completed','ready','failed','cancelled')", []any{source.SandboxID}},
		{"workspace_binding_heads", "sandbox_id = ? AND (state <> 'ready' OR COALESCE(pending_operation_id, '') <> '')", []any{source.SandboxID}},
		{"workspace_releases", "sandbox_id = ? AND state IN ('confirmed','dispatching','unknown','released')", []any{source.SandboxID}},
	}
	for _, check := range checks {
		var count int64
		if err := tx.Table(check.table).Where(check.where, check.args...).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrQuarantined
		}
	}
	return nil
}

// ReviewSoloEnvironment stops exactly one original Solo environment while all
// writers are offline. Once dispatch is durable, retries only observe the
// original result; they never repeat a possibly accepted native stop.
func (a *SQLAuthority) ReviewSoloEnvironment(ctx context.Context, db *gorm.DB, owner string, sandboxID uint, provider SoloReviewProvider, authorize SoloReviewAuthorization) (SoloEnvironmentReview, error) {
	var row SoloEnvironmentReview
	if provider == nil || authorize == nil || sandboxID == 0 {
		return row, ErrConflict
	}
	permit, err := a.soloReviewPermit(ctx, db, owner)
	if err != nil {
		return row, err
	}
	if err := db.WithContext(ctx).AutoMigrate(&SoloEnvironmentReview{}); err != nil {
		return row, err
	}
	var source soloReviewSource
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := a.guardSoloReviewTx(tx, owner, permit); err != nil {
			return err
		}
		var err error
		source, err = soloReviewSourceTx(tx, sandboxID)
		if err != nil {
			return err
		}
		if err := a.guardSoloReviewParentsTx(tx, permit, source); err != nil {
			return err
		}
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", owner).Error
		if err == nil {
			if row.Protocol != SoloEnvironmentReviewProtocol || row.DatasetID != permit.DatasetID() || row.Lineage != permit.Lineage() || row.SandboxID != sandboxID || row.SoloID != source.SoloID || row.ProviderID != source.Name {
				return ErrConflict
			}
			if row.BindingFingerprint != soloReviewBindingDigest(source) || row.CompletedAt == nil && row.SourceFingerprint != soloReviewDigest(source) {
				return ErrConflict
			}
			return a.GuardTx(tx, permit, "", "")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var quarantine models.ExecutionQuarantine
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("dataset_id = ? AND entity_kind = ? AND entity_id = ? AND review_state = 'pending'", permit.DatasetID(), EntitySandbox, strconv.FormatUint(uint64(sandboxID), 10)).Take(&quarantine).Error; err != nil {
			return errors.Join(ErrQuarantined, err)
		}
		row = SoloEnvironmentReview{ID: owner, Protocol: SoloEnvironmentReviewProtocol, DatasetID: permit.DatasetID(), Lineage: permit.Lineage(), SandboxID: sandboxID, SoloID: source.SoloID, QuarantineID: quarantine.ID, ProviderID: source.Name, SourceFingerprint: soloReviewDigest(source), BindingFingerprint: soloReviewBindingDigest(source), OriginalGeneration: source.Generation, OriginalAuthorizationFingerprint: source.AuthorizationFingerprint, State: "prepared", CreatedAt: time.Now().UTC()}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return a.GuardTx(tx, permit, "", "")
	})
	if err != nil {
		return row, err
	}
	grant, err := authorize(ctx, sandboxID)
	if err != nil || !validSHA(grant, false) {
		return row, errors.Join(ErrUnavailable, err)
	}
	observed, err := provider.Observe(ctx, row.ProviderID)
	if err != nil || !validSHA(observed.Fingerprint, false) {
		return row, errors.Join(ErrUnavailable, err)
	}
	if row.ProviderFingerprint != "" && row.ProviderFingerprint != observed.Fingerprint {
		return row, ErrConflict
	}
	if observed.Status != "running" && observed.Status != "stopped" {
		return row, ErrConflict
	}
	if row.CompletedAt != nil {
		if observed.Status != "stopped" {
			return row, ErrConflict
		}
		return row, nil
	}
	if row.DispatchedAt == nil {
		permit, err = a.soloReviewPermit(ctx, db, owner)
		if err == nil && (permit.DatasetID() != row.DatasetID || permit.Lineage() != row.Lineage) {
			err = ErrConflict
		}
		if err != nil {
			return row, err
		}
		now := time.Now().UTC()
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := a.guardSoloReviewTx(tx, owner, permit); err != nil {
				return err
			}
			current, err := soloReviewSourceTx(tx, sandboxID)
			if err != nil {
				return err
			}
			if soloReviewDigest(current) != row.SourceFingerprint {
				return ErrConflict
			}
			if err := a.guardSoloReviewParentsTx(tx, permit, current); err != nil {
				return err
			}
			res := tx.Model(&SoloEnvironmentReview{}).Where("id = ? AND state = 'prepared' AND dispatched_at IS NULL", row.ID).Updates(map[string]any{"state": "dispatching", "provider_fingerprint": observed.Fingerprint, "dispatched_at": now})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return ErrConflict
			}
			return a.GuardTx(tx, permit, "", "")
		})
		if err != nil {
			return row, err
		}
		row.State, row.ProviderFingerprint, row.DispatchedAt = "dispatching", observed.Fingerprint, &now
		if observed.Status != "stopped" {
			if observed.Status != "running" {
				return row, ErrConflict
			}
			if err := provider.Stop(ctx, row.ProviderID); err != nil {
				return row, errors.Join(ErrUnavailable, err)
			}
		}
	}
	observed, err = provider.Observe(ctx, row.ProviderID)
	if err != nil {
		return row, err
	}
	if observed.Fingerprint != row.ProviderFingerprint || observed.Status != "stopped" {
		return row, ErrConflict
	}
	currentGrant, err := authorize(ctx, sandboxID)
	if err != nil || currentGrant != grant {
		return row, errors.Join(ErrConflict, err)
	}
	permit, err = a.soloReviewPermit(ctx, db, owner)
	if err == nil && (permit.DatasetID() != row.DatasetID || permit.Lineage() != row.Lineage) {
		err = ErrConflict
	}
	if err != nil {
		return row, err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := a.guardSoloReviewTx(tx, owner, permit); err != nil {
			return err
		}
		current, err := soloReviewSourceTx(tx, sandboxID)
		if err != nil {
			return err
		}
		if soloReviewDigest(current) != row.SourceFingerprint {
			return ErrConflict
		}
		if err := a.guardSoloReviewParentsTx(tx, permit, current); err != nil {
			return err
		}
		changed := tx.Model(&models.ExecutionQuarantine{}).Where("id = ? AND dataset_id = ? AND entity_kind = ? AND entity_id = ? AND review_state = 'pending'", row.QuarantineID, row.DatasetID, EntitySandbox, strconv.FormatUint(uint64(sandboxID), 10)).Update("review_state", "reviewed")
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrConflict
		}
		changed = tx.Table("sandboxes").Where("id = ? AND name = ? AND generation = ? AND COALESCE(operation_owner,'') = ''", sandboxID, row.ProviderID, row.OriginalGeneration).Updates(map[string]any{"status": "stopped", "desired_state": "stopped", "destroy_at": nil, "error": "", "authorization_fingerprint": "", "generation": gorm.Expr("COALESCE(generation,0)+1")})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrConflict
		}
		changed = tx.Table("solo_sessions").Where("id = ? AND sandbox_id = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = ''", current.SoloID, sandboxID, current.Epoch, current.Attempt).Updates(map[string]any{"status": models.SoloStatusStopped, "error": "", "resume_context_pending": true})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrConflict
		}
		now := time.Now().UTC()
		row.State, row.StoppedAt, row.CompletedAt = "completed", &now, &now
		changed = tx.Model(&SoloEnvironmentReview{}).Where("id = ? AND state = 'dispatching' AND completed_at IS NULL", row.ID).Updates(map[string]any{"state": row.State, "stopped_at": now, "completed_at": now})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrConflict
		}
		return a.GuardTx(tx, permit, "", "")
	})
	return row, err
}

// FinalizeSoloEnvironmentReview releases only this original offline operation.
// It re-observes the same stopped provider and never dispatches another stop.
func (a *SQLAuthority) FinalizeSoloEnvironmentReview(ctx context.Context, db *gorm.DB, owner string, sandboxID uint, provider SoloReviewProvider, authorize SoloReviewAuthorization) (SoloEnvironmentReview, error) {
	row, err := a.ReviewSoloEnvironment(ctx, db, owner, sandboxID, provider, authorize)
	if err != nil {
		return row, err
	}
	permit, err := a.soloReviewPermit(ctx, db, owner)
	if err == nil && (permit.DatasetID() != row.DatasetID || permit.Lineage() != row.Lineage) {
		err = ErrConflict
	}
	if err != nil {
		return row, err
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := a.guardSoloReviewTx(tx, owner, permit); err != nil {
			return err
		}
		current, err := soloReviewSourceTx(tx, sandboxID)
		if err != nil {
			return err
		}
		if current.Name != row.ProviderID || current.SoloID != row.SoloID || current.Generation != row.OriginalGeneration+1 || current.SandboxStatus != "stopped" || current.DesiredState != "stopped" || current.SoloStatus != models.SoloStatusStopped || soloReviewBindingDigest(current) != row.BindingFingerprint {
			return ErrConflict
		}
		if err := a.guardSoloReviewParentsTx(tx, permit, current); err != nil {
			return err
		}
		now := time.Now().UTC()
		changed := tx.Model(&SoloEnvironmentReview{}).Where("id = ? AND state = 'completed' AND completed_at IS NOT NULL AND finalized_at IS NULL", owner).Updates(map[string]any{"state": "finalized", "finalized_at": now})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrConflict
		}
		changed = tx.Model(&storageguard.Control{}).Where("name = 'primary' AND owner = ?", owner).Updates(map[string]any{"owner": "", "revision": gorm.Expr("revision + 1")})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrInactive
		}
		row.State, row.FinalizedAt = "finalized", &now
		return a.GuardTx(tx, permit, "", "")
	})
	return row, err
}
