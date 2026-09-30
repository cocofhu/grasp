package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func authenticatedRuntimeEventLog(t *testing.T) (*sandbox.Sandbox, *atomic.Int32) {
	t.Helper()
	srv, host, port := eventWSServer(t)
	t.Cleanup(srv.Close)
	original := srv.Config.Handler
	logins := &atomic.Int32{}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			logins.Add(1)
			var body struct {
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "runtime-token" {
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
		original.ServeHTTP(w, r)
	})
	return &sandbox.Sandbox{Host: host, Port: port, Password: "runtime-token"}, logins
}

func TestRuntimeEventHistoryAuthAndRetry(t *testing.T) {
	sb, logins := authenticatedRuntimeEventLog(t)
	p := &acpProvider{live: map[string]*sandbox.Sandbox{"r|n": sb}}
	ctx := context.Background()
	events, live, err := p.LiveNodeEvents(ctx, "r", "n")
	if err != nil || !live || len(events) != 1 || events[0].Text != "hello-snap" {
		t.Fatalf("live events=%+v live=%v err=%v", events, live, err)
	}
	page, _, _, live, err := p.LiveNodeEventsPage(ctx, "r", "n", "", 20)
	if err != nil || !live || len(page) != 1 || page[0].Text != "hello-snap" {
		t.Fatalf("live page=%+v live=%v err=%v", page, live, err)
	}
	fallback := []models.AcpEvent{{Kind: "message", Text: "streamed fallback"}}
	if snap := p.snapshotEvents(ctx, sb, fallback); len(snap) != 1 || snap[0].Text != "hello-snap" {
		t.Fatalf("persisted snapshot=%+v", snap)
	}
	if logins.Load() != 3 {
		t.Fatalf("logins=%d want 3", logins.Load())
	}

	// Authentication failure remains distinguishable; only the best-effort
	// teardown snapshot falls back to the events already streamed by the agent.
	sb.Password = "revoked-token"
	if _, live, err := p.LiveNodeEvents(ctx, "r", "n"); err == nil || live {
		t.Fatalf("failure hidden: live=%v err=%v", live, err)
	}
	if _, _, _, live, err := p.LiveNodeEventsPage(ctx, "r", "n", "", 20); err == nil || live {
		t.Fatalf("page failure hidden: live=%v err=%v", live, err)
	}
	if snap := p.snapshotEvents(ctx, sb, fallback); len(snap) != 1 || snap[0].Text != "streamed fallback" {
		t.Fatalf("fallback lost=%+v", snap)
	}
	sb.Password = "runtime-token"
	if events, live, err := p.LiveNodeEvents(ctx, "r", "n"); err != nil || !live || len(events) != 1 {
		t.Fatalf("retry events=%v live=%v err=%v", events, live, err)
	}
}

func TestTimelineUsesAuthenticatedReaderWithoutRepeatedLogin(t *testing.T) {
	sb, logins := authenticatedRuntimeEventLog(t)
	timeline := newAcpTimelineStore()
	reader := sandbox.NewEventLogReader(sb.Host, sb.Port, sb.Password)
	for i := 0; i < 3; i++ {
		timeline.refreshFromReader(context.Background(), "r", "n", reader)
	}
	entry, ok := timeline.get("r", "n")
	if !ok || len(entry.events) != 1 || entry.events[0].Text != "hello-snap" {
		t.Fatalf("timeline=%+v ok=%v", entry, ok)
	}
	if logins.Load() != 1 {
		t.Fatalf("polling allocated %d sessions, want 1", logins.Load())
	}
	timeline.refreshFromSandbox(context.Background(), "r", "n", sb.Host, sb.Port, "invalid-token")
	entry, ok = timeline.get("r", "n")
	if !ok || len(entry.events) != 1 || entry.events[0].Text != "hello-snap" {
		t.Fatalf("auth failure erased timeline=%+v ok=%v", entry, ok)
	}
}

func TestRuntimeRegistersAuthenticatedTimeline(t *testing.T) {
	for _, park := range []bool{false, true} {
		name := "register-live"
		if park {
			name = "park-react-session"
		}
		t.Run(name, func(t *testing.T) {
			sb, _ := authenticatedRuntimeEventLog(t)
			p := &acpProvider{live: map[string]*sandbox.Sandbox{}, sessions: map[string]*reactSession{}, timeline: newAcpTimelineStore()}
			req := NodeReq{RunID: "r", NodeID: "n"}
			if park {
				p.parkReactSession(req, sb, nil, "")
			} else {
				p.registerLive(req, sb, nil)
			}
			defer p.deregisterLive(req)
			deadline := time.Now().Add(2 * time.Second)
			for {
				if entry, ok := p.timeline.get("r", "n"); ok {
					if len(entry.events) != 1 || entry.events[0].Text != "hello-snap" {
						t.Fatalf("timeline=%+v", entry)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("authenticated timeline never populated")
				}
				time.Sleep(5 * time.Millisecond)
			}
		})
	}
}
