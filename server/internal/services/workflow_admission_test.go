package services

import (
	"errors"
	"path/filepath"
	"testing"

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
