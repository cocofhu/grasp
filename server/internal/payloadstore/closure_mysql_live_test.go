package payloadstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"gorm.io/gorm"
)

func TestMySQLLivePrimaryClosureKeepsOriginalReceiptAcrossConnections(t *testing.T) {
	dsn := os.Getenv("GRASP_MYSQL_PAYLOAD_TEST_DSN")
	if dsn == "" {
		t.Skip("GRASP_MYSQL_PAYLOAD_TEST_DSN must point to a disposable empty native MySQL 8.4+ database")
	}
	cfg := config.DatabaseConfig{Driver: "mysql", DSN: dsn}
	probe, err := database.ConnectWithFiles(cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tables, err := probe.Migrator().GetTables()
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(probe); err != nil {
		t.Fatal(err)
	}
	if len(tables) != 0 {
		t.Fatal("native closure regression refuses a nonempty database")
	}
	db, storage, owner, mutations := closureFixture(t, "valid", cfg)
	first, err := CertifyPrimary(t.Context(), db, storage, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.ConnectWithFiles(cfg, filepath.Join(storage.BlobsRoot, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(reopened) }()
	second, err := CertifyPrimary(t.Context(), reopened, storage, owner)
	if err != nil || !first.CompletedAt.Equal(second.CompletedAt) || first.ReferenceSHA256 != second.ReferenceSHA256 || first.Cells != second.Cells {
		t.Fatal("MySQL precision changed the immutable receipt across restart", first, second, err)
	}
	if err := reopened.Transaction(func(tx *gorm.DB) error { return CheckPrimaryClosure(t.Context(), tx, storage) }, &sql.TxOptions{ReadOnly: true}); err != nil {
		t.Fatal("startup proof changed metadata or used objects inside readonly SQL", err)
	}
	final, err := FinalizePrimaryClosure(t.Context(), reopened, storage, owner)
	if err != nil || final.FinalizedAt == nil || !final.CompletedAt.Equal(first.CompletedAt) || mutations.Load() != 0 {
		t.Fatal("native finalization changed the original result or used SQL cursor during GET", final, err, mutations.Load())
	}
}
