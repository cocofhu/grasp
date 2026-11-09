package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
)

func TestColdReservationAtomicIdentityAndReleaseFence(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	sb := models.Sandbox{ID: "original", Status: models.StatusStopped}
	sb.SetEnv(map[string]string{"old": "synthetic-old"})
	if err := st.Create(ctx, &sb); err != nil {
		t.Fatal(err)
	}
	in := models.ColdStart{Resource: sb.ID, RequestID: "review-original", InputSHA256: strings.Repeat("a", 64), PayloadSHA256: strings.Repeat("b", 64), State: "dispatching"}
	env := map[string]string{"GRASP_EXECUTION_LINEAGE": "current-lineage", "current": "synthetic-current"}
	got, admitted, err := st.ReserveColdStart(ctx, in, env)
	if err != nil || !admitted || got.RequestID != in.RequestID {
		t.Fatalf("reservation: %+v %v %v", got, admitted, err)
	}
	if err := st.GuardColdStart(ctx, sb.ID); !errors.Is(err, driver.ErrColdStartUnknown) {
		t.Fatal("pending native mutation was not fenced", err)
	}
	if _, again, err := st.ReserveColdStart(ctx, in, map[string]string{"GRASP_EXECUTION_LINEAGE": "current-lineage", "injected": "different"}); err != nil || again {
		t.Fatalf("accepted request not idempotent: %v %v", again, err)
	}
	row, _ := st.Get(ctx, sb.ID)
	if row.Env()["old"] != "" || row.Env()["current"] != "synthetic-current" || row.Env()["injected"] != "" {
		t.Fatal("reservation merged original/changed authorization")
	}
	for _, field := range []string{"nonce", "input", "payload"} {
		changed := in
		switch field {
		case "nonce":
			changed.RequestID = "another-request"
		case "input":
			changed.InputSHA256 = strings.Repeat("c", 64)
		case "payload":
			changed.PayloadSHA256 = strings.Repeat("d", 64)
		}
		if _, _, err := st.ReserveColdStart(ctx, changed, env); !errors.Is(err, driver.ErrColdStartConflict) {
			t.Fatalf("changed %s accepted: %v", field, err)
		}
	}
	if err := st.ConfirmColdStart(ctx, in, "original-native"); err != nil {
		t.Fatal(err)
	}
	if err := st.GuardColdStart(ctx, sb.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ConfirmColdStart(ctx, in, "foreign-native"); !errors.Is(err, driver.ErrColdStartConflict) {
		t.Fatal("native identity replaced", err)
	}
	if _, err := st.ColdStart(ctx, sb.ID, "other-request"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if saved, err := st.ColdStart(ctx, sb.ID, in.RequestID); err != nil || saved.State != "started" || saved.NativeIdentity != "original-native" {
		t.Fatalf("receipt: %+v %v", saved, err)
	}
	other := models.Sandbox{ID: "under-release", Status: models.StatusStopped}
	if err := st.Create(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if err := st.db.Create(&models.EnvironmentReleaseFence{Resource: other.ID, Lineage: "current-lineage", ActiveReleaseID: "explicit-release"}).Error; err != nil {
		t.Fatal(err)
	}
	in.Resource = other.ID
	if _, _, err := st.ReserveColdStart(ctx, in, env); !errors.Is(err, driver.ErrEnvironmentReleasePending) {
		t.Fatalf("cold start bypassed accepted release: %v", err)
	}
	if _, err := st.ColdStart(ctx, other.ID, in.RequestID); !errors.Is(err, ErrNotFound) {
		t.Fatal("rejected release race persisted cold dispatch", err)
	}
}

func TestColdReservationRollsBackWholeMetadataTransactionOnFailure(t *testing.T) {
	for _, failure := range []string{"running-parent", "missing-parent", "foreign-lineage", "env-write"} {
		t.Run(failure, func(t *testing.T) {
			st := testStore(t)
			ctx := context.Background()
			sb := models.Sandbox{ID: "original", Status: models.StatusStopped}
			sb.SetEnv(map[string]string{"old": "retained"})
			if failure == "running-parent" {
				sb.Status = models.StatusRunning
			}
			if failure != "missing-parent" {
				if err := st.Create(ctx, &sb); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "foreign-lineage" {
				if err := st.db.Create(&models.EnvironmentReleaseFence{Resource: sb.ID, Lineage: "foreign"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if failure == "env-write" {
				if err := st.db.Callback().Update().Before("gorm:update").Register("reject-cold-env", func(tx *gorm.DB) {
					if tx.Statement.Table == "sandboxes" {
						_ = tx.AddError(errors.New("simulated metadata failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			in := models.ColdStart{Resource: sb.ID, RequestID: "review-original", State: "dispatching"}
			if _, _, err := st.ReserveColdStart(ctx, in, map[string]string{"GRASP_EXECUTION_LINEAGE": "current", "new": "unauthorized"}); err == nil {
				t.Fatal("failing metadata transaction was accepted")
			}
			if _, err := st.ColdStart(ctx, sb.ID, in.RequestID); !errors.Is(err, ErrNotFound) {
				t.Fatal("failed transaction left a dispatch fence", err)
			}
			if failure != "missing-parent" {
				row, _ := st.Get(ctx, sb.ID)
				if row.Env()["old"] != "retained" || row.Env()["new"] != "" {
					t.Fatal("failed transaction published partial authorization")
				}
			}
		})
	}
}

func TestColdMetadataUnavailableDoesNotBecomeMissingOrAccepted(t *testing.T) {
	st := testStore(t)
	sqlDB, _ := st.db.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := st.ColdStart(ctx, "original", "request-original"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatal("SQL outage became missing receipt", err)
	}
	if err := st.GuardColdStart(ctx, "original"); err == nil {
		t.Fatal("unavailable fence allowed lifecycle")
	}
	if err := st.ConfirmColdStart(ctx, models.ColdStart{Resource: "original"}, "native"); err == nil {
		t.Fatal("unavailable receipt publication succeeded")
	}
	if _, _, err := st.ReserveColdStart(ctx, models.ColdStart{Resource: "original"}, nil); err == nil {
		t.Fatal("unavailable admission succeeded")
	}
}
