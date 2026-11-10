package services

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
)

type serviceReviewStop struct{ service *SandboxService }

func (p serviceReviewStop) Observe(ctx context.Context, id string) (executionlineage.SoloReviewObservation, error) {
	status, err := p.service.mgr.ProbeStatus(ctx, id)
	if status == "exited" {
		status = "stopped"
	}
	return executionlineage.SoloReviewObservation{Fingerprint: strings.Repeat("b", 64), Status: status}, err
}
func (p serviceReviewStop) Stop(ctx context.Context, id string) error {
	return p.service.mgr.Gateway().Stop(ctx, id)
}

type serviceReviewCold struct {
	service      *SandboxService
	request, id  string
	calls        int
	before       func(sandbox.Spec) error
	unsupported  bool
	preflightErr error
}

func (c *serviceReviewCold) CanResumeConfigured(_ context.Context, id string) (bool, error) {
	if id != c.id {
		return false, errors.New("capability check changed original environment")
	}
	return !c.unsupported, c.preflightErr
}

func (c *serviceReviewCold) ResumeConfigured(ctx context.Context, id string, spec sandbox.Spec, requestID string) (*sandbox.Sandbox, error) {
	c.calls++
	if id != c.id || requestID != c.request {
		return nil, errors.New("cold resume changed original operation")
	}
	if spec.Env["GIT_REPOS"] != "" || spec.WorkspaceDir != "" {
		return nil, errors.New("cold migration tried to clone or relocate repositories")
	}
	if c.before != nil {
		if err := c.before(spec); err != nil {
			return nil, err
		}
	}
	return c.service.mgr.Resume(ctx, id)
}

func reviewedSoloFixture(t *testing.T) (*SandboxService, *models.Sandbox, executionlineage.SoloEnvironmentReview, *serviceReviewCold, string) {
	t.Helper()
	s, row, _, _, _ := recoveryAuthorizationFixture(t)
	if err := s.db.Table("sandboxes").Where("id = ?", row.ID).Updates(map[string]any{"status": "running", "authorization_fingerprint": ""}).Error; err != nil {
		t.Fatal(err)
	}
	row.Status, row.AuthorizationFingerprint = "running", ""
	if err := s.db.Table("solo_sessions").Where("id = ?", row.ThreadID).Update("status", models.SoloStatusRunning).Error; err != nil {
		t.Fatal(err)
	}
	controller, authority, owner := serviceExecutionController(t, s.db)
	s.execution = controller
	if err := storageguard.Enter(t.Context(), s.db, owner, nil); err != nil {
		t.Fatal(err)
	}
	provider := serviceReviewStop{s}
	if _, err := authority.ReviewSoloEnvironment(t.Context(), s.db, owner, row.ID, provider, s.OfflineSoloEnvironmentAuthorization); err != nil {
		t.Fatal(err)
	}
	receipt, err := authority.FinalizeSoloEnvironmentReview(t.Context(), s.db, owner, row.ID, provider, s.OfflineSoloEnvironmentAuthorization)
	if err != nil {
		t.Fatal(err)
	}
	// This is the same accepted user Resume intent that ResumeEnvironment
	// writes before EnsureSoloRunning. No startup scan writes this intent.
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("sandboxes").Where("id = ?", row.ID).Update("desired_state", "running").Error; err != nil {
			return err
		}
		return tx.Table("solo_sessions").Where("id = ?", row.ThreadID).Update("status", models.SoloStatusStarting).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "dirty-nongit-binary.dat")
	if err := os.WriteFile(marker, []byte{0, 1, 255, 0, 5}, 0o600); err != nil {
		t.Fatal(err)
	}
	cold := &serviceReviewCold{service: s, id: row.Name, request: receipt.ID + "-cold"}
	return s, &row, receipt, cold, marker
}

func runReviewedCold(t *testing.T, s *SandboxService, row *models.Sandbox, receipt executionlineage.SoloEnvironmentReview, cold soloReviewedColdResumer) error {
	t.Helper()
	ctx, release, err := s.acquireSandboxOperation(t.Context(), row.ID)
	if err != nil {
		return err
	}
	defer release()
	current, err := s.Get(row.ID)
	if err != nil {
		return err
	}
	return s.resumeReviewedSoloWith(ctx, current, receipt, cold)
}

func TestReviewedSoloColdResumePublishesCurrentGrantOnlyAfterNativeProof(t *testing.T) {
	s, row, receipt, cold, marker := reviewedSoloFixture(t)
	cold.before = func(spec sandbox.Spec) error {
		var before models.Sandbox
		if err := s.db.First(&before, row.ID).Error; err != nil {
			return err
		}
		if before.AuthorizationFingerprint != "" || before.Status == "running" {
			return errors.New("old environment was granted authority before cold configuration")
		}
		if spec.ConfigHome == "" || spec.Env["AGENT_PROVIDER"] == "" {
			return errors.New("cold resume lacks actual trusted configuration")
		}
		return nil
	}
	if err := runReviewedCold(t, s, row, receipt, cold); err != nil {
		t.Fatal(err)
	}
	var after models.Sandbox
	if err := s.db.First(&after, row.ID).Error; err != nil || after.Name != row.Name || after.Status != "running" || len(after.AuthorizationFingerprint) != 64 {
		t.Fatal("current grant not published on exact original resource", err)
	}
	if after.Token != row.Token || after.RunID != row.RunID || after.DestroyAt != nil {
		t.Fatal("cold migration replaced original MCP or file identity")
	}
	if data, err := os.ReadFile(marker); err != nil || !bytes.Equal(data, []byte{0, 1, 255, 0, 5}) {
		t.Fatal("non-Git dirty binary lost", err)
	}
	var saved executionlineage.SoloEnvironmentReview
	if err := s.db.First(&saved, "id = ?", receipt.ID).Error; err != nil || saved.ColdCompletedAt == nil || saved.ColdState != "completed" || saved.ColdRequestID != cold.request {
		t.Fatal("cold receipt not durable", err)
	}
	if _, err := s.soloRecoveryAuthorization(t.Context(), &after); err != nil {
		t.Fatal("original recovery path cannot use rebuilt current grant", err)
	}
	var solo models.SoloSession
	if err := s.db.First(&solo, "id = ?", row.ThreadID).Error; err != nil || solo.SandboxID != row.ID || solo.Status != models.SoloStatusRunning || !solo.ResumeContextPending {
		t.Fatal("original Solo was replaced or lost history handoff", err)
	}
}

func TestReviewedSoloColdResumeFailureAndChangedGrantRetainOriginal(t *testing.T) {
	for _, scenario := range []string{"unknown", "grant", "parent", "physical_owner"} {
		t.Run(scenario, func(t *testing.T) {
			s, row, receipt, cold, marker := reviewedSoloFixture(t)
			cold.before = func(sandbox.Spec) error {
				switch scenario {
				case "unknown":
					return errors.New("native cold configure acknowledgement lost")
				case "grant":
					return s.db.Table("credentials").Where("purpose = ?", "model_api").Update("enabled", false).Error
				case "parent":
					return s.db.Table("solo_sessions").Where("id = ?", row.ThreadID).Update("lifecycle_epoch", 99).Error
				case "physical_owner":
					return s.db.Table("sandboxes").Where("id = ?", row.ID).Updates(map[string]any{"operation_owner": "successor", "generation": gorm.Expr("generation+1")}).Error
				}
				return nil
			}
			if err := runReviewedCold(t, s, row, receipt, cold); err == nil {
				t.Fatal("unknown or changed cold result was published")
			}
			var after models.Sandbox
			if err := s.db.First(&after, row.ID).Error; err != nil || after.Name != row.Name || after.AuthorizationFingerprint != "" {
				t.Fatal("failed cold migration granted or replaced the old environment", err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("failed cold migration removed the only file copy", err)
			}
			var saved executionlineage.SoloEnvironmentReview
			if err := s.db.First(&saved, "id = ?", receipt.ID).Error; err != nil || saved.ColdCompletedAt != nil {
				t.Fatal("failed cold result completed receipt", err)
			}
		})
	}
}

func TestUnreviewedOrNotExplicitlyResumedSoloCannotRebuildGrant(t *testing.T) {
	s, row, receipt, cold, _ := reviewedSoloFixture(t)
	if err := s.db.Table("solo_sessions").Where("id = ?", row.ThreadID).Update("status", models.SoloStatusStopped).Error; err != nil {
		t.Fatal(err)
	}
	if err := runReviewedCold(t, s, row, receipt, cold); err == nil || cold.calls != 0 {
		t.Fatal("observation implicitly resumed the old environment", err, cold.calls)
	}
	if err := s.db.Delete(&executionlineage.SoloEnvironmentReview{}, "id = ?", receipt.ID).Error; err != nil {
		t.Fatal(err)
	}
	handled, err := s.resumeReviewedSoloEnvironment(context.Background(), row)
	if handled || err != nil {
		t.Fatal("missing source-specific receipt established a grant", handled, err)
	}
}

func TestReviewedSoloUnsupportedColdConfigurationNeverDispatches(t *testing.T) {
	for _, provider := range []string{"docker", "e2b"} {
		t.Run(provider, func(t *testing.T) {
			s, row, receipt, cold, marker := reviewedSoloFixture(t)
			cold.unsupported = true
			err := runReviewedCold(t, s, row, receipt, cold)
			var rejection *SoloError
			if !errors.As(err, &rejection) || rejection.Code != "environment_cold_configuration_unsupported" || cold.calls != 0 {
				t.Fatal("unsupported provider was dispatched or reported as unknown", err, cold.calls)
			}
			var saved executionlineage.SoloEnvironmentReview
			if err := s.db.First(&saved, "id = ?", receipt.ID).Error; err != nil || saved.ColdDispatchedAt != nil || saved.ColdRequestID != "" || saved.ColdAuthorizationFingerprint != "" {
				t.Fatal("unsupported preflight persisted a native dispatch", err)
			}
			var original models.Sandbox
			if err := s.db.First(&original, row.ID).Error; err != nil || original.Name != row.Name || original.AuthorizationFingerprint != "" || original.Status != "stopped" || original.DestroyAt != nil {
				t.Fatal("unsupported preflight changed original resource or grant", err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatal("unsupported preflight removed original files", err)
			}
		})
	}
}

func TestReviewedSoloCapabilityReadFailureDoesNotMarkUnknownDispatch(t *testing.T) {
	s, row, receipt, cold, _ := reviewedSoloFixture(t)
	cold.preflightErr = errors.New("capability lookup unavailable")
	if err := runReviewedCold(t, s, row, receipt, cold); !errors.Is(err, cold.preflightErr) || cold.calls != 0 {
		t.Fatal("preflight read failure dispatched a native request", err)
	}
	var saved executionlineage.SoloEnvironmentReview
	if err := s.db.First(&saved, "id = ?", receipt.ID).Error; err != nil || saved.ColdDispatchedAt != nil || saved.ColdState != "" {
		t.Fatal("pre-dispatch read failure became an unknown native result", err)
	}
}
