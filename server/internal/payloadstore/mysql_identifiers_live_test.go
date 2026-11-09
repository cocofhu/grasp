package payloadstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
)

// This test requires a disposable, empty database. It exercises real MySQL
// parsing of StateRun.usage in both normalization and destination-only closure.
func TestMySQLLiveQuotedPayloadProjectionAndClosure(t *testing.T) {
	dsn := os.Getenv("GRASP_MYSQL_PAYLOAD_TEST_DSN")
	if dsn == "" {
		t.Skip("GRASP_MYSQL_PAYLOAD_TEST_DSN is unset; provide a disposable empty MySQL 8.4+ database")
	}
	ctx := context.Background()
	cfg := config.DatabaseConfig{Driver: "mysql", DSN: dsn}
	root := filepath.Join(t.TempDir(), "payloads")
	probe, err := database.ConnectWithFiles(cfg, root)
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
		t.Fatal("live payload regression refuses a nonempty database")
	}
	db, err := database.OpenWithFiles(cfg, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(db) }()
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	objects := &replicaStore{objects: map[string]replicaBytes{}}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	const original = `{"inputTokens":2}`
	if err := db.Table("state_runs").Create(map[string]any{"id": 7, "run_id": "quoted-history", "usage": original, "output_md": "original output"}).Error; err != nil {
		t.Fatal(err)
	}
	report, err := filestore.Migrate(ctx, db, models.AllModels()...)
	if err != nil || report.Migrated != 1 || report.Deferred != 0 || len(report.Failures) != 0 {
		t.Fatalf("MySQL reserved payload projection failed: %+v %v", report, err)
	}
	if _, err := files.ArchiveLocal(ctx); err != nil {
		t.Fatal(err)
	}
	refs, err := VerifyReferences(ctx, db, objects)
	if err != nil || refs.Payloads < 1 {
		t.Fatalf("MySQL destination-only closure failed: %+v %v", refs, err)
	}
	var stored string
	if err := db.Table("state_runs").Select("`usage`").Where("id = ?", 7).Row().Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != original {
		t.Fatal("small usage metadata was unexpectedly externalized or changed")
	}
	if err := db.Table("state_runs").Select("output_md").Where("id = ?", 7).Row().Scan(&stored); err != nil || !filestore.IsRef(stored) {
		t.Fatal("history output was not externalized", err)
	}
	actual, err := files.Load(ctx, stored, filestore.Text)
	if err != nil || string(actual) != "original output" {
		t.Fatalf("history output changed during normalization: %v", err)
	}
}
