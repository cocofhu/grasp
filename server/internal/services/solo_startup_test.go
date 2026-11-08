package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSoloRecoveryLeavesNewSandboxCreationAlone(t *testing.T) {
	for _, status := range []string{"creating", "pulling"} {
		t.Run(status, func(t *testing.T) {
			acp, port := fakeACPServer(t)
			defer acp.Close()
			solo, sbx, gateway, db := newSoloFixture(t, port, 3)
			creating, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sandboxes" {
					close(creating)
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				gateway.Server.Config.Handler.ServeHTTP(w, r)
			}))
			defer proxy.Close()
			defer unblock()
			sbx.mgr = sandbox.NewManager(sandbox.NewGatewayClient(proxy.URL, ""), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
			view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA", Text: "evaluate this plan"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				unblock()
				waitFor(t, "new sandbox startup finished", func() bool {
					row, err := solo.Require(view.ID, "alice")
					return err == nil && row.Status == models.SoloStatusRunning
				})
				_ = solo.Cancel(view.ID, "alice")
				waitFor(t, "startup turn settled", func() bool { return !solo.lookup(view.ID).sess.Snapshot().Busy })
				_, _ = solo.Close(context.Background(), view.ID, "alice")
			}()
			select {
			case <-creating:
			case <-time.After(5 * time.Second):
				t.Fatal("gateway create did not start")
			}
			if err := db.Model(&models.Sandbox{}).Where("id = ?", view.SandboxID).Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			waitFor(t, "first message persisted while preparing", func() bool {
				messages, _ := solo.Messages(view.ID, "alice")
				return len(messages) == 1 && messages[0].Role == "user"
			})

			// The gateway has not assigned an id yet. Recovery must not probe
			// the local correlation name and turn the expected 404 into failure.
			sbx.RecoverSoloSandboxes(context.Background())
			row, err := sbx.Get(view.SandboxID)
			if err != nil || row.Status != status || row.Error != "" {
				t.Fatalf("recovery interrupted a new sandbox: %+v %v", row, err)
			}
			unblock()
			waitFor(t, "first message answered after creation", func() bool {
				messages, _ := solo.Messages(view.ID, "alice")
				return len(messages) == 2 && messages[1].Role == "assistant" && messages[1].Status == "ok" && messages[1].Content == "hi"
			})
		})
	}
}

func TestSoloPreparationFailurePersistsReply(t *testing.T) {
	for _, status := range []string{"failed", "stopped"} {
		t.Run(status, func(t *testing.T) {
			solo, _, _, db := newSoloFixture(t, 0, 3)
			row := models.SoloSession{ID: "preparing", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusError, Error: "sandbox startup failed"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if status == "stopped" {
				row.Status, row.Error = models.SoloStatusStarting, ""
				cancel()
			}
			if err := db.Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			chat := &soloChat{svc: solo, id: row.ID}
			_, err := chat.execute(ctx, &soloTurn{ID: "first-turn", Text: "evaluate this plan"})
			if err == nil {
				t.Fatal("expected preparation to fail")
			}
			messages, err := solo.Messages(row.ID, "alice")
			if err != nil || len(messages) != 2 {
				t.Fatalf("preparation failure must survive reload as a visible reply: %+v %v", messages, err)
			}
			user, reply := messages[0], messages[1]
			if user.Status != status || reply.Role != "assistant" || reply.ID != "reply:"+user.ID || reply.TurnID != user.ID || reply.Status != status || reply.Error == "" || reply.Error != user.Error {
				t.Fatalf("failure lost its turn or status: user=%+v reply=%+v", user, reply)
			}
			if status == "failed" && !strings.Contains(reply.Error, "sandbox startup failed") {
				t.Fatalf("startup error was lost: %q", reply.Error)
			}
		})
	}
}
