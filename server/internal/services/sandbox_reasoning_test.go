package services

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestAlignChatSettingsAppliesPairAndReadsBeforeRestart(t *testing.T) {
	svc := newSandboxService(t, newTestDB(t), &dockerState{})
	selected, effort := "aux", "high"
	posts, readable, effortSupported := 0, false, true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/api/login":
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "fixture", Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/models":
			if !readable {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			body := map[string]any{"selected": selected}
			if effortSupported {
				body["reasoningEffort"] = effort
			}
			_ = json.NewEncoder(w).Encode(body)
		case "/api/model":
			var body struct {
				Model           string  `json:"model"`
				ReasoningEffort *string `json:"reasoningEffort"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			posts++
			selected = body.Model
			if body.ReasoningEffort != nil {
				effort = *body.ReasoningEffort
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"selected": selected, "reasoningEffort": effort})
		default:
			http.NotFound(w, req)
		}
	}))
	defer server.Close()
	host, rawPort, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatal(err)
	}
	row := &models.Sandbox{Name: "settings-fixture", Host: host, ACPPort: port, Token: "fixture", Status: "running"}
	if err := svc.db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := svc.AlignChatSettings(ctx, row.ID, "aux", "ultra"); !errors.Is(err, ErrChatModelUnknown) || posts != 0 {
		t.Fatalf("unread chat restarted: posts=%d err=%v", posts, err)
	}
	readable = true
	if changed, err := svc.AlignChatSettings(ctx, row.ID, "aux", "high"); err != nil || changed || posts != 0 {
		t.Fatalf("matching settings restarted: changed=%v posts=%d err=%v", changed, posts, err)
	}
	if changed, err := svc.AlignChatSettings(ctx, row.ID, "aux", "ultra"); err != nil || !changed || posts != 1 || selected != "aux" || effort != "ultra" {
		t.Fatalf("effort-only: changed=%v posts=%d pair=%s/%s err=%v", changed, posts, selected, effort, err)
	}
	if changed, err := svc.AlignChatSettings(ctx, row.ID, "", "low"); err != nil || !changed || posts != 2 || selected != "" || effort != "low" {
		t.Fatalf("combined: changed=%v posts=%d pair=%s/%s err=%v", changed, posts, selected, effort, err)
	}
	effortSupported = false
	if _, err := svc.AlignChatSettings(ctx, row.ID, "aux", "high"); err == nil || posts != 2 {
		t.Fatalf("old runtime silently accepted effort: posts=%d err=%v", posts, err)
	}
	if changed, err := svc.AlignChatSettings(ctx, row.ID, "aux", ""); err != nil || !changed || posts != 3 {
		t.Fatalf("old runtime lost model switching: changed=%v posts=%d err=%v", changed, posts, err)
	}
}
