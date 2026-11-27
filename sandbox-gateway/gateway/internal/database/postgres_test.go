package database_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func postgresTestDatabase(t *testing.T) (config.DBConfig, *pgx.Conn) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to a PostgreSQL test database URI")
	}
	uri, err := url.Parse(dsn)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") {
		t.Fatal("TEST_POSTGRES_DSN must be a PostgreSQL URI")
	}
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to PostgreSQL test database: %v", err)
	}
	schema := pgx.Identifier{"gateway_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+schema.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "SELECT set_config('search_path', $1, false)", schema[0]); err != nil {
		t.Fatal(err)
	}
	query := uri.Query()
	query.Set("search_path", schema[0])
	uri.RawQuery = query.Encode()
	t.Setenv("SBGW_DB_DRIVER", "postgres")
	t.Setenv("SBGW_DB_DSN", uri.String())
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Database, conn
}

func TestPostgresStartupWaitsForSchemaLock(t *testing.T) {
	cfg, conn := postgresTestDatabase(t)
	if _, err := conn.Exec(t.Context(), "SELECT pg_advisory_lock(hashtext('grasp.schema.startup'), hashtext(current_schema()::text))"); err != nil {
		t.Fatal(err)
	}
	uri, err := url.Parse(cfg.DSN)
	if err != nil {
		t.Fatal(err)
	}
	query := uri.Query()
	query.Set("lock_timeout", "300ms")
	uri.RawQuery = query.Encode()
	cfg.DSN = uri.String()
	db, err := database.Open(cfg)
	if db != nil {
		pool, _ := db.DB()
		_ = pool.Close()
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

func TestPostgresSandboxPersistence(t *testing.T) {
	cfg, conn := postgresTestDatabase(t)
	ctx := t.Context()
	open := func() (*store.Store, func() error) {
		t.Helper()
		db, err := database.Open(cfg)
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = pool.Close() })
		return store.New(db), pool.Close
	}

	st, closeStore := open()
	sb := &models.Sandbox{ID: "test", Name: "sbx-test", Status: models.StatusCreating}
	sb.SetEndpoints(map[int]string{22: "127.0.0.1:2222"})
	sb.SetLabels(map[string]string{"owner": "test"})
	if err := st.Create(ctx, sb); err != nil {
		t.Fatal(err)
	}
	changed, err := st.TransitionStatus(ctx, sb.ID, models.StatusCreating, models.StatusRunning, "")
	if err != nil || !changed {
		t.Fatalf("sandbox transition: changed=%v err=%v", changed, err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}

	st, _ = open()
	rows, err := st.List(ctx, store.ListFilter{Labels: map[string]string{"owner": "test"}})
	if err != nil || len(rows) != 1 || rows[0].ID != sb.ID || rows[0].Status != models.StatusRunning || rows[0].Endpoints()[22] != "127.0.0.1:2222" {
		t.Fatalf("sandbox metadata after restart: rows=%+v err=%v", rows, err)
	}
	if _, err := conn.Exec(ctx, "ALTER TABLE sandboxes ADD COLUMN purpose text"); err != nil {
		t.Fatal(err)
	}
	wrongDB, err := database.Open(cfg)
	if err == nil {
		pool, _ := wrongDB.DB()
		_ = pool.Close()
		t.Fatal("opened a schema containing Grasp's sandbox column")
	}
	if !strings.Contains(err.Error(), "separate sandbox-gateway database") {
		t.Fatalf("unexpected error for a shared database: %v", err)
	}
}
