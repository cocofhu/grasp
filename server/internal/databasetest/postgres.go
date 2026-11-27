// Package databasetest provides database fixtures for integration tests.
package databasetest

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PostgresDSN returns a DSN for a schema isolated to the test and skips when
// the integration database is not configured.
func PostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set TEST_POSTGRES_DSN to a PostgreSQL test database URI")
	}
	uri, err := url.Parse(dsn)
	if err != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") {
		t.Fatal("TEST_POSTGRES_DSN must be a PostgreSQL URI")
	}
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("connect to PostgreSQL test database: %v", err)
	}
	schema := pgx.Identifier{"grasp_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		defer func() { _ = conn.Close(ctx) }()
		if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	if _, err := conn.Exec(t.Context(), "CREATE SCHEMA "+schema.Sanitize()); err != nil {
		t.Fatal(err)
	}
	query := uri.Query()
	query.Set("search_path", schema[0])
	uri.RawQuery = query.Encode()
	return uri.String()
}
