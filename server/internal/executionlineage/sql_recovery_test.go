package executionlineage

import (
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestInitializationLostTicketAcknowledgementReturnsOriginalSQLIntent(t *testing.T) {
	db, a, owner := authorityFixture(t)
	first, err := a.Initialize(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQL(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reopened.Initialize(t.Context(), owner)
	if err != nil || first.Document() != second.Document() {
		t.Fatal("lost local-write acknowledgement rotated initialization", err)
	}
	var count int64
	if err := db.Model(&models.ExecutionRecovery{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
	// Even a mistakenly released SQL maintenance flag cannot hide the durable
	// pending operation from startup or the generic release preflight.
	if err := storageguard.Leave(t.Context(), db, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReadActive(t.Context()); !errors.Is(err, ErrInactive) {
		t.Fatal("lost ticket became executable", err)
	}
	closed, err := NewLocalController(t.Context(), db)
	if !errors.Is(err, ErrInactive) || closed == nil {
		t.Fatal("startup ignored pending initialization", err)
	}
}

func TestRestoreLostTicketAcknowledgementPreservesTargetAndBackup(t *testing.T) {
	db, a, owner := authorityFixture(t)
	activateFixture(t, db, a, owner)
	owner = nextMaintenance(t, db, owner)
	backup := strings.Repeat("b", 64)
	first, err := a.BeginRestore(t.Context(), owner, backup)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQL(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reopened.BeginRestore(t.Context(), owner, backup)
	if err != nil || first.Document() != second.Document() {
		t.Fatal("lost local ticket write rotated restore generation", err)
	}
	if _, err := reopened.BeginRestore(t.Context(), owner, strings.Repeat("c", 64)); !errors.Is(err, ErrConflict) {
		t.Fatal("retry changed its original backup", err)
	}
	for _, document := range []TicketDocument{{Version: 1, Marker: first.Document().Marker}, {Version: 2, Protocol: "foreign", Marker: first.Document().Marker}} {
		if _, err := reopened.Resume(t.Context(), document); !errors.Is(err, ErrConflict) {
			t.Fatal("legacy or foreign ticket was adopted", err)
		}
	}
	if err := storageguard.Check(t.Context(), db, owner); err != nil {
		t.Fatal("failure released maintenance", err)
	}
}

func TestImportedRestoreRequiresExplicitOriginalBackupRebinding(t *testing.T) {
	source, a, owner := authorityFixture(t)
	_, before := activateFixture(t, source, a, owner)
	var old models.ExecutionDataset
	if err := source.First(&old).Error; err != nil {
		t.Fatal(err)
	}
	owner = nextMaintenance(t, source, owner)
	backup := strings.Repeat("d", 64)
	original, err := a.BeginRestore(t.Context(), owner, backup)
	if err != nil {
		t.Fatal(err)
	}
	// Model importing a backup into a separate SQL database: it contains the
	// old binding, but not the new restore-begin intent committed afterwards.
	target, restored, targetOwner := authorityFixture(t)
	if err := target.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(t.Context(), target, targetOwner); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Enter(t.Context(), target, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Resume(t.Context(), original.Document()); !errors.Is(err, ErrConflict) {
		t.Fatal("bare JSON registered an imported recovery", err)
	}
	if _, err := restored.ResumeImported(t.Context(), original.Document(), strings.Repeat("e", 64)); !errors.Is(err, ErrConflict) {
		t.Fatal("wrong backup re-registered intent", err)
	}
	ticket, err := restored.ResumeImported(t.Context(), original.Document(), backup)
	if err != nil || ticket.Document() != original.Document() {
		t.Fatal(err)
	}
	commitQuarantine(t, target, restored, owner, ticket)
	permit, err := restored.Activate(t.Context(), target, owner, ticket)
	if err != nil || permit.DatasetID() == before.DatasetID() || permit.Lineage() == before.Lineage() {
		t.Fatal("restore reused original execution identity", err)
	}
	if _, err := a.ReadActive(t.Context()); !errors.Is(err, ErrInactive) {
		t.Fatal("target finalize released original SQL source", err)
	}
	if err := storageguard.Leave(t.Context(), target, owner); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewLocalController(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err != nil {
		t.Fatal("finalized imported binding cannot serve new work", err)
	}
}

func TestImportedUnboundBackupKeepsUnknownSourceAndQuarantinesOldWork(t *testing.T) {
	source, a, owner := authorityFixture(t)
	_, before := activateFixture(t, source, a, owner)
	owner = nextMaintenance(t, source, owner)
	backup := strings.Repeat("c", 64)
	original, err := a.BeginRestore(t.Context(), owner, backup)
	if err != nil {
		t.Fatal(err)
	}
	// The restore starts from a bound installation, but its verified backup
	// predates execution bindings. Its source identity must remain unknown.
	target, restored, targetOwner := authorityFixture(t)
	if err := target.Table("runs").Create(map[string]any{"id": "old-pending", "status": "queued"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(t.Context(), target, targetOwner); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Enter(t.Context(), target, owner, nil); err != nil {
		t.Fatal(err)
	}
	ticket, err := restored.ResumeImported(t.Context(), original.Document(), backup)
	if err != nil {
		t.Fatal(err)
	}
	binding := commitQuarantine(t, target, restored, owner, ticket)
	if binding.SourceDatasetID != "" || binding.SourceLineage != "" || binding.BackupSHA256 != backup || binding.QuarantineCount != 1 {
		t.Fatal("unbound backup acquired invented source provenance", binding)
	}
	if ticket.Document().Marker.PreviousDatasetID != before.DatasetID() || ticket.Document().Marker.PreviousLineage != before.Lineage() {
		t.Fatal("restore ticket lost the actual begin-time binding")
	}
	permit, err := restored.Activate(t.Context(), target, owner, ticket)
	if err != nil || permit.DatasetID() != binding.DatasetID || permit.Lineage() != binding.Lineage {
		t.Fatal("verified unbound backup could not finalize", err)
	}
	if err := storageguard.Leave(t.Context(), target, owner); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewLocalController(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Check(t.Context()); err != nil {
		t.Fatal("restored unbound backup cannot admit new work", err)
	}
	if err := runtime.Check(t.Context(), Entity{Kind: EntityRun, ID: "old-pending"}); !errors.Is(err, ErrQuarantined) {
		t.Fatal("unbound source made old pending work replayable", err)
	}
}

func TestUnboundBackupProvenanceIsValidOnlyForActiveBinding(t *testing.T) {
	marker := Marker{Version: 1, NamespaceID: uuid.NewString(), DatasetID: uuid.NewString(), Lineage: uuid.NewString(), OperationID: uuid.NewString(), Revision: 4, State: Active, BackupSHA256: strings.Repeat("a", 64), UpdatedAt: time.Now().UTC()}
	if !marker.valid() {
		t.Fatal("active binding rejected a verified backup with unknown source")
	}
	for _, scenario := range []string{"restoring", "initializing", "only_dataset", "only_lineage", "bad_hash", "bad_source"} {
		t.Run(scenario, func(t *testing.T) {
			changed := marker
			switch scenario {
			case "restoring":
				changed.State = Restoring
			case "initializing":
				changed.State, changed.Revision = Initializing, 1
			case "only_dataset":
				changed.PreviousDatasetID = uuid.NewString()
			case "only_lineage":
				changed.PreviousLineage = uuid.NewString()
			case "bad_hash":
				changed.BackupSHA256 = "unverified"
			case "bad_source":
				changed.PreviousDatasetID, changed.PreviousLineage = "unknown", "unknown"
			}
			if changed.valid() {
				t.Fatal("incomplete provenance or recovery intent was accepted")
			}
		})
	}
}

func TestRecoveryMetadataCannotBeTamperedOrFinalizedByAnotherOwner(t *testing.T) {
	for _, scenario := range []string{"hash", "marker", "unknown_state", "owner", "runtime"} {
		t.Run(scenario, func(t *testing.T) {
			db, a, owner := authorityFixture(t)
			ticket, err := a.Initialize(t.Context(), owner)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "hash":
				err = db.Model(&models.ExecutionRecovery{}).Where("id = ?", owner).Update("ticket_sha256", strings.Repeat("a", 64)).Error
			case "marker":
				err = db.Model(&models.ExecutionRecovery{}).Where("id = ?", owner).Update("marker_json", "{}").Error
			case "unknown_state":
				err = db.Model(&models.ExecutionRecovery{}).Where("id = ?", owner).Update("state", "unknown").Error
			case "owner":
				err = db.Model(&storageguard.Control{}).Where("name = 'primary'").Update("owner", uuid.NewString()).Error
			case "runtime":
				err = db.Create(&storageguard.Runtime{ID: uuid.NewString()}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.Resume(t.Context(), ticket.Document()); err == nil {
				t.Fatal("changed recovery resumed")
			}
			if err := db.Transaction(func(tx *gorm.DB) error { _, err := a.QuarantineTx(tx, owner, ticket); return err }); err == nil {
				t.Fatal("changed recovery quarantined/published SQL")
			}
			if _, err := a.Activate(t.Context(), db, owner, ticket); err == nil {
				t.Fatal("changed recovery finalized")
			}
			var count int64
			if err := db.Model(&models.ExecutionDataset{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("failed recovery wrote a binding", count, err)
			}
		})
	}
}

func TestPreparedSQLProofSurvivesSlowStagingButNeverAdoptsRestoredBinding(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		db, a, owner := authorityFixture(t)
		defer func() { _ = database.Close(db) }()
		activateFixture(t, db, a, owner)
		if err := storageguard.Leave(t.Context(), db, owner); err != nil {
			t.Fatal(err)
		}
		runtime, err := NewLocalController(t.Context(), db)
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := runtime.PrepareContext(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Hour) // Synthetic time: a legitimate long object PUT.
		if err := db.Transaction(func(tx *gorm.DB) error { return runtime.GuardContextTx(prepared, tx) }); err != nil {
			t.Fatal("SQL proof acquired an unrelated wall-clock TTL", err)
		}
		owner = uuid.NewString()
		if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
			t.Fatal(err)
		}
		ticket, err := a.BeginRestore(t.Context(), owner, strings.Repeat("f", 64))
		if err != nil {
			t.Fatal(err)
		}
		commitQuarantine(t, db, a, owner, ticket)
		if _, err := a.Activate(t.Context(), db, owner, ticket); err != nil {
			t.Fatal(err)
		}
		if err := storageguard.Leave(t.Context(), db, owner); err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error { return runtime.GuardContextTx(prepared, tx) }); !errors.Is(err, ErrUnbound) {
			t.Fatal("old prepared proof adopted a restored SQL generation", err)
		}
		if _, err := runtime.Current(t.Context()); !errors.Is(err, ErrUnbound) {
			t.Fatal("old process cached its lineage as current", err)
		}
		restarted, err := NewLocalController(t.Context(), db)
		if err != nil || restarted.DatasetID() == runtime.DatasetID() {
			t.Fatal("restart did not bind restored dataset", err)
		}
		if err := restarted.Check(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
}
