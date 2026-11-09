package executionlineage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type soloReviewNative struct {
	observation         SoloReviewObservation
	stops, observations int
	stop                func() error
	observe             func() error
}

func (p *soloReviewNative) Observe(context.Context, string) (SoloReviewObservation, error) {
	p.observations++
	if p.observe != nil {
		if err := p.observe(); err != nil {
			return SoloReviewObservation{}, err
		}
	}
	return p.observation, nil
}
func (p *soloReviewNative) Stop(context.Context, string) error {
	p.stops++
	if p.stop != nil {
		return p.stop()
	}
	p.observation.Status = "stopped"
	return nil
}

func soloReviewFixture(t *testing.T) (*gorm.DB, *Authority, string, models.Sandbox, models.Sandbox, *soloReviewNative) {
	t.Helper()
	db, a, calls, owner := authorityFixture(t)
	rows := []models.Sandbox{{Name: "original-provider-one", Profile: "profile", Purpose: "solo", ThreadID: "original-solo-one", Status: "running"}, {Name: "original-provider-two", Profile: "profile", Purpose: "solo", ThreadID: "original-solo-two", Status: "running"}}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.SoloSession{ID: rows[i].ThreadID, UserID: "original-owner", AgentName: "profile", SandboxID: rows[i].ID, Status: models.SoloStatusRunning}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Table("chat_queue_turns").Create(map[string]any{"id": "unrelated-original-unknown", "queue_id": "other-queue", "state": "unknown", "payload": "do-not-replay"}).Error; err != nil {
		t.Fatal(err)
	}
	ticket, err := a.Initialize(t.Context(), uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	commitQuarantine(t, db, a, calls, owner, ticket)
	if _, err := a.Activate(t.Context(), db, owner, ticket); err != nil {
		t.Fatal(err)
	}
	p := &soloReviewNative{observation: SoloReviewObservation{Fingerprint: strings.Repeat("c", 64), Status: "running"}}
	return db, a, owner, rows[0], rows[1], p
}

func reviewGrant(context.Context, uint) (string, error) { return strings.Repeat("a", 64), nil }

func TestOfflineSoloReviewStopsOnlyOriginalAndRetainsOtherQuarantines(t *testing.T) {
	db, a, owner, original, other, p := soloReviewFixture(t)
	var binding models.ExecutionDataset
	if err := db.First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	p.observe = func() error {
		// A separate writer can acquire metadata while observing the native
		// provider: no object/provider I/O occurs under the SQL transaction.
		return db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&storageguard.Control{}).Where("name = 'primary'").UpdateColumn("revision", gorm.Expr("revision + 1")).Error
		})
	}
	row, err := a.ReviewSoloEnvironment(t.Context(), db, owner, original.ID, p, reviewGrant)
	if err != nil {
		t.Fatal(err)
	}
	if row.CompletedAt == nil || row.FinalizedAt != nil || p.stops != 1 || row.ProviderID != original.Name {
		t.Fatal("review did not preserve the original stop receipt", row, p.stops)
	}
	if err := storageguard.Check(t.Context(), db, owner); err != nil {
		t.Fatal("completion silently cleared maintenance", err)
	}
	var sb models.Sandbox
	if err := db.First(&sb, original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if sb.Name != original.Name || sb.Status != "stopped" || sb.DesiredState != "stopped" || sb.Generation != original.Generation+1 || sb.AuthorizationFingerprint != "" || sb.DestroyAt != nil {
		t.Fatal("stop-only review invented authorization or replaced files", sb)
	}
	var solo models.SoloSession
	if err := db.First(&solo, "id = ?", original.ThreadID).Error; err != nil || solo.SandboxID != original.ID || solo.Status != models.SoloStatusStopped || !solo.ResumeContextPending {
		t.Fatal("original Solo was not retained stopped", solo, err)
	}
	var untouched models.Sandbox
	if err := db.First(&untouched, other.ID).Error; err != nil || untouched.Status != "running" {
		t.Fatal("another original environment changed", err)
	}
	var reviews []models.ExecutionQuarantine
	if err := db.Find(&reviews).Error; err != nil {
		t.Fatal(err)
	}
	pending, reviewed := 0, 0
	for _, q := range reviews {
		if q.ReviewState == "pending" {
			pending++
		}
		if q.ReviewState == "reviewed" {
			reviewed++
		}
	}
	if pending != 2 || reviewed != 1 {
		t.Fatal("review broadened beyond one sandbox", pending, reviewed)
	}
	count, digest, err := quarantineFingerprint(db, binding.DatasetID)
	if err != nil || count != binding.QuarantineCount || digest != binding.QuarantineSHA256 {
		t.Fatal("review erased the original frozen set", err)
	}
	if _, err := a.ReviewSoloEnvironment(t.Context(), db, owner, original.ID, p, reviewGrant); err != nil || p.stops != 1 {
		t.Fatal("same receipt repeated a native stop", err, p.stops)
	}
	row, err = a.FinalizeSoloEnvironmentReview(t.Context(), db, owner, original.ID, p, reviewGrant)
	if err != nil || row.FinalizedAt == nil || p.stops != 1 {
		t.Fatal("explicit original finalize failed", err)
	}
	var control storageguard.Control
	if err := db.First(&control, "name = 'primary'").Error; err != nil || control.Owner != "" {
		t.Fatal("original maintenance not released", err)
	}
	controller, err := NewController(t.Context(), db, a)
	if err != nil {
		t.Fatal(err)
	}
	if err := controller.Check(t.Context(), Entity{Kind: EntitySandbox, ID: strconvID(original.ID)}); err != nil {
		t.Fatal("reviewed sandbox still permanently isolated", err)
	}
	if err := controller.Check(t.Context(), Entity{Kind: EntitySandbox, ID: strconvID(other.ID)}); !errors.Is(err, ErrQuarantined) {
		t.Fatal("another original sandbox was unblocked", err)
	}
	if err := controller.Check(t.Context(), Entity{Kind: EntityChatTurn, ID: "unrelated-original-unknown"}); !errors.Is(err, ErrQuarantined) {
		t.Fatal("old unknown execution could replay", err)
	}
}

func strconvID(id uint) string { return fmt.Sprint(id) }

func TestOfflineSoloReviewLostStopResponseOnlyObservesOriginalReceipt(t *testing.T) {
	db, a, owner, original, _, p := soloReviewFixture(t)
	p.stop = func() error { p.observation.Status = "stopped"; return errors.New("lost stop response") }
	if _, err := a.ReviewSoloEnvironment(t.Context(), db, owner, original.ID, p, reviewGrant); err == nil {
		t.Fatal("unknown result incorrectly completed")
	}
	var q models.ExecutionQuarantine
	if err := db.Where("entity_kind = ? AND entity_id = ?", EntitySandbox, strconvID(original.ID)).First(&q).Error; err != nil || q.ReviewState != "pending" {
		t.Fatal("unknown result cleared quarantine", err)
	}
	row, err := a.ReviewSoloEnvironment(t.Context(), db, owner, original.ID, p, reviewGrant)
	if err != nil || row.CompletedAt == nil || p.stops != 1 {
		t.Fatal("resume replayed Stop instead of checking the receipt", err, p.stops)
	}
}

func TestOfflineSoloReviewChangedScopeGrantOrAuthorityCannotComplete(t *testing.T) {
	for _, scenario := range []string{"source", "provider", "grant", "authority", "maintenance", "still_running"} {
		t.Run(scenario, func(t *testing.T) {
			db, a, owner, original, _, p := soloReviewFixture(t)
			grant := reviewGrant
			p.stop = func() error {
				p.observation.Status = "stopped"
				switch scenario {
				case "source":
					return db.Table("solo_sessions").Where("id = ?", original.ThreadID).Update("lifecycle_epoch", 1).Error
				case "provider":
					p.observation.Fingerprint = strings.Repeat("d", 64)
				case "grant":
					grant = func(context.Context, uint) (string, error) { return strings.Repeat("b", 64), nil }
				case "authority":
					_, err := a.BeginRestore(t.Context(), uuid.NewString(), strings.Repeat("f", 64))
					return err
				case "maintenance":
					return db.Model(&storageguard.Control{}).Where("name = 'primary'").Update("owner", uuid.NewString()).Error
				case "still_running":
					p.observation.Status = "running"
				}
				return nil
			}
			authorize := func(ctx context.Context, id uint) (string, error) { return grant(ctx, id) }
			if _, err := a.ReviewSoloEnvironment(t.Context(), db, owner, original.ID, p, authorize); err == nil {
				t.Fatal("changed scope was accepted")
			}
			var sb models.Sandbox
			if err := db.First(&sb, original.ID).Error; err != nil || sb.Status != "running" || sb.Name != original.Name || sb.AuthorizationFingerprint != "" {
				t.Fatal("failed review changed original state", err)
			}
			var q models.ExecutionQuarantine
			if err := db.Where("entity_kind = ? AND entity_id = ?", EntitySandbox, strconvID(original.ID)).First(&q).Error; err != nil || q.ReviewState != "pending" {
				t.Fatal("failed review unquarantined an environment", err)
			}
		})
	}
}

func TestOfflineSoloReviewRefusesPendingParentOrCurrentWorkBeforeProvider(t *testing.T) {
	for _, scenario := range []string{"parent", "legacy_pending", "queued", "lease", "wrong_owner", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			db, a, owner, original, _, p := soloReviewFixture(t)
			authorize := reviewGrant
			switch scenario {
			case "parent":
				if err := db.Create(&models.ExecutionQuarantine{ID: strings.Repeat("d", 64), DatasetID: "ignored", EntityKind: string(EntitySolo), EntityID: original.ThreadID, ReviewState: "pending"}).Error; err != nil {
					t.Fatal(err)
				}
			case "legacy_pending":
				if err := db.Table("chat_pending_turns").Create(map[string]any{"id": "old-request", "surface": "solo", "session_key": original.ThreadID}).Error; err != nil {
					t.Fatal(err)
				}
			case "queued":
				hash := sha256.Sum256([]byte("solo\x00" + original.ThreadID))
				if err := db.Table("chat_queue_turns").Create(map[string]any{"id": "new-explicit-request", "queue_id": hex.EncodeToString(hash[:]), "state": "queued"}).Error; err != nil {
					t.Fatal(err)
				}
			case "lease":
				if err := db.Table("sandboxes").Where("id = ?", original.ID).Update("operation_owner", uuid.NewString()).Error; err != nil {
					t.Fatal(err)
				}
			case "wrong_owner":
				owner = uuid.NewString()
			case "revoked":
				authorize = func(context.Context, uint) (string, error) { return "", errors.New("grant revoked") }
			}
			if _, err := a.ReviewSoloEnvironment(t.Context(), db, owner, original.ID, p, authorize); err == nil {
				t.Fatal("pending work or revoked grant accepted")
			}
			if p.stops != 0 || p.observations != 0 {
				t.Fatal("rejected scope touched the native provider", p.stops, p.observations)
			}
		})
	}
}

func TestOfflineSoloReviewCanceledOrFailedPublicationRetainsOriginalReceipt(t *testing.T) {
	for _, kind := range []string{"cancel", "sql_failure"} {
		t.Run(kind, func(t *testing.T) {
			db, a, owner, original, _, p := soloReviewFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p.stop = func() error {
				p.observation.Status = "stopped"
				if kind == "cancel" {
					cancel()
					return context.Canceled
				}
				return db.Exec("CREATE TRIGGER fail_review_publication BEFORE UPDATE ON sandboxes BEGIN SELECT RAISE(ABORT,'injected publication failure'); END").Error
			}
			if _, err := a.ReviewSoloEnvironment(ctx, db, owner, original.ID, p, reviewGrant); err == nil {
				t.Fatal("failed publication reported completion")
			}
			var q models.ExecutionQuarantine
			if err := db.Where("entity_kind = ? AND entity_id = ?", EntitySandbox, strconvID(original.ID)).First(&q).Error; err != nil || q.ReviewState != "pending" {
				t.Fatal("failed publication partially cleared quarantine", err)
			}
			var row SoloEnvironmentReview
			if err := db.First(&row, "id = ?", owner).Error; err != nil || row.DispatchedAt == nil || row.CompletedAt != nil {
				t.Fatal("failed publication lost original dispatch receipt", err)
			}
			if err := storageguard.Check(t.Context(), db, owner); err != nil {
				t.Fatal("failure released maintenance", err)
			}
			if kind == "sql_failure" {
				if err := db.Exec("DROP TRIGGER fail_review_publication").Error; err != nil {
					t.Fatal(err)
				}
			}
			row, err := a.ReviewSoloEnvironment(t.Context(), db, owner, original.ID, p, reviewGrant)
			if err != nil || row.CompletedAt == nil || p.stops != 1 {
				t.Fatal("recovery repeated native stop or lost same receipt", err, p.stops)
			}
		})
	}
}
