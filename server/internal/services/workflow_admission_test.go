package services

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

type admissionRecorder struct {
	calls int
	input RunStart
}

func (r *admissionRecorder) StartAdmittedRun(input RunStart) (*models.Run, error) {
	r.calls++
	r.input = input
	return &models.Run{ID: "started"}, nil
}

func TestWorkflowFeatureAndAuthenticatedLaunchPolicy(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "admission.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	prior := config.GetConfig()
	config.StoreConfig(&config.Config{})
	t.Cleanup(func() { config.StoreConfig(prior) })
	recorder := &admissionRecorder{}
	admission := NewRunAdmission(db, recorder)
	if err := db.Create(&models.SoloSession{ID: "mine", UserID: "alice"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"web", "api", "pm"} {
		if _, err := admission.Start(RunStart{Source: source, ActorID: "alice", WorkflowID: "workflow"}); err != nil {
			t.Fatal(source, err)
		}
	}
	if recorder.calls != 3 {
		t.Fatal(recorder.calls)
	}
	for _, invalid := range []RunStart{
		{Source: "solo", ActorID: "alice", SoloID: "mine", IdempotencyKey: "click"},
		{Source: "solo", ActorID: "mallory", SoloID: "mine", IdempotencyKey: "click", Confirmed: true},
		{Source: "pm", ActorID: "alice", SoloID: "mine", Confirmed: true},
		{Source: "manual", ActorID: "alice", Confirmed: true},
		{Source: "web"},
	} {
		if _, err := admission.Start(invalid); err == nil {
			t.Fatalf("accepted %+v", invalid)
		}
	}
	if recorder.calls != 3 {
		t.Fatal("rejected request reached engine")
	}
	settings := NewSettingsService(db, nil, nil)
	if err := settings.SetWorkflowFeature(false); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"web", "api", "pm", "solo"} {
		_, err := admission.Start(RunStart{Source: source, ActorID: "alice", SoloID: map[string]string{"solo": "mine"}[source], Confirmed: true, IdempotencyKey: "click"})
		var denied *AdmissionError
		if !errors.As(err, &denied) || denied.Code != "workflow_disabled" {
			t.Fatalf("%s: %v", source, err)
		}
	}
	t.Setenv("GRASP_WORKFLOWS_ENABLED", "true")
	feature, err := settings.WorkflowFeature()
	if err != nil || !feature.Enabled || !feature.Locked {
		t.Fatalf("%+v %v", feature, err)
	}
	if err := settings.SetWorkflowFeature(false); err == nil {
		t.Fatal("environment lock bypassed")
	}
	t.Setenv("GRASP_WORKFLOWS_ENABLED", "invalid")
	if _, err := admission.Start(RunStart{Source: "api", ActorID: "key"}); err == nil {
		t.Fatal("invalid feature setting must fail closed")
	}
}

func TestWorkflowLaunchIdentityIgnoresResolvedReferenceMetadata(t *testing.T) {
	input := RunStart{WorkflowID: "workflow", Source: "web", ActorID: "alice", IdempotencyKey: "click",
		FirstMessageInput: &RunFirstMessage{Text: "review this", References: []ReferenceInput{{Kind: " github_issue ", Target: " acme/repo#42 "}}},
		FirstMessage: &models.CompositeText{Text: "review this", ReferenceUser: "alice", References: []models.MessageReference{{
			ID: "random-a", Kind: models.ReferenceKindGitHubIssue, Target: "acme/repo#42", Title: "Before", State: "open", CreatedAt: time.Now(),
		}}},
	}
	key, original, err := input.Identity()
	if err != nil {
		t.Fatal(err)
	}
	input.FirstMessage.References[0] = models.MessageReference{ID: "random-b", Kind: models.ReferenceKindGitHubPR,
		Target: "acme/repo#42", Title: "After", State: "closed", Access: models.ReferenceAccessDenied, CreatedAt: time.Now().Add(time.Hour)}
	input.FirstMessageInput.References[0] = ReferenceInput{Kind: "github_issue", Target: "acme/repo#42"}
	input.PrepareFirstMessage = func() (*models.CompositeText, error) { return nil, errors.New("must not be hashed") }
	retryKey, retry, err := input.Identity()
	if err != nil || retryKey != key || retry != original {
		t.Fatalf("resolved metadata changed identity: key=%s fingerprint=%s err=%v", retryKey, retry, err)
	}
	input.FirstMessageInput.Text = "changed instruction"
	_, changed, err := input.Identity()
	if err != nil || changed == original {
		t.Fatalf("changed original message retained fingerprint: %s %v", changed, err)
	}
}

func TestWorkflowDisabledStillAcknowledgesOwnedLaunchReceipts(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "disabled-receipt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	previous := config.GetConfig()
	config.StoreConfig(&config.Config{})
	t.Cleanup(func() { config.StoreConfig(previous) })
	if err := db.Create(&models.SoloSession{ID: "mine", UserID: "alice"}).Error; err != nil {
		t.Fatal(err)
	}
	inputs := make([]RunStart, 0, 4)
	for _, source := range []string{"web", "api", "pm", "solo"} {
		input := RunStart{WorkflowID: "workflow", Source: source, ActorID: "alice", IdempotencyKey: "same-request", Title: "Original instruction"}
		if source == "solo" {
			input.SoloID, input.Confirmed = "mine", true
		}
		key, fingerprint, err := input.Identity()
		if err != nil {
			t.Fatal(err)
		}
		run := models.Run{ID: "run-" + source, WorkflowID: input.WorkflowID, Status: "completed"}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.WorkflowLaunch{Key: key, Fingerprint: fingerprint, RunID: run.ID, ActorID: input.ActorID, Source: input.Source, SoloID: input.SoloID}).Error; err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, input)
	}
	if err := NewSettingsService(db, nil, nil).SetWorkflowFeature(false); err != nil {
		t.Fatal(err)
	}
	recorder := &admissionRecorder{}
	admission := NewRunAdmission(db, recorder)
	for _, input := range inputs {
		t.Run(input.Source, func(t *testing.T) {
			run, err := admission.Start(input)
			if err != nil || run.ID != "run-"+input.Source {
				t.Fatalf("accepted receipt could not be confirmed: %+v %v", run, err)
			}
			changed := input
			changed.Title = "Changed instruction"
			_, err = admission.Start(changed)
			var conflict *AdmissionError
			if !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" || conflict.Status != http.StatusConflict {
				t.Fatalf("different intent bypassed receipt check: %v", err)
			}
			fresh := input
			fresh.IdempotencyKey = "new-request"
			_, err = admission.Start(fresh)
			var disabled *AdmissionError
			if !errors.As(err, &disabled) || disabled.Code != "workflow_disabled" || disabled.Status != http.StatusForbidden {
				t.Fatalf("disabled feature allowed new work: %v", err)
			}
		})
	}
	foreign := inputs[len(inputs)-1]
	foreign.ActorID = "mallory"
	_, err = admission.Start(foreign)
	var notFound *AdmissionError
	if !errors.As(err, &notFound) || notFound.Status != http.StatusNotFound {
		t.Fatalf("receipt lookup bypassed Solo ownership: %v", err)
	}
	if recorder.calls != 0 {
		t.Fatalf("confirming old receipts started new work: %d", recorder.calls)
	}
}
