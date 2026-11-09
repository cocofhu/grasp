package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestWorkspaceImportRejectedConfirmationProofKeepsOriginalNonce(t *testing.T) {
	s, runtime, input := workspaceImportFixture(t)
	ctx := context.Background()
	preview, err := s.Preview(ctx, "solo-import", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	request := WorkspaceImportConfirmInput{Confirmed: true, ClientRequestID: "same-confirmation-nonce"}
	if err := s.db.Model(&models.Sandbox{}).Where("purpose = ?", SandboxPurposeSolo).Update("desired_state", "stopped").Error; err != nil {
		t.Fatal(err)
	}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, request)
	importCode(t, err, "workspace_target_unavailable")
	var rejected *SoloError
	if !errors.As(err, &rejected) || rejected.Params["confirmationAccepted"] != false || rejected.Params["importId"] != preview.ID || rejected.Params["clientRequestId"] != request.ClientRequestID || runtime.imports != 0 {
		t.Fatalf("pre-admission rejection lacks the exact receipt proof: %#v, imports=%d", rejected, runtime.imports)
	}
	if err := s.db.Model(&models.Sandbox{}).Where("purpose = ?", SandboxPurposeSolo).Update("desired_state", "running").Error; err != nil {
		t.Fatal(err)
	}
	result, err := s.Confirm(ctx, "solo-import", "alice", preview.ID, request)
	if err != nil || result.State != "applied" || runtime.imports != 1 {
		t.Fatalf("same original request could not proceed after rejection: %+v, %v", result, err)
	}
}

func TestWorkspaceImportPostAdmissionTargetErrorHasNoRejectionProof(t *testing.T) {
	s, runtime, input := workspaceImportFixture(t)
	ctx := context.Background()
	preview, err := s.Preview(ctx, "solo-import", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	runtime.importHook = func() {
		if err := s.db.Model(&models.Sandbox{}).Where("purpose = ?", SandboxPurposeSolo).Update("desired_state", "stopped").Error; err != nil {
			t.Fatal(err)
		}
	}
	request := WorkspaceImportConfirmInput{Confirmed: true, ClientRequestID: "accepted-before-target-stop"}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, request)
	importCode(t, err, "workspace_target_unavailable")
	var result *SoloError
	if !errors.As(err, &result) || result.Params["confirmationAccepted"] != nil || runtime.imports != 1 {
		t.Fatalf("an accepted filesystem operation was reported unaccepted: %#v", result)
	}
	var row models.WorkspaceImport
	if err := s.db.First(&row, "id = ?", preview.ID).Error; err != nil || row.ConfirmNonce != request.ClientRequestID || row.State != "unknown" {
		t.Fatalf("accepted receipt lost: %+v, %v", row, err)
	}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, request)
	importCode(t, err, "workspace_import_reconcile_required")
	if runtime.imports != 1 {
		t.Fatal("unknown import replayed")
	}
}

func TestWorkspaceImportStalePreparedRowAndLostLeaseCannotMintRejectionProof(t *testing.T) {
	for _, changed := range []string{"accepted", "lease_lost"} {
		t.Run(changed, func(t *testing.T) {
			s, _, input := workspaceImportFixture(t)
			ctx := context.Background()
			preview, err := s.Preview(ctx, "solo-import", "alice", input)
			if err != nil {
				t.Fatal(err)
			}
			stale, err := s.load(ctx, "solo-import", "alice", preview.ID)
			if err != nil {
				t.Fatal(err)
			}
			held, release, err := s.sandbox.acquireSandboxOperation(ctx, stale.TargetSandboxID, "code_import")
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			nonce := "same-competing-confirmation"
			if changed == "accepted" {
				err = s.db.Model(&models.WorkspaceImport{}).Where("id = ?", stale.ID).Updates(map[string]any{"state": "applying", "confirm_nonce": nonce}).Error
			} else {
				err = s.db.Model(&models.Sandbox{}).Where("id = ?", stale.TargetSandboxID).Updates(map[string]any{"operation_owner": "new-owner", "generation": stale.SourceGeneration + 100, "operation_expires_at": time.Now().Add(time.Minute)}).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			original := workspaceImportError(409, "workspace_target_unavailable", "not proof of admission")
			result := s.rejectedBeforeAdmission(held, stale, nonce, original)
			var typed *SoloError
			if result != original || !errors.As(result, &typed) || typed.Params["confirmationAccepted"] != nil {
				t.Fatalf("stale observation minted global non-admission proof: %#v", typed)
			}
		})
	}
}

func TestWorkspaceImportSQLAdmissionFailureDoesNotMintRejectionProof(t *testing.T) {
	s, runtime, input := workspaceImportFixture(t)
	ctx := context.Background()
	preview, err := s.Preview(ctx, "solo-import", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec(`CREATE TRIGGER import_admission_failure BEFORE UPDATE ON workspace_imports WHEN NEW.state = 'applying' BEGIN SELECT RAISE(ABORT, 'metadata publication unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, WorkspaceImportConfirmInput{Confirmed: true, ClientRequestID: "opaque-sql-failure"})
	var typed *SoloError
	if err == nil || (errors.As(err, &typed) && typed.Params["confirmationAccepted"] != nil) || runtime.imports != 0 {
		t.Fatalf("ambiguous SQL failure minted an acceptance claim: %v", err)
	}
}

func TestWorkspaceImportReadRetiresExpiredPreviewWithoutDeletingSharedObject(t *testing.T) {
	s, runtime, input := workspaceImportFixture(t)
	ctx := context.Background()
	before := time.Now().UTC()
	preview, err := s.Preview(ctx, "solo-import", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExpiresAt == nil || preview.ExpiresAt.Before(before.Add(workspaceImportPreviewTTL)) || preview.ExpiresAt.After(time.Now().Add(workspaceImportPreviewTTL)) {
		t.Fatalf("new preview lacks its fixed lifetime: %+v", preview)
	}
	var original models.WorkspaceImport
	if err := s.db.First(&original, "id = ?", preview.ID).Error; err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	if err := s.db.Model(&models.WorkspaceImport{}).Where("id = ?", original.ID).Update("expires_at", past).Error; err != nil {
		t.Fatal(err)
	}
	shared := original
	shared.ID, shared.State, shared.ConfirmNonce, shared.ExpiresAt = "accepted-shared-archive", "unknown", "accepted-nonce", &past
	if err := s.db.Create(&shared).Error; err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(ctx, "solo-import", "alice", original.ID)
	if err != nil || view.State != "expired" {
		t.Fatalf("abandoned preview still confirmable: %+v, %v", view, err)
	}
	var retired models.WorkspaceImport
	if err := s.db.First(&retired, "id = ?", original.ID).Error; err != nil || retired.ArchiveRef != "" || retired.ArchiveSHA256 != original.ArchiveSHA256 || retired.ConfirmNonce != "" {
		t.Fatalf("retirement lost evidence or kept the unused root: %+v, %v", retired, err)
	}
	if err := s.db.First(&shared, "id = ?", shared.ID).Error; err != nil || shared.State != "unknown" || shared.ArchiveRef != original.ArchiveRef {
		t.Fatalf("retirement touched accepted/shared content: %+v, %v", shared, err)
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		t.Fatal(err)
	}
	body, err := files.Open(ctx, original.ArchiveRef)
	if err != nil {
		t.Fatal("logical retirement deleted a shared object", err)
	}
	_, readErr := io.Copy(io.Discard, body)
	if err := body.Close(); err != nil || readErr != nil {
		t.Fatalf("shared archive unreadable: %v, %v", readErr, err)
	}
	_, err = s.Confirm(ctx, "solo-import", "alice", original.ID, WorkspaceImportConfirmInput{Confirmed: true, ClientRequestID: "expired-confirmation"})
	importCode(t, err, "workspace_import_expired")
	if runtime.imports != 0 {
		t.Fatal("expired preview dispatched")
	}
}

func TestWorkspaceImportExpiryScanIsBoundedScopedAndNeverGuessesLegacyAge(t *testing.T) {
	s, runtime, input := workspaceImportFixture(t)
	ctx := context.Background()
	preview, err := s.Preview(ctx, "solo-import", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	var source models.WorkspaceImport
	if err := s.db.First(&source, "id = ?", preview.ID).Error; err != nil {
		t.Fatal(err)
	}
	past, zero := time.Now().Add(-time.Hour), time.Time{}
	for i := 0; i < workspaceImportExpiryBatch+1; i++ {
		row := source
		row.ID, row.ExpiresAt = fmt.Sprintf("expired-%03d", i), &past
		if err := s.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"legacy", "zero", "other-actor", "other-solo", "applying", "unknown", "applied", "nonce"} {
		row := source
		row.ID, row.ExpiresAt = kind, &past
		switch kind {
		case "legacy":
			row.CreatedAt, row.ExpiresAt = past.Add(-365*24*time.Hour), nil
		case "zero":
			row.ExpiresAt = &zero
		case "other-actor":
			row.ActorID = "someone-else"
		case "other-solo":
			row.SoloID = "different-solo"
		case "applying", "unknown", "applied":
			row.State = kind
		case "nonce":
			row.ConfirmNonce = "never-retire-an-accepted-request"
		}
		if err := s.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.List(ctx, "solo-import", "alice", input.RunID); err != nil {
		t.Fatal(err)
	}
	var retired int64
	if err := s.db.Model(&models.WorkspaceImport{}).Where("state = ?", "expired").Count(&retired).Error; err != nil || retired != workspaceImportExpiryBatch {
		t.Fatalf("unbounded retirement: count=%d, %v", retired, err)
	}
	for _, id := range []string{"legacy", "zero", "other-actor", "other-solo", "applying", "unknown", "applied", "nonce"} {
		var row models.WorkspaceImport
		if err := s.db.First(&row, "id = ?", id).Error; err != nil || row.ArchiveRef == "" || row.State == "expired" {
			t.Fatalf("unsafe retirement of %s: %+v, %v", id, row, err)
		}
	}
	if runtime.imports != 0 || runtime.checks != 0 || runtime.exports != 1 {
		t.Fatal("metadata retirement invoked provider operations")
	}
}

type expiryDuringInventory struct {
	*workspaceImportRuntime
	expire func()
}

func (r expiryDuringInventory) Inventory(ctx context.Context, id uint) (sandbox.WorkspaceCodeInventory, error) {
	value, err := r.workspaceImportRuntime.Inventory(ctx, id)
	r.expire()
	return value, err
}

func TestWorkspaceImportExpiresDuringPreparationCannotWinAdmissionCAS(t *testing.T) {
	s, runtime, input := workspaceImportFixture(t)
	ctx := context.Background()
	preview, err := s.Preview(ctx, "solo-import", "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	s.runtime = expiryDuringInventory{workspaceImportRuntime: runtime, expire: func() {
		if err := s.db.Model(&models.WorkspaceImport{}).Where("id = ?", preview.ID).Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
	}}
	_, err = s.Confirm(ctx, "solo-import", "alice", preview.ID, WorkspaceImportConfirmInput{Confirmed: true, ClientRequestID: "expiry-during-prepare"})
	if err == nil || runtime.imports != 0 {
		t.Fatal("stale preparation bypassed expiry CAS", err)
	}
	current, err := s.Get(ctx, "solo-import", "alice", preview.ID)
	if err != nil || current.State != "expired" {
		t.Fatalf("expired preparation was not retired: %+v, %v", current, err)
	}
}
