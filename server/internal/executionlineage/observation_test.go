package executionlineage

import (
	"errors"
	"reflect"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
)

func TestSQLObservationIsReadOnlyAndReportsMaintenance(t *testing.T) {
	db, a, owner := authorityFixture(t)
	_, permit := activateFixture(t, db, a, owner)
	before, err := db.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	view, err := ObserveSQL(t.Context(), db)
	if !errors.Is(err, ErrInactive) || view.Active || !view.SQLMatches || !view.MaintenanceActive || view.DatasetID != permit.DatasetID() {
		t.Fatal("maintenance was reported as executable", view, err)
	}
	if err := storageguard.Leave(t.Context(), db, owner); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("PRAGMA query_only=ON").Error; err != nil {
		t.Fatal(err)
	}
	view, err = ObserveSQL(t.Context(), db)
	if err != nil || !view.Active || view.Status != "active" || !view.SQLMatches || !view.SQLDatasetPresent {
		t.Fatal(view, err)
	}
	after, err := db.Migrator().GetTables()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("status initialized metadata", before, after, err)
	}
}

func TestSQLObservationOnlyKnownRecoveryTableCanBeAbsent(t *testing.T) {
	db, a, owner := authorityFixture(t)
	activateFixture(t, db, a, owner)
	if err := storageguard.Leave(t.Context(), db, owner); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&models.ExecutionRecovery{}); err != nil {
		t.Fatal(err)
	}
	view, err := ObserveSQL(t.Context(), db)
	if err != nil || !view.Active {
		t.Fatal("legacy existing binding became unreadable", view, err)
	}
	if _, err := a.ReadActive(t.Context()); err != nil {
		t.Fatal("legacy offline copy/status became unavailable", err)
	}
	controller, err := NewLocalController(t.Context(), db)
	if err == nil || controller == nil {
		t.Fatal("runtime silently treated lost recovery metadata as legacy", err)
	}
	if err := db.Migrator().DropTable(&storageguard.Control{}); err != nil {
		t.Fatal(err)
	}
	view, err = ObserveSQL(t.Context(), db)
	if !errors.Is(err, ErrUnavailable) || view.Active {
		t.Fatal("missing maintenance table was ignored", view, err)
	}
}

func TestSQLObservationPendingUnknownAndMetadataFailureStayClosed(t *testing.T) {
	view, err := ObserveSQL(t.Context(), nil)
	if !errors.Is(err, ErrUninitialized) || view.Status != "uninitialized" || view.SQLDatasetPresent {
		t.Fatal(view, err)
	}
	db, a, owner := authorityFixture(t)
	activateFixture(t, db, a, owner)
	if err := storageguard.Leave(t.Context(), db, owner); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.ExecutionRecovery{ID: "unknown-recovery", State: "unknown"}).Error; err != nil {
		t.Fatal(err)
	}
	view, err = ObserveSQL(t.Context(), db)
	if !errors.Is(err, ErrInactive) || view.Active || !view.SQLDatasetPresent {
		t.Fatal("unknown recovery allowed serving", view, err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	view, err = ObserveSQL(t.Context(), db)
	if !errors.Is(err, ErrUnavailable) || view.Status != "unavailable" {
		t.Fatal("SQL failure looked uninitialized", view, err)
	}
}
