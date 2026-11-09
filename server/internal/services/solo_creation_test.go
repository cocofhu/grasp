package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"gorm.io/gorm"
)

type creationPayloadObjects struct {
	*journalViewObjects
	beforePut func(context.Context) error
}

func (s *creationPayloadObjects) Put(ctx context.Context, key string, body io.ReadSeeker, info objectstore.Info) error {
	if err := s.beforePut(ctx); err != nil {
		return err
	}
	return s.journalViewObjects.Put(ctx, key, body, info)
}

func TestSoloCreationPreparesFirstBodyBeforeOuterSQLTransaction(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 1)
	insertSoloModelConfig(t, db)
	started, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	objects := &creationPayloadObjects{journalViewObjects: &journalViewObjects{objects: map[string]journalViewObject{}}}
	objects.beforePut = func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	// Fail after successful publication so no provisioner/native process is
	// launched. Every SQL admission row must roll back together.
	if err := db.Callback().Create().Before("gorm:create").Register("test:creation-prepared-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "chat_queue_turns" {
			_ = tx.AddError(errors.New("injected creation SQL failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := solo.Create(context.Background(), "alice", SoloCreateInput{ClientRequestID: "blocked-first-body", AgentName: "agentA", Text: "durable initial input"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first-body publication did not start")
	}
	probe, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// A synchronous write on the only SQLite connection proves that even
	// Creation's outer compute/workflow-admission TX has not started yet.
	if err := db.WithContext(probe).Create(&models.Setting{Key: "creation-upload-probe", Value: "connection free"}).Error; err != nil {
		t.Fatalf("first-body upload held the outer creation transaction: %v", err)
	}
	for _, model := range []any{&models.SoloSession{}, &models.SoloCreation{}, &models.Sandbox{}, &models.ChatQueueTurn{}} {
		var count int64
		if err := db.WithContext(probe).Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("creation published SQL before its body: %T count=%d %v", model, count, err)
		}
	}
	unblock.Do(func() { close(release) })
	select {
	case err := <-done:
		var failure *SoloError
		if !errors.As(err, &failure) || failure.Status != 503 || failure.Code != "creation_not_confirmed" {
			t.Fatalf("failed SQL falsely accepted prepared creation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("creation failed to finish after publication")
	}
	for _, model := range []any{&models.SoloSession{}, &models.SoloCreation{}, &models.Sandbox{}, &models.ChatQueueTurn{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("failed receipt leaked creation state: %T count=%d %v", model, count, err)
		}
	}
	objects.mu.Lock()
	defer objects.mu.Unlock()
	if len(objects.objects) != 1 {
		t.Fatalf("failed SQL did not preserve exactly one prepared orphan: %d", len(objects.objects))
	}
}

func TestSoloCreationParallelRetriesRetainIdentityAndFirstCommand(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, gateway, db := newSoloFixture(t, port, 1)
	insertSoloModelConfig(t, db)
	chatter := newRestoreChat(true)
	solo.SetChatterForTest(chatter)
	solo.SetModelAlignerForTest(&recordAligner{})
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	other.SetChatterForTest(chatter)
	other.SetModelAlignerForTest(&recordAligner{})
	// The provider must never see an unbound creation. This handler runs
	// after the SQL reservation transaction has released its locks.
	original := gateway.Server.Config.Handler
	gateway.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sandboxes") {
			var row models.SoloSession
			if err := db.First(&row).Error; err != nil || row.SandboxID == 0 {
				t.Errorf("provider started before parent binding: sandbox=%d err=%v", row.SandboxID, err)
			}
		}
		original.ServeHTTP(w, r)
	})
	input := SoloCreateInput{ClientRequestID: "one-first-command", AgentName: "agentA", Text: "first native action"}
	var wg sync.WaitGroup
	results := make(chan *SoloSessionView, 16)
	failures := make(chan error, 16)
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			service := solo
			if i%2 == 1 {
				service = other
			}
			view, err := service.Create(context.Background(), "alice", input)
			if err != nil {
				failures <- err
				return
			}
			results <- view
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var sessionID, turnID string
	for view := range results {
		if view.ClientRequestID != input.ClientRequestID || view.InitialTurnID == "" || (sessionID != "" && view.ID != sessionID) || (turnID != "" && view.InitialTurnID != turnID) {
			t.Fatalf("inconsistent creation receipt: %+v", view)
		}
		sessionID, turnID = view.ID, view.InitialTurnID
	}
	if t.Failed() || sessionID == "" {
		t.FailNow()
	}
	select {
	case <-chatter.started:
	case <-time.After(5 * time.Second):
		t.Fatal("accepted first command did not start")
	}
	close(chatter.release)
	waitFor(t, "first command finished", func() bool {
		receipt, err := solo.durable.Receipt(context.Background(), soloQueueScope(sessionID), turnID)
		return err == nil && receipt.State == chatsession.TurnSucceeded
	})
	for _, model := range []any{&models.SoloSession{}, &models.SoloCreation{}, &models.Sandbox{}, &models.ChatQueueTurn{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("retry created duplicates: %T count=%d err=%v", model, count, err)
		}
	}
	if len(chatter.seen()) != 1 {
		t.Fatalf("first command replayed: %v", chatter.seen())
	}
	if _, err := solo.Close(context.Background(), sessionID, "alice"); err != nil {
		t.Fatal(err)
	}
	again, err := other.Create(context.Background(), "alice", input)
	if err != nil || again.ID != sessionID || again.Status != models.SoloStatusClosed || len(chatter.seen()) != 1 {
		t.Fatalf("retry after close provisioned/replayed: %+v err=%v", again, err)
	}
	changed := input
	changed.Text = "different action"
	_, err = other.Create(context.Background(), "alice", changed)
	var failure *SoloError
	if !errors.As(err, &failure) || failure.Code != "creation_identity_conflict" {
		t.Fatalf("identity accepted changed content: %v", err)
	}
	if err := solo.Delete(context.Background(), sessionID, "alice"); err != nil {
		t.Fatal(err)
	}
	_, err = other.Create(context.Background(), "alice", input)
	if !errors.As(err, &failure) || failure.Status != 410 {
		t.Fatalf("deleted original recreated: %v", err)
	}
}

func TestSoloCreationActorIsolationAndPassiveResolution(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, _, _, db := newSoloFixture(t, port, 2)
	input := SoloCreateInput{ClientRequestID: "same-client-identity", AgentName: "agentA"}
	alice, err := solo.Create(context.Background(), "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	var failure *SoloError
	if _, err := solo.ResolveCreation(context.Background(), "bob", input.ClientRequestID); !errors.As(err, &failure) || failure.Status != 404 {
		t.Fatalf("another actor resolved private request: %v", err)
	}
	bob, err := solo.Create(context.Background(), "bob", input)
	if err != nil || bob.ID == alice.ID || bob.UserID != "bob" {
		t.Fatalf("actors shared request namespace: %+v %v", bob, err)
	}
	waitFor(t, "both fixture environments ready", func() bool {
		var ready int64
		db.Model(&models.SoloSession{}).Where("status = ?", models.SoloStatusRunning).Count(&ready)
		return ready == 2
	})
	if _, err := solo.Close(context.Background(), alice.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	before := int64(0)
	db.Model(&models.Sandbox{}).Count(&before)
	resolved, err := solo.ResolveCreation(context.Background(), "alice", input.ClientRequestID)
	if err != nil || resolved.ID != alice.ID || resolved.Status != models.SoloStatusClosed {
		t.Fatalf("resolution changed closed chat: %+v %v", resolved, err)
	}
	var after int64
	db.Model(&models.Sandbox{}).Count(&after)
	if before != after {
		t.Fatal("resolution provisioned compute")
	}
}

func TestSoloCreationRestoresOnlyAcceptedUnboundIntents(t *testing.T) {
	acp, port := fakeACPServer(t)
	defer acp.Close()
	solo, sbx, _, db := newSoloFixture(t, port, 1)
	row := models.SoloSession{ID: "accepted-before-process-exit", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStarting}
	input := SoloCreateInput{ClientRequestID: "creation-before-process-exit", AgentName: "agentA", Text: "survive the reservation gap"}
	turn, err := solo.initialSoloTurn(context.Background(), &row, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := solo.prepareInitialSoloTurn(context.Background(), &row, turn)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, _ := soloCreationFingerprint(input, "")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := solo.admitInitialSoloTurn(context.Background(), tx, &row, prepared); err != nil {
			return err
		}
		return tx.Create(&models.SoloCreation{ID: soloCreationKey("alice", input.ClientRequestID), UserID: "alice", Fingerprint: fingerprint, SessionID: row.ID, InitialTurnID: turn.ID}).Error
	}); err != nil {
		t.Fatal(err)
	}
	ghost := models.SoloSession{ID: "legacy-unrequested", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusStarting}
	if err := db.Create(&ghost).Error; err != nil {
		t.Fatal(err)
	}
	chatter := newRestoreChat(false)
	solo.SetChatterForTest(chatter)
	solo.SetModelAlignerForTest(&recordAligner{})
	if ready, err := solo.soloQueueReady(row.ID); err != nil || ready {
		t.Fatalf("command executable before sandbox reservation: ready=%v err=%v", ready, err)
	}
	if _, err := solo.ResolveCreation(context.Background(), "alice", input.ClientRequestID); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&models.Sandbox{}).Count(&count)
	if count != 0 {
		t.Fatal("passive receipt lookup started compute")
	}
	other := NewSoloService(db, sbx, solo.agents, solo.projects)
	other.SetChatterForTest(chatter)
	other.SetModelAlignerForTest(&recordAligner{})
	var wg sync.WaitGroup
	for _, service := range []*SoloService{solo, other} {
		wg.Add(1)
		go func() { defer wg.Done(); service.RestoreQueuedTurns() }()
	}
	wg.Wait()
	waitFor(t, "restored first command finishes", func() bool {
		receipt, err := solo.durable.Receipt(context.Background(), soloQueueScope(row.ID), turn.ID)
		return err == nil && receipt.State == chatsession.TurnSucceeded
	})
	db.Model(&models.Sandbox{}).Count(&count)
	if count != 1 || len(chatter.seen()) != 1 {
		t.Fatalf("recovery duplicated environment or command: sandboxes=%d calls=%v", count, chatter.seen())
	}
	if err := db.First(&ghost, "id = ?", ghost.ID).Error; err != nil || ghost.SandboxID != 0 {
		t.Fatal("recovery provisioned legacy unrequested environment")
	}
}

func TestSoloCreationContinuationReceiptDoesNotPretendToAcceptNavigationText(t *testing.T) {
	solo, _, _, db := newSoloFixture(t, 0, 1)
	parent := models.SoloSession{ID: "original-solo", UserID: "alice", Status: models.SoloStatusClosed, AgentName: "agentA"}
	run := models.Run{ID: "created-workflow", OriginSoloID: parent.ID, Status: "completed", Title: "first title"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	input := SoloCreateInput{ClientRequestID: "continue-existing", AgentName: "agentA", Text: "keep this as a draft"}
	view, err := solo.ContinueWorkflow(context.Background(), run.ID, "alice", input)
	if err != nil || view.ID != parent.ID || view.InitialTurnID != "" || view.ClientRequestID != input.ClientRequestID {
		t.Fatalf("existing-chat navigation acknowledged unsaved text: %+v %v", view, err)
	}
	db.Model(&run).Update("title", "new server title")
	again, err := solo.ContinueWorkflow(context.Background(), run.ID, "alice", input)
	if err != nil || again.ID != view.ID || again.InitialTurnID != "" {
		t.Fatalf("metadata changed receipt identity: %+v %v", again, err)
	}
	var count int64
	db.Model(&models.ChatQueueTurn{}).Count(&count)
	if count != 0 {
		t.Fatal("navigating closed history dispatched a command")
	}
}
