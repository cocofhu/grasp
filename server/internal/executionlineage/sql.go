package executionlineage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EntityKind string

const (
	EntityRun           EntityKind = "run"
	EntityStateRun      EntityKind = "state_run"
	EntityChatTurn      EntityKind = "chat_turn"
	EntityQueue         EntityKind = "chat_queue"
	EntityLegacyPending EntityKind = "legacy_pending"
	EntitySolo          EntityKind = "solo"
	EntityPMDraft       EntityKind = "pm_draft"
	EntityCronJob       EntityKind = "cron_job"
	EntityCronRun       EntityKind = "cron_run"
	EntityInstruction   EntityKind = "workflow_instruction"
	EntityWorkflowEvent EntityKind = "workflow_event"
	EntityWorkspaceJob  EntityKind = "workspace_job"
	EntityRestore       EntityKind = "workspace_restore"
	EntityImport        EntityKind = "workspace_import"
	EntityHold          EntityKind = "payload_hold"
	EntitySandbox       EntityKind = "sandbox"
)

type quarantineSpec struct {
	kind         EntityKind
	table, state string
	where        string
	required     bool
}

// Only these named newer tables may be absent in a pre-upgrade backup. A
// present table must still contain every metadata column used below. There is
// no generic "missing table means no pending work" fallback for core Run data.
var quarantineSpecs = []quarantineSpec{
	{EntityRun, "runs", "status", "COALESCE(status, '') NOT IN ('completed','failed','cancelled')", true},
	{EntityStateRun, "state_runs", "status", "status IN ('running','pending','queued','waiting_human','paused')", true},
	{EntityQueue, "chat_queues", "'pending_or_owned'", "COALESCE(active_turn_id, '') <> '' OR COALESCE(owner, '') <> '' OR EXISTS (SELECT 1 FROM chat_queue_turns t WHERE t.queue_id = chat_queues.id AND t.state IN ('queued','executing','unknown'))", false},
	{EntityChatTurn, "chat_queue_turns", "state", "state IN ('queued','executing','unknown')", false},
	{EntityLegacyPending, "chat_pending_turns", "CASE WHEN started THEN 'dispatched_unknown' ELSE 'queued' END", "1 = 1", false},
	{EntitySolo, "solo_sessions", "status", "status IN ('creating','starting','restarting','recovering')", false},
	{EntityPMDraft, "chat_turn_drafts", "status", "status = 'streaming'", false},
	{EntityCronJob, "agent_cron_jobs", "'enabled_or_claimed'", "enabled = 1 OR claimed_at IS NOT NULL OR COALESCE(claim_owner, '') <> ''", false},
	{EntityCronRun, "agent_cron_runs", "status", "finished_at IS NULL", false},
	{EntityInstruction, "workflow_instructions", "state", "state IN ('queued','dispatching','dispatched','unknown')", false},
	{EntityWorkflowEvent, "workflow_conversation_events", "disposition", "disposition = 'pending'", false},
	{EntityWorkspaceJob, "workspace_snapshot_jobs", "state", "state NOT IN ('completed','ready','failed','cancelled')", false},
	{EntityRestore, "workspace_restores", "state", "state NOT IN ('restored','failed','cancelled')", false},
	{EntityImport, "workspace_imports", "state", "state NOT IN ('applied','failed','expired')", false},
	{EntityHold, "payload_upload_holds", "state", "state <> 'pinned'", false},
	{EntitySandbox, "sandboxes", "status", "status IN ('pulling','creating','starting','running','stopping')", false},
}

func knownKind(kind EntityKind) bool {
	for _, spec := range quarantineSpecs {
		if spec.kind == kind {
			return true
		}
	}
	return false
}

func outsideSQLTransaction(db *gorm.DB) error {
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return ErrTransaction
	}
	return nil
}

// EnsureRecoverySchema is explicitly offline and migrates only comparison /
// quarantine metadata, never application payload bodies or file serializers.
func EnsureRecoverySchema(ctx context.Context, db *gorm.DB, maintenanceID string) error {
	if err := outsideSQLTransaction(db); err != nil {
		return err
	}
	if err := storageguard.Check(ctx, db, maintenanceID); err != nil {
		return err
	}
	return db.WithContext(ctx).AutoMigrate(&models.ExecutionDataset{}, &models.ExecutionQuarantine{})
}

func (a *Authority) validTicket(ticket Ticket) bool {
	return ticket.data != nil && ticket.data.authority == a && ticket.data.marker.valid() && (ticket.data.marker.State == Initializing || ticket.data.marker.State == Restoring)
}

// QuarantineTx only freezes SQL metadata. It preserves all user bodies and
// native unknown/dispatched states and deliberately provides no bulk replay or
// generic review-release API. The marker stays inactive until Activate reads
// back this transaction's committed binding outside the transaction.
func (a *Authority) QuarantineTx(tx *gorm.DB, maintenanceID string, ticket Ticket) (models.ExecutionDataset, error) {
	var binding models.ExecutionDataset
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return binding, ErrTransaction
	}
	if !a.validTicket(ticket) {
		return binding, ErrConflict
	}
	var control storageguard.Control
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("name = ? AND owner = ?", "primary", maintenanceID).Take(&control).Error; err != nil || maintenanceID == "" {
		return binding, errors.Join(ErrInactive, err)
	}
	target := ticket.data.marker
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&binding, "id = ?", "primary").Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return binding, err
	}
	if err == nil && binding.DatasetID == target.DatasetID && binding.OperationID == target.OperationID {
		if !bindingMatches(binding, target, target.Revision+1) {
			return binding, ErrUnbound
		}
		return binding, nil // Retry must not quarantine post-activation new work.
	}
	sourceDataset, sourceLineage := binding.DatasetID, binding.Lineage
	for _, spec := range quarantineSpecs {
		if !tx.Migrator().HasTable(spec.table) {
			if spec.required {
				return binding, fmt.Errorf("required recovery metadata table %s is missing", spec.table)
			}
			continue
		}
		// Explicit projection avoids every ORM payload serializer. Page by PK
		// to bound driver buffers while the offline metadata transaction runs.
		where := spec.where
		if spec.kind == EntitySolo && tx.Migrator().HasColumn(spec.table, "lifecycle_action") {
			where += " OR COALESCE(lifecycle_action, '') <> ''"
		}
		if spec.kind == EntitySandbox && tx.Migrator().HasColumn(spec.table, "operation_owner") {
			where += " OR COALESCE(operation_owner, '') <> ''"
		}
		var after string
		for {
			var rows []struct{ ID, PriorState string }
			q := tx.Table(spec.table).Select("id, " + spec.state + " AS prior_state").Where(where).Order("id").Limit(256).Clauses(clause.Locking{Strength: "UPDATE"})
			if after != "" {
				q = q.Where("id > ?", after)
			}
			if err := q.Find(&rows).Error; err != nil {
				return binding, err
			}
			if len(rows) == 0 {
				break
			}
			entries := make([]models.ExecutionQuarantine, 0, len(rows))
			for _, row := range rows {
				if row.ID == "" || len(row.ID) > 191 {
					return binding, ErrConflict
				}
				body, _ := json.Marshal([]string{target.DatasetID, string(spec.kind), row.ID})
				sum := sha256.Sum256(body)
				entries = append(entries, models.ExecutionQuarantine{ID: hex.EncodeToString(sum[:]), DatasetID: target.DatasetID, OperationID: target.OperationID, EntityKind: string(spec.kind), EntityID: row.ID, PriorState: row.PriorState, SourceDatasetID: sourceDataset, SourceLineage: sourceLineage, ReviewState: "pending"})
			}
			if err := tx.Create(&entries).Error; err != nil {
				return binding, err
			}
			after = rows[len(rows)-1].ID
		}
	}
	count, fingerprint, err := quarantineFingerprint(tx, target.DatasetID)
	if err != nil {
		return binding, err
	}
	binding = models.ExecutionDataset{ID: "primary", NamespaceID: target.NamespaceID, DatasetID: target.DatasetID, Lineage: target.Lineage, ControlRevision: target.Revision + 1, OperationID: target.OperationID, SourceDatasetID: sourceDataset, SourceLineage: sourceLineage, BackupSHA256: target.BackupSHA256, QuarantineCount: count, QuarantineSHA256: fingerprint, BoundAt: time.Now().UTC()}
	if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&binding).Error; err != nil {
		return binding, err
	}
	return binding, nil
}

func quarantineFingerprint(db *gorm.DB, datasetID string) (int64, string, error) {
	count, after := int64(0), ""
	hash := sha256.New()
	for {
		var rows []models.ExecutionQuarantine
		query := db.Select("id", "entity_kind", "entity_id", "prior_state").Where("dataset_id = ?", datasetID).Order("id").Limit(256)
		if after != "" {
			query = query.Where("id > ?", after)
		}
		if err := query.Find(&rows).Error; err != nil {
			return 0, "", err
		}
		if len(rows) == 0 {
			return count, hex.EncodeToString(hash.Sum(nil)), nil
		}
		for _, row := range rows {
			if err := json.NewEncoder(hash).Encode([]string{row.ID, row.EntityKind, row.EntityID, row.PriorState}); err != nil {
				return 0, "", err
			}
		}
		count += int64(len(rows))
		after = rows[len(rows)-1].ID
	}
}

func bindingMatches(binding models.ExecutionDataset, marker Marker, revision uint64) bool {
	return binding.ID == "primary" && binding.NamespaceID == marker.NamespaceID && binding.DatasetID == marker.DatasetID && binding.Lineage == marker.Lineage && binding.OperationID == marker.OperationID && binding.ControlRevision == revision && binding.BackupSHA256 == marker.BackupSHA256
}

// Activate requires the SQL binding to exist after commit and the same offline
// maintenance owner to remain present. An uncommitted/rolled-back binding never
// activates the independently stored lineage.
func (a *Authority) Activate(ctx context.Context, db *gorm.DB, maintenanceID string, ticket Ticket) (Permit, error) {
	if err := outsideSQLTransaction(db); err != nil {
		return Permit{}, err
	}
	if !a.validTicket(ticket) {
		return Permit{}, ErrConflict
	}
	if err := storageguard.Check(ctx, db, maintenanceID); err != nil {
		return Permit{}, err
	}
	var binding models.ExecutionDataset
	if err := db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error; err != nil {
		return Permit{}, errors.Join(ErrUnbound, err)
	}
	target := ticket.data.marker
	if !bindingMatches(binding, target, target.Revision+1) {
		return Permit{}, ErrUnbound
	}
	// All quarantine records and their identities must survive the committed
	// SQL snapshot. Review-state changes do not erase the original frozen set.
	count, fingerprint, err := quarantineFingerprint(db.WithContext(ctx), target.DatasetID)
	if err != nil {
		return Permit{}, err
	}
	if binding.QuarantineCount != count || binding.QuarantineSHA256 != fingerprint {
		return Permit{}, ErrUnbound
	}
	current, info, err := a.read(ctx)
	if err != nil {
		return Permit{}, err
	}
	if activeChild(current, target) {
		return a.permit(current), nil
	}
	if current != target {
		return Permit{}, ErrConflict
	}
	current.State, current.Revision, current.UpdatedAt = Active, current.Revision+1, time.Now().UTC()
	if err := a.publish(ctx, current, info.ETag); err != nil {
		return Permit{}, err
	}
	return a.permit(current), nil
}

func (a *Authority) validPermit(permit Permit) bool {
	return permit.data != nil && permit.data.authority == a && permit.data.marker.State == Active && permit.data.expiresAt.After(time.Now())
}

// CheckBinding is the mandatory startup comparison after a fresh ReadActive.
// It uses only SQL; an old/restored SQL dataset cannot make itself authoritative.
func (a *Authority) CheckBinding(ctx context.Context, db *gorm.DB, permit Permit) error {
	if !a.validPermit(permit) {
		return ErrUnavailable
	}
	var binding models.ExecutionDataset
	if err := db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error; err != nil {
		return errors.Join(ErrUnbound, err)
	}
	if !bindingMatches(binding, permit.data.marker, permit.data.marker.Revision) {
		return ErrUnbound
	}
	if !a.validPermit(permit) {
		return ErrUnavailable
	}
	return nil
}

// GuardTx belongs in the actual dispatch/claim/publication transaction. Empty
// kind/id checks only the current dataset for admitting a brand new identity.
// Each existing source must also check its parent Run/Solo/Queue as applicable.
func (a *Authority) GuardTx(tx *gorm.DB, permit Permit, kind EntityKind, id string) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return ErrTransaction
	}
	if kind != "" && (!knownKind(kind) || id == "") || kind == "" && id != "" {
		return ErrConflict
	}
	if err := a.CheckBinding(tx.Statement.Context, tx.Clauses(clause.Locking{Strength: "SHARE"}), permit); err != nil {
		return err
	}
	if kind == "" {
		return nil
	}
	var rows []struct{ ID string }
	if err := tx.Table("execution_quarantines").Select("id").Where("entity_kind = ? AND entity_id = ? AND review_state = ?", string(kind), id, "pending").Limit(1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != 0 {
		return ErrQuarantined
	}
	if !a.validPermit(permit) {
		return ErrUnavailable // A SQL lock wait cannot extend proof lifetime.
	}
	return nil
}

func localGuardTx(tx *gorm.DB, entities []Entity) error {
	for _, entity := range entities {
		if !knownKind(entity.Kind) || entity.ID == "" {
			return ErrConflict
		}
		var rows []struct{ ID string }
		if err := tx.Table("execution_quarantines").Select("id").Where("entity_kind = ? AND entity_id = ? AND review_state = ?", string(entity.Kind), entity.ID, "pending").Limit(1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 0 {
			return ErrQuarantined
		}
	}
	return nil
}
