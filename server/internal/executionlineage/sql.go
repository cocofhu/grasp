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
	if db == nil {
		return ErrUnavailable
	}
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
	return db.WithContext(ctx).AutoMigrate(&models.ExecutionDataset{}, &models.ExecutionQuarantine{}, &models.ExecutionRecovery{})
}

func (a *SQLAuthority) validTicket(ticket Ticket) bool {
	return ticket.data != nil && ticket.data.authority == a && ticket.data.marker.valid() && (ticket.data.marker.State == Initializing || ticket.data.marker.State == Restoring)
}

// QuarantineTx only freezes SQL metadata. It preserves all user bodies and
// native unknown/dispatched states and deliberately provides no bulk replay or
// generic review-release API. The durable recovery stays pending until
// Activate validates this transaction's committed binding and quarantine set.
func (a *SQLAuthority) QuarantineTx(tx *gorm.DB, maintenanceID string, ticket Ticket) (models.ExecutionDataset, error) {
	var binding models.ExecutionDataset
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return binding, ErrTransaction
	}
	if !a.validTicket(ticket) {
		return binding, ErrConflict
	}
	if maintenanceID != ticket.data.marker.OperationID {
		return binding, ErrConflict
	}
	if err := offlineGuardTx(tx, maintenanceID); err != nil {
		return binding, err
	}
	var recovery models.ExecutionRecovery
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&recovery, "id = ?", maintenanceID).Error; err != nil || !recoveryMatches(recovery, ticket.Document()) {
		return binding, errors.Join(ErrConflict, err)
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
	if recovery.State == "finalized" || err == nil && (binding.OperationID == maintenanceID || target.State == Initializing) {
		return binding, ErrConflict
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

// Activate validates the committed binding and complete quarantine set, then
// finalizes the durable SQL recovery record. It never releases maintenance.
func (a *SQLAuthority) Activate(ctx context.Context, db *gorm.DB, maintenanceID string, ticket Ticket) (Permit, error) {
	if err := outsideSQLTransaction(db); err != nil {
		return Permit{}, err
	}
	if !a.validTicket(ticket) || ticket.data.marker.OperationID != maintenanceID {
		return Permit{}, ErrConflict
	}
	var binding models.ExecutionDataset
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := offlineGuardTx(tx, maintenanceID); err != nil {
			return err
		}
		var row models.ExecutionRecovery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row, "id = ?", maintenanceID).Error; err != nil || !recoveryMatches(row, ticket.Document()) {
			return errors.Join(ErrConflict, err)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&binding, "id = ?", "primary").Error; err != nil {
			return errors.Join(ErrUnbound, err)
		}
		if !bindingMatches(binding, ticket.data.marker, ticket.data.marker.Revision+1) {
			return ErrUnbound
		}
		count, fingerprint, err := quarantineFingerprint(tx, binding.DatasetID)
		if err != nil {
			return err
		}
		if binding.QuarantineCount != count || binding.QuarantineSHA256 != fingerprint {
			return ErrUnbound
		}
		if row.State == "finalized" {
			return nil
		}
		now := time.Now().UTC()
		changed := tx.Model(&models.ExecutionRecovery{}).Where("id = ? AND state = ? AND ticket_sha256 = ? AND finalized_at IS NULL", maintenanceID, ticket.data.marker.State, row.TicketSHA256).Updates(map[string]any{"state": "finalized", "finalized_at": now})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return Permit{}, err
	}
	// Read the committed stored values (including SQL timestamp precision).
	return a.ReadActive(ctx)
}

func (a *SQLAuthority) validPermit(permit Permit) bool {
	return permit.data != nil && permit.data.authority == a && permit.data.marker.State == Active
}

// CheckBinding compares a private preflight to current SQL and rejects every
// unfinished recovery, including one whose local workdir ticket was lost.
func (a *SQLAuthority) CheckBinding(ctx context.Context, db *gorm.DB, permit Permit) error {
	if !a.validPermit(permit) {
		return ErrUnavailable
	}
	if err := noPendingRecovery(db.WithContext(ctx)); err != nil {
		return err
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
func (a *SQLAuthority) GuardTx(tx *gorm.DB, permit Permit, kind EntityKind, id string) error {
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
		return ErrUnavailable
	}
	return nil
}
