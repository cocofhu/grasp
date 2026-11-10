package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
)

func instructionFixture(t *testing.T) (*Engine, *instructionQueue) {
	t.Helper()
	g := takeoverPlainGraph(capsPlain)
	e, db := setupEngineGraph(t, g)
	run := models.Run{ID: "instruction-run", WorkflowID: "wf", Status: "running", Graph: g}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	visit := models.StateRun{RunID: run.ID, NodeID: "impl", NodeType: "agent", Iteration: 1, Status: "running"}
	if err := db.Create(&visit).Error; err != nil {
		t.Fatal(err)
	}
	q, err := e.openInstructionQueue(context.Background(), visit.ID, run.ID, "impl", 1)
	if err != nil {
		t.Fatal(err)
	}
	return e, q
}

func instructionInput(q *instructionQueue, text string) InstructionInput {
	return InstructionInput{Iteration: 1, Generation: q.generation, ClientMessageID: uuid.NewString(), Text: text}
}

func TestWorkflowInstructionsOrderedDurableAndIdempotent(t *testing.T) {
	e, q := instructionFixture(t)
	ctx := context.Background()
	input := instructionInput(q, "补充一个边界用例")
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	ids := make(chan string, 12)
	for range 12 {
		wg.Go(func() {
			item, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", input)
			if err != nil {
				errs <- err
				return
			}
			ids <- item.ID
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	close(ids)
	first := ""
	for id := range ids {
		if first != "" && id != first {
			t.Fatal("a client retry created another instruction")
		}
		first = id
	}
	input2 := instructionInput(q, "保留未提交的文件")
	second, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "bob", input2)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{first, second.ID} {
		item, err := q.Next(ctx, false)
		if err != nil || item == nil || item.ID != want {
			t.Fatalf("FIFO target changed: got=%+v want=%s err=%v", item, want, err)
		}
		if err := q.Complete(ctx, item.ID, "applied", ""); err != nil {
			t.Fatal(err)
		}
	}
	if item, err := q.Next(ctx, true); err != nil || item != nil {
		t.Fatalf("queue did not seal empty: %+v %v", item, err)
	}
	if _, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", instructionInput(q, "too late")); err == nil {
		t.Fatal("sealed execution accepted a late instruction")
	}
	item, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", input)
	if err != nil || item.ID != first || item.State != "applied" {
		t.Fatalf("a retry after completion lost its receipt: %+v %v", item, err)
	}
	input.Text = "different intent"
	if _, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", input); err == nil {
		t.Fatal("client identity reused with changed intent")
	}
	box, history, more, err := e.InstructionHistory(ctx, "instruction-run", "impl", q.generation, 0)
	if err != nil || more || len(history) != 2 || box.State != "sealed" || history[0].ActorID != "alice" || history[1].ActorID != "bob" {
		t.Fatalf("durable ordered history lost: %+v more=%v err=%v", history, more, err)
	}
}

func TestWorkflowInstructionSealRacesAdmissionWithoutLosingAcceptedMessages(t *testing.T) {
	e, q := instructionFixture(t)
	ctx := context.Background()
	for n := range 20 {
		if err := q.RestartAttempt(ctx); err != nil {
			t.Fatal(err)
		}
		input := instructionInput(q, fmt.Sprintf("boundary %d", n))
		var wg sync.WaitGroup
		var accepted *models.WorkflowInstruction
		var admissionErr error
		var claimedID string
		var sealErr error
		wg.Go(func() { accepted, admissionErr = e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", input) })
		wg.Go(func() {
			item, err := q.Next(ctx, true)
			sealErr = err
			if item != nil {
				claimedID = item.ID
			}
		})
		wg.Wait()
		if sealErr != nil {
			t.Fatal(sealErr)
		}
		if admissionErr == nil {
			if accepted == nil || claimedID != accepted.ID {
				t.Fatal("an accepted message was lost at the finalization boundary")
			}
			if err := q.Complete(ctx, claimedID, "applied", ""); err != nil {
				t.Fatal(err)
			}
		} else {
			var typed *InstructionError
			if !errors.As(admissionErr, &typed) || typed.Code != "instruction_closed" || claimedID != "" {
				t.Fatalf("unexpected racing refusal: %v, claimed=%s", admissionErr, claimedID)
			}
		}
	}
}

func TestWorkflowInstructionAttemptChangeKeepsUnknownAndExpiresUnsent(t *testing.T) {
	e, q := instructionFixture(t)
	ctx := context.Background()
	old := *q
	first, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", instructionInput(q, "dispatch may have external effects"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Next(ctx, false); err != nil {
		t.Fatal(err)
	}
	unsentInput := instructionInput(q, "not yet sent")
	unsent, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", unsentInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.RestartAttempt(ctx); err != nil {
		t.Fatal(err)
	}
	for id, state := range map[string]string{first.ID: "unknown", unsent.ID: "expired"} {
		var got models.WorkflowInstruction
		if err := e.db.First(&got, "id = ?", id).Error; err != nil || got.State != state {
			t.Fatalf("old instruction %s: state=%s err=%v", id, got.State, err)
		}
	}
	if _, err := old.Next(ctx, true); err == nil {
		t.Fatal("old executor claimed a new-generation message")
	}
	if err := old.close(ctx, "obsolete executor"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", instructionInput(q, "new attempt")); err != nil {
		t.Fatalf("obsolete close sealed the new attempt: %v", err)
	}
	if item, err := q.Next(ctx, false); err != nil || item == nil || item.ID == first.ID || item.ID == unsent.ID {
		t.Fatalf("old work was replayed into the new attempt: %+v %v", item, err)
	}
	if err := old.Complete(ctx, first.ID, "applied", ""); err == nil {
		t.Fatal("old executor acknowledged an unknown dispatch after losing generation")
	}
}

func TestWorkflowInstructionHistoryPagesAcrossAttemptsAndVisits(t *testing.T) {
	e, q := instructionFixture(t)
	ctx := context.Background()
	for n := range 55 {
		if n == 25 {
			if err := q.RestartAttempt(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", instructionInput(q, fmt.Sprintf("requirement %d", n))); err != nil {
			t.Fatal(err)
		}
		item, err := q.Next(ctx, false)
		if err != nil || item == nil {
			t.Fatalf("dispatch %d: %+v %v", n, item, err)
		}
		if err := q.Complete(ctx, item.ID, "applied", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.close(ctx, "visit finished"); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Model(&models.StateRun{}).Where("id = ?", q.mailboxID).Update("status", "completed").Error; err != nil {
		t.Fatal(err)
	}
	visit := models.StateRun{RunID: "instruction-run", NodeID: "impl", NodeType: "agent", Iteration: 2, Status: "running"}
	if err := e.db.Create(&visit).Error; err != nil {
		t.Fatal(err)
	}
	newQueue, err := e.openInstructionQueue(ctx, visit.ID, visit.RunID, visit.NodeID, 2)
	if err != nil {
		t.Fatal(err)
	}
	input := instructionInput(newQueue, "new visit requirement")
	input.Iteration = 2
	last, err := e.EnqueueInstruction(ctx, visit.RunID, visit.NodeID, "alice", input)
	if err != nil || last.Sequence != 56 {
		t.Fatalf("node history sequence regressed across visits: %+v %v", last, err)
	}
	box, firstPage, more, err := e.InstructionHistory(ctx, visit.RunID, visit.NodeID, "", 0)
	if err != nil || !more || box.Iteration != 2 || len(firstPage) != 50 || firstPage[0].Sequence != 7 || firstPage[49].Sequence != 56 {
		t.Fatalf("current page is incomplete: count=%d more=%v err=%v", len(firstPage), more, err)
	}
	_, older, more, err := e.InstructionHistory(ctx, visit.RunID, visit.NodeID, "", firstPage[0].Sequence)
	if err != nil || more || len(older) != 6 || older[0].Sequence != 1 || older[5].Sequence != 6 {
		t.Fatalf("older page lost history: count=%d more=%v err=%v", len(older), more, err)
	}
}

func TestWorkflowInstructionClaimRejectsCancelledOrSupersededVisits(t *testing.T) {
	for _, condition := range []string{"cancelled", "manual", "new-visit"} {
		t.Run(condition, func(t *testing.T) {
			e, q := instructionFixture(t)
			ctx := context.Background()
			if _, err := e.EnqueueInstruction(ctx, "instruction-run", "impl", "alice", instructionInput(q, "must remain on this execution")); err != nil {
				t.Fatal(err)
			}
			switch condition {
			case "cancelled":
				if err := e.db.Model(&models.Run{}).Where("id = ?", "instruction-run").Update("status", "cancelled").Error; err != nil {
					t.Fatal(err)
				}
			case "manual":
				if err := e.db.Model(&models.StateRun{}).Where("id = ?", q.mailboxID).Update("mode", models.NodeModeManual).Error; err != nil {
					t.Fatal(err)
				}
			case "new-visit":
				if err := e.db.Create(&models.StateRun{RunID: "instruction-run", NodeID: "impl", Iteration: 2, Status: "running"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if item, err := q.Next(ctx, true); err == nil || item != nil {
				t.Fatalf("an inactive target dispatched user intent: %+v %v", item, err)
			}
		})
	}
}
