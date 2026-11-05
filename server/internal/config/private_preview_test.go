package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivatePreviewConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, preview, public, mode string
		private, wantError          bool
	}{
		{name: "unchanged default"},
		{name: "missing isolated origin", private: true, wantError: true},
		{name: "isolated origin", private: true, preview: "https://preview.example.net", public: "https://grasp.example.com"},
		{name: "same origin", private: true, preview: "https://grasp.example.com", public: "https://grasp.example.com", wantError: true},
		{name: "platform in reserved wildcard", private: true, preview: "https://example.com", public: "https://grasp.example.com", wantError: true},
		{name: "IP cannot route wildcard", private: true, preview: "http://127.0.0.1", wantError: true},
		{name: "production rejects same cookie site", private: true, preview: "https://preview.example.com", public: "https://grasp.example.com", mode: "production", wantError: true},
		{name: "production distinct cookie sites", private: true, preview: "https://preview.example.net", public: "https://grasp.example.com", mode: "production"},
		{name: "trimmed production mode", private: true, preview: "https://preview.example.com", public: "https://grasp.example.com", mode: " production ", wantError: true},
		{name: "local development", private: true, preview: "http://preview.localhost:8080", public: "http://grasp.localhost:8080", mode: "development"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GRASP_SANDBOX_PRIVATE_ENDPOINTS", "false")
			if tc.private {
				t.Setenv("GRASP_SANDBOX_PRIVATE_ENDPOINTS", "true")
			}
			t.Setenv("GRASP_PREVIEW_ADVERTISE", tc.preview)
			t.Setenv("GRASP_PUBLIC_ADVERTISE", tc.public)
			t.Setenv("GRASP_DEPLOYMENT_MODE", tc.mode)
			file := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(file, []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := parse(file)
			if (err != nil) != tc.wantError {
				t.Fatalf("parse error = %v, wantError %v", err, tc.wantError)
			}
			if err == nil && cfg.Sandbox.PrivateEndpoints != tc.private {
				t.Fatal("private endpoint override ignored")
			}
		})
	}
}

func TestPrivatePreviewRejectsInvalidBoolean(t *testing.T) {
	t.Setenv("GRASP_SANDBOX_PRIVATE_ENDPOINTS", "tru")
	if _, err := parse(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("invalid opt-in silently disabled private endpoints")
	}
}
