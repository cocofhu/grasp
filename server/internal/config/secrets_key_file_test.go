package config

import (
	"path/filepath"
	"testing"
)

func TestSecretsKeyFile(t *testing.T) {
	dbDir := t.TempDir()
	cases := []struct {
		name   string
		mode   string
		driver string
		path   string
		want   string
	}{
		{"local-demo sqlite", "local-demo", "sqlite", filepath.Join(dbDir, "grasp.db"), filepath.Join(dbDir, "secrets.key")},
		{"development relative db", "development", "sqlite", "grasp.db", "secrets.key"},
		{"production requires a key", "production", "sqlite", filepath.Join(dbDir, "grasp.db"), ""},
		{"mysql has no file", "local-demo", "mysql", "", ""},
		{"in-memory has no file", "local-demo", "sqlite", ":memory:", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{}
			c.Server.DeploymentMode = tc.mode
			c.Database.Driver = tc.driver
			c.Database.Path = tc.path
			if got := c.SecretsKeyFile(); got != tc.want {
				t.Fatalf("SecretsKeyFile() = %q, want %q", got, tc.want)
			}
		})
	}
}
