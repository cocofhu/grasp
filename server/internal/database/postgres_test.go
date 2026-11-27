package database_test

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/databasetest"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func TestPostgresStartupWaitsForSchemaLock(t *testing.T) {
	dsn := databasetest.PostgresDSN(t)
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(t.Context()) }()
	if _, err := conn.Exec(t.Context(), "SELECT pg_advisory_lock(hashtext('grasp.schema.startup'), hashtext(current_schema()::text))"); err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := uri.Query()
	query.Set("lock_timeout", "300ms")
	uri.RawQuery = query.Encode()
	cfg := config.DatabaseConfig{Driver: "postgres", DSN: uri.String()}
	db, err := database.OpenWithFiles(cfg, t.TempDir())
	if db != nil {
		_ = database.Close(db)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("startup should wait for the schema lock: %v", err)
	}
	var exists bool
	if err := conn.QueryRow(t.Context(), "SELECT to_regclass('sandboxes') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("sandboxes table created while schema lock was held: exists=%v err=%v", exists, err)
	}
}

func TestPostgresPersistence(t *testing.T) {
	cfg := config.DatabaseConfig{Driver: "postgres", DSN: databasetest.PostgresDSN(t)}
	root := filepath.Join(t.TempDir(), "payloads")
	open := func() *gorm.DB {
		t.Helper()
		db, err := database.OpenWithFiles(cfg, root)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close(db) })
		return db
	}
	db := open()
	for _, m := range models.AllModels() {
		if !db.Migrator().HasTable(m) {
			t.Fatalf("missing PostgreSQL table for %T", m)
		}
	}
	run := models.Run{ID: "tagged", Status: "completed", Tags: []string{"database", "迁移"}, Inputs: map[string]any{"prompt": "check PostgreSQL"}}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	other := []models.Run{
		{ID: "one-tag", Tags: []string{"database"}},
		{ID: "different-case", Tags: []string{"Database", "迁移"}},
		{ID: "no-tags"},
	}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	artifacts := services.NewArtifactService(db)
	if _, err := artifacts.Save(run.ID, "node", "Report_final.md", "markdown", "persistent report"); err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.Save(run.ID, "node", "ReportXfinal.md", "markdown", "other report"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(db); err != nil {
		t.Fatal(err)
	}

	db = open()
	ws, err := services.NewWorkspaceService(db).Workspace()
	if err != nil || ws.ID != models.WorkspaceID {
		t.Fatalf("workspace on restart: id=%q err=%v", ws.ID, err)
	}
	rows, total, err := services.NewRunService(db).ListPageByTags(nil, "", []string{"database", "迁移"}, 1, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != run.ID || rows[0].Inputs["prompt"] != "check PostgreSQL" {
		t.Fatalf("tag filter after restart: rows=%+v total=%d err=%v", rows, total, err)
	}
	artifacts = services.NewArtifactService(db)
	if content, ok := artifacts.Get(run.ID, "Report_final.md"); !ok || content != "persistent report" {
		t.Fatalf("artifact after restart: content=%q found=%v", content, ok)
	}
	list, total := artifacts.AllPage(services.ArtifactFilter{Q: "report_"}, 1, 10)
	if total != 1 || len(list) != 1 || list[0].Name != "Report_final.md" {
		t.Fatalf("artifact search: rows=%+v total=%d", list, total)
	}
	if err := db.Exec("ALTER TABLE sandboxes ADD COLUMN workload_released boolean").Error; err != nil {
		t.Fatal(err)
	}
	wrongDB, err := database.OpenWithFiles(cfg, root)
	if err == nil {
		_ = database.Close(wrongDB)
		t.Fatal("opened a schema containing the gateway's sandbox column")
	}
	if !strings.Contains(err.Error(), "separate Grasp database") {
		t.Fatalf("unexpected error for a shared database: %v", err)
	}
}

func TestPostgresSearchMatchesSQLite(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			cfg := config.DatabaseConfig{Driver: driver}
			if driver == "postgres" {
				cfg.DSN = databasetest.PostgresDSN(t)
			}
			dir := t.TempDir()
			cfg.Path = filepath.Join(dir, "grasp.db")
			db, err := database.OpenWithFiles(cfg, filepath.Join(dir, "payloads"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close(db) })

			t.Run("solo references", func(t *testing.T) {
				sessions := []models.SoloSession{
					{ID: "wanted", UserID: "alice", Title: "CLI_100% report"},
					{ID: "wildcard", UserID: "alice", Title: "CLIX1000 report"},
					{ID: "other-user", UserID: "bob", Title: "CLI_100% report"},
				}
				if err := db.Create(&sessions).Error; err != nil {
					t.Fatal(err)
				}
				solo := services.NewSoloService(db, nil, nil, nil)
				scope := services.ReferenceScope("alice", "", nil)
				results, err := solo.SearchScopedReferences(t.Context(), scope,
					services.ReferenceSearchQuery{Lane: "solo", Query: "cli_100%"})
				if err != nil {
					t.Fatal(err)
				}
				items := results.Items.([]services.SoloReferenceItem)
				if len(items) != 1 || items[0].Target != "wanted" {
					t.Fatalf("Solo reference search returned %+v", items)
				}
			})

			t.Run("audit", func(t *testing.T) {
				audit := services.NewAuditService(db)
				audit.Record(services.AuditRecord{
					Action: "workflow.save", ResourceType: "workflow", ResourceID: "Example",
					Summary: `Saved workflow/Example to C:\Workspace`,
				})
				for _, filter := range []services.AuditListFilter{
					{Search: `c:\workspace`},
					{Resource: `c:\workspace`},
					{Resource: "workflow/example"},
					{Action: "WORKFLOW"},
				} {
					items, total, err := audit.ListPage(filter)
					if err != nil || total != 1 || len(items) != 1 {
						t.Errorf("audit filter %+v: total=%d rows=%d err=%v", filter, total, len(items), err)
					}
				}
			})

			t.Run("artifacts", func(t *testing.T) {
				artifacts := []models.Artifact{
					{ID: "report", Name: "Report.md"},
					{ID: "ledger", Name: "Feedback.history.md"},
				}
				if err := db.Create(&artifacts).Error; err != nil {
					t.Fatal(err)
				}
				svc := services.NewArtifactService(db)
				items, total := svc.AllPage(services.ArtifactFilter{}, 1, 10)
				if total != 1 || len(items) != 1 || items[0].ID != "report" {
					t.Errorf("artifact list: total=%d items=%+v", total, items)
				}
				tree, err := svc.Tree()
				if err != nil || tree.Count != 1 {
					t.Errorf("artifact tree: count=%d err=%v", tree.Count, err)
				}
				localized := []models.Artifact{
					{ID: "upper", Name: "Ébauche.md"},
					{ID: "lower", Name: "ébauche.md"},
				}
				if err := db.Create(&localized).Error; err != nil {
					t.Fatal(err)
				}
				items, total = svc.AllPage(services.ArtifactFilter{Q: "Ébauche"}, 1, 10)
				if total != 1 || len(items) != 1 || items[0].ID != "lower" {
					t.Errorf("localized artifact search: total=%d items=%+v", total, items)
				}
			})
		})
	}
}
