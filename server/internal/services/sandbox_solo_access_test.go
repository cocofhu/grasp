package services

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSoloInteractiveAccessRequiresCurrentAppliedAuthorization(t *testing.T) {
	for _, state := range []string{"revoked", "closed", "stop pending", "provider stopped"} {
		t.Run(state, func(t *testing.T) {
			s, row, credentials, ids, control := recoveryAuthorizationFixture(t)
			if err := s.db.Model(&row).Updates(map[string]any{"status": "running", "host": "127.0.0.1", "acp_port": 34567, "code_server_port": 8080}).Error; err != nil {
				t.Fatal(err)
			}
			switch state {
			case "revoked":
				if err := credentials.Revoke(ids["mcp"]); err != nil {
					t.Fatal(err)
				}
			case "closed":
				if err := s.db.Model(&models.SoloSession{}).Where("id = ?", row.ThreadID).Update("status", models.SoloStatusClosed).Error; err != nil {
					t.Fatal(err)
				}
			case "stop pending":
				if err := s.db.Model(&row).Update("desired_state", "stopped").Error; err != nil {
					t.Fatal(err)
				}
			}
			var mutations atomic.Int32
			hook := func(r *http.Request) {
				if r.Method != http.MethodGet {
					mutations.Add(1)
				}
			}
			control.hook.Store(&hook)
			ctx := context.Background()
			if _, _, token, err := s.acpConnection(ctx, row.ID); err == nil || token != "" {
				t.Fatal("denied ACP access disclosed a bridge credential")
			}
			if _, err := s.ACPUpstream(ctx, row.ID); err == nil {
				t.Fatal("ACP endpoint bypassed current authorization")
			}
			if _, err := s.IDEUpstream(ctx, row.ID); err == nil {
				t.Fatal("IDE endpoint bypassed current authorization")
			}
			if _, err := s.OpenTerminal(ctx, row.ID); err == nil {
				t.Fatal("terminal bypassed current authorization")
			}
			if _, err := s.ChatModelSettings(ctx, row.ID); err == nil {
				t.Fatal("model endpoint bypassed current authorization")
			}
			if _, err := s.EventLog(ctx, row.ID); err == nil {
				t.Fatal("native log access bypassed current authorization")
			}
			if mutations.Load() != 0 || control.starts.Load() != 0 {
				t.Fatal("denied interactive access started or updated the retained guest")
			}
		})
	}
}

func TestSoloHistoryViewDoesNotExposeUnavailableEnvironmentAccess(t *testing.T) {
	s, row, credentials, ids, control := recoveryAuthorizationFixture(t)
	if err := s.db.Model(&row).Updates(map[string]any{"status": "running", "acp_port": 34567, "code_server_port": 8080}).Error; err != nil {
		t.Fatal(err)
	}
	if err := credentials.Revoke(ids["model"]); err != nil {
		t.Fatal(err)
	}
	var mutations atomic.Int32
	hook := func(r *http.Request) {
		if r.Method != http.MethodGet {
			mutations.Add(1)
		}
	}
	control.hook.Store(&hook)
	view, err := s.GetView(context.Background(), row.ID)
	if err != nil || view.Password != "" || view.Connected || view.HasACP || view.HasCodeServer || len(view.Endpoints) != 0 {
		t.Fatalf("history exposed unavailable interactive access: %+v %v", view, err)
	}
	if view.ID != row.ID || view.Name != row.Name || view.ThreadID != row.ThreadID || mutations.Load() != 0 || control.starts.Load() != 0 {
		t.Fatal("history lookup modified the retained guest or lost attribution")
	}
}
