package engine

import (
	"errors"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestWorkflowLaunchConcurrentRetryAndRestart(t *testing.T) {
	e, db := setupEngine(t)
	input := services.RunStart{WorkflowID: "clarify-to-design", Source: "web", ActorID: "alice", IdempotencyKey: "same-click", Inputs: map[string]any{"idea": "make it"}, Trigger: models.TriggerManual}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			run, err := services.NewRunAdmission(db, e).Start(input)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- run.ID
		})
	}
	wg.Wait()
	close(ids)
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("duplicate run: %s != %s", id, first)
		}
	}
	var launches, runs int64
	db.Model(&models.WorkflowLaunch{}).Count(&launches)
	db.Model(&models.Run{}).Count(&runs)
	if launches != 1 || runs != 1 {
		t.Fatalf("launches=%d runs=%d", launches, runs)
	}
	// Retry remains stable after the workflow head changes or is removed, and
	// through a newly constructed admission service (no process-local cache).
	if err := db.Delete(&models.WorkflowDef{}, "id = ?", input.WorkflowID).Error; err != nil {
		t.Fatal(err)
	}
	run, err := services.NewRunAdmission(db, e).Start(input)
	if err != nil || run.ID != first {
		t.Fatalf("%+v %v", run, err)
	}
	input.Title = "different"
	_, err = services.NewRunAdmission(db, e).Start(input)
	var conflict *services.AdmissionError
	if !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" {
		t.Fatalf("%v", err)
	}
}

func TestWorkflowDisabledCoversLegacyEngineWrappers(t *testing.T) {
	e, db := setupEngine(t)
	if err := services.NewSettingsService(db, nil, nil).SetWorkflowFeature(false); err != nil {
		t.Fatal(err)
	}
	_, err := e.StartRun("clarify-to-design", map[string]any{"idea": "blocked"}, models.TriggerManual)
	var denied *services.AdmissionError
	if !errors.As(err, &denied) || denied.Code != "workflow_disabled" {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.Run{}).Count(&count)
	if count != 0 {
		t.Fatal("run created while disabled")
	}
}

func TestWorkflowLaunchRejectsAChangedReviewedVersion(t *testing.T) {
	e, db := setupEngine(t)
	_, err := services.NewRunAdmission(db, e).Start(services.RunStart{WorkflowID: "clarify-to-design", Source: "web", ActorID: "alice", Published: true, ExpectedVersion: 2, Inputs: map[string]any{"idea": "version review"}})
	var changed *services.AdmissionError
	if !errors.As(err, &changed) || changed.Code != "workflow_version_changed" {
		t.Fatalf("%v", err)
	}
	var count int64
	db.Model(&models.Run{}).Count(&count)
	if count != 0 {
		t.Fatal("executed a version different from the launch card")
	}
}
