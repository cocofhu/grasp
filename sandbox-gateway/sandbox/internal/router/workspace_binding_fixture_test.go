package router

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/auth"
	"backend/internal/service"
	"backend/internal/workspacebinding"
	"backend/internal/workspacejob"
	"backend/internal/workspacesnapshot"
)

// This fixture uses the actual authenticated router, native file collector,
// materializer, ChatManager idle transition and persistent binding journal.
// It starts no external coding tool and makes no model request.
func TestWorkspaceBindingProtocolFixture(t *testing.T) {
	if os.Getenv("GRASP_WORKSPACE_BINDING_FIXTURE") != "1" {
		t.Skip("cross-module binding protocol fixture")
	}
	source, control, restores := os.Getenv("GRASP_SNAPSHOT_TEST_SOURCE"), os.Getenv("GRASP_SNAPSHOT_TEST_CONTROL"), os.Getenv("GRASP_SNAPSHOT_TEST_RESTORE")
	controller, err := workspacesnapshot.NewController(workspacesnapshot.ControllerConfig{Engine: workspacesnapshot.Config{Root: source}, Jobs: workspacejob.Config{Root: filepath.Join(control, "jobs"), Resource: "resource-one", Lineage: "lineage-original"}, RestoreRoot: restores, Binding: &workspacebinding.Config{ControlRoot: filepath.Join(control, "bindings"), OriginalRoot: source, RestoreRoot: restores, Resource: "resource-one", Lineage: "lineage-original"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = controller.Close(context.Background()) }()
	chats := service.NewChatManager()
	if err := chats.SetWorkspaceBinding(controller.Binding); err != nil {
		t.Fatal(err)
	}
	if err := controller.Binding.Install(t.Context(), chats.WorkspaceConsumer(), controller.RootConsumer("capture"), controller.RootConsumer("code")); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: New(&Dependencies{WebRoot: t.TempDir(), Chats: chats, Auth: auth.NewGuard("snapshot-fixture-password"), Workspace: controller}), ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	defer func() { _ = server.Close(); <-done }()
	data, _ := json.Marshal(map[string]string{"address": listener.Addr().String()})
	if err := os.WriteFile(os.Getenv("GRASP_SNAPSHOT_TEST_READY"), data, 0600); err != nil {
		t.Fatal(err)
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(3 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-t.Context().Done():
			return
		case <-timeout.C:
			t.Fatal("binding fixture parent did not stop")
		case <-tick.C:
			if _, err := os.Stat(os.Getenv("GRASP_SNAPSHOT_TEST_STOP")); err == nil {
				return
			}
		}
	}
}
