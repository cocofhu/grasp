package router

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"backend/internal/auth"
	"backend/internal/service"
	"backend/internal/workspacejob"
	"backend/internal/workspacesnapshot"
	"github.com/gin-gonic/gin"
)

// This opt-in subprocess lets the server module exercise its production client
// and publication worker against this module's actual authenticated router and
// native Git collector. It is never compiled into a runtime bundle.
func TestWorkspaceSnapshotProtocolFixture(t *testing.T) {
	if os.Getenv("GRASP_WORKSPACE_SNAPSHOT_FIXTURE") != "1" {
		t.Skip("cross-module protocol fixture")
	}
	controller, err := workspacesnapshot.NewController(workspacesnapshot.ControllerConfig{
		Engine:      workspacesnapshot.Config{Root: os.Getenv("GRASP_SNAPSHOT_TEST_SOURCE")},
		Jobs:        workspacejob.Config{Root: os.Getenv("GRASP_SNAPSHOT_TEST_CONTROL"), Resource: "resource-one", Lineage: "lineage-original"},
		RestoreRoot: os.Getenv("GRASP_SNAPSHOT_TEST_RESTORE"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = controller.Close(context.Background()) }()
	gin.SetMode(gin.TestMode)
	server := &http.Server{Handler: New(&Dependencies{WebRoot: t.TempDir(), Chats: service.NewChatManager(), Auth: auth.NewGuard("snapshot-fixture-password"), Workspace: controller}), ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	defer func() { _ = server.Close(); <-finished }()
	data, err := json.Marshal(map[string]string{"address": listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("GRASP_SNAPSHOT_TEST_READY"), data, 0600); err != nil {
		t.Fatal(err)
	}
	stop := os.Getenv("GRASP_SNAPSHOT_TEST_STOP")
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-t.Context().Done():
			return
		case <-deadline.C:
			t.Fatal("protocol fixture parent did not stop")
		case <-tick.C:
			if _, err := os.Stat(stop); err == nil {
				return
			}
		}
	}
}
