package services

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSandboxHistoryAuthenticatesWithPersistedToken(t *testing.T) {
	for _, live := range []bool{false, true} {
		name := "after-platform-restart"
		if live {
			name = "live-connection"
		}
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			s := newSandboxService(t, db, &dockerState{})
			srv, host, port := eventLogWSServer(t)
			t.Cleanup(srv.Close)
			original := srv.Config.Handler
			var loginCount atomic.Int32
			srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/login" {
					loginCount.Add(1)
					var body struct {
						Password string `json:"password"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "persisted-token" {
						http.Error(w, "unauthorized", http.StatusUnauthorized)
						return
					}
					http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "session", Path: "/"})
					return
				}
				cookie, err := r.Cookie("agentchat_session")
				if err != nil || cookie.Value != "session" {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				if r.URL.Path == "/api/events" {
					_ = json.NewEncoder(w).Encode(map[string]any{"events": []map[string]any{jsonRaw("")}, "hasMore": false})
					return
				}
				original.ServeHTTP(w, r)
			})
			row := &models.Sandbox{Name: "authenticated-history", Token: "persisted-token", Host: host, ACPPort: port}
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
			if live {
				s.live[row.ID] = &liveSandbox{sb: &sandbox.Sandbox{Host: host, Port: port, Password: "stale-memory-token"}}
			}
			ctx := context.Background()
			if events, err := s.Events(ctx, row.ID); err != nil || len(events) == 0 {
				t.Fatalf("events=%v err=%v", events, err)
			}
			if frames, err := s.EventLog(ctx, row.ID); err != nil || len(frames) == 0 {
				t.Fatalf("frames=%v err=%v", frames, err)
			}
			for _, cursor := range []string{"", "1"} {
				page, err := s.EventLogPage(ctx, row.ID, cursor, 20)
				if err != nil || page == nil || len(page.Events) == 0 {
					t.Fatalf("cursor=%q page=%+v err=%v", cursor, page, err)
				}
			}
			if loginCount.Load() != 4 {
				t.Fatalf("loginCount=%d want 4", loginCount.Load())
			}
		})
	}
}
