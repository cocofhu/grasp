package services

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestValidateRunSandboxEnvOK(t *testing.T) {
	got, err := ValidateRunSandboxEnv([]models.EnvEntry{
		{Key: "", Value: ""},
		{Key: " LOG_LEVEL ", Value: "debug"},
		{Key: "EMPTY", Value: ""},
		{Key: "SECRET", Value: "s3cret", Secret: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(got), got)
	}
	if got[0].Key != "LOG_LEVEL" || got[0].Value != "debug" {
		t.Fatalf("got[0]=%+v", got[0])
	}
	if got[1].Key != "EMPTY" || got[1].Value != "" {
		t.Fatalf("empty override %+v", got[1])
	}
	if !got[2].Secret || got[2].Value != "s3cret" {
		t.Fatalf("secret %+v", got[2])
	}
}

func TestValidateRunSandboxEnvRejects(t *testing.T) {
	cases := []struct {
		name string
		in   []models.EnvEntry
		want string
	}{
		{
			name: "missing key",
			in:   []models.EnvEntry{{Key: "", Value: "x"}},
			want: "row 1",
		},
		{
			name: "duplicate",
			in: []models.EnvEntry{
				{Key: "A", Value: "1"},
				{Key: "A", Value: "2"},
			},
			want: "A (duplicate)",
		},
		{
			name: "auth key",
			in:   []models.EnvEntry{{Key: "CURSOR_API_KEY", Value: "k"}},
			want: "CURSOR_API_KEY",
		},
		{
			name: "password",
			in:   []models.EnvEntry{{Key: "ACP_BRIDGE_PASSWORD", Value: "p"}},
			want: "ACP_BRIDGE_PASSWORD",
		},
		{
			name: "alias",
			in:   []models.EnvEntry{{Key: "GRASP_CURSOR_API_KEY", Value: "k"}},
			want: "GRASP_CURSOR_API_KEY",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateRunSandboxEnv(tc.in)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidateRunSandboxEnvNilEmpty(t *testing.T) {
	got, err := ValidateRunSandboxEnv(nil)
	if err != nil || got != nil {
		t.Fatalf("nil: got=%v err=%v", got, err)
	}
	got, err = ValidateRunSandboxEnv([]models.EnvEntry{{Key: "", Value: ""}})
	if err != nil || got != nil {
		t.Fatalf("empty rows: got=%v err=%v", got, err)
	}
}
