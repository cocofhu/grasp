package services

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

func TestDiagnosticRunsUseCurrentAuthorityAndRetainOnRevocation(t *testing.T) {
	db := newTestDB(t)
	controller, authority, _ := serviceExecutionController(t, db)
	arts := NewArtifactService(db)
	arts.SetExecutionController(controller)
	a, b := "doctor-a-"+uuid.NewString(), "doctor-b-"+uuid.NewString()
	if err := arts.DiagnosticRuns(t.Context(), true, a, b); err != nil {
		t.Fatal(err)
	}
	if _, err := arts.Save(a, "doctor", "proof.txt", "text", "original"); err != nil {
		t.Fatal(err)
	}
	if _, ok := arts.Get(b, "proof.txt"); ok {
		t.Fatal("diagnostic artifacts crossed run boundaries")
	}
	if err := arts.DiagnosticRuns(t.Context(), false, a, b); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&models.Run{}).Where("id IN ?", []string{a, b}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("diagnostic cleanup retained runs", count, err)
	}
	a, b = "doctor-a-"+uuid.NewString(), "doctor-b-"+uuid.NewString()
	if err := arts.DiagnosticRuns(t.Context(), true, a, b); err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.BeginRestore(t.Context(), owner, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	if err := arts.DiagnosticRuns(t.Context(), false, a, b); err == nil {
		t.Fatal("revoked process deleted diagnostic metadata")
	}
	if err := arts.DiagnosticRuns(t.Context(), true, "doctor-a-"+uuid.NewString(), "doctor-b-"+uuid.NewString()); err == nil {
		t.Fatal("revoked process created a diagnostic namespace")
	}
	if err := db.Model(&models.Run{}).Where("id IN ?", []string{a, b}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("revocation removed retained evidence", count, err)
	}
}

func TestDiagnosticCleanupRejectsChangedOwnership(t *testing.T) {
	db := newTestDB(t)
	arts := NewArtifactService(db)
	a, b := "doctor-a-"+uuid.NewString(), "doctor-b-"+uuid.NewString()
	if err := arts.DiagnosticRuns(t.Context(), true, a, b); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Run{}).Where("id = ?", a).Update("created_by", "another-owner").Error; err != nil {
		t.Fatal(err)
	}
	if err := arts.DiagnosticRuns(t.Context(), false, a, b); err == nil {
		t.Fatal("cleanup ignored changed ownership")
	}
	for _, ids := range [][]string{{a}, {a, a}, {"a", "b"}, {"doctor-a-invalid", b}} {
		if err := arts.DiagnosticRuns(t.Context(), true, ids...); err == nil {
			t.Fatal("invalid diagnostic identities admitted", ids)
		}
	}
}
