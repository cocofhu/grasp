package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

type eventLogAuthCounts struct {
	logins atomic.Int32
	reads  atomic.Int32
	expire atomic.Bool
	deny   atomic.Bool
}

// Neither /ws nor /api/events can be read before login; the password is not a cookie.
func protectedEventLogServer(t *testing.T, cookieName string) (string, int, *eventLogAuthCounts) {
	t.Helper()
	counts := &eventLogAuthCounts{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		n := counts.logins.Add(1)
		var body struct {
			Password string `json:"password"`
		}
		if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "bridge-secret" || counts.deny.Load() {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: strconv.Itoa(int(n)), Path: "/"})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var connect map[string]any
		if conn.ReadJSON(&connect) != nil {
			return
		}
		if connect["op"] != "connect" || connect["autoPermission"] != true {
			t.Errorf("unexpected observer handshake: %v", connect)
			return
		}
		var events []map[string]any
		for turn := 1; turn <= 10; turn++ {
			events = append(events, map[string]any{"type": "prompt_begin"}, chunkEvent(fmt.Sprintf("recent-%d", turn)))
		}
		_ = conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "protected-session", "eventLog": events, "totalTurns": 11, "hasMoreTurns": true})
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("before") != "1" {
			t.Errorf("before=%q, want 1", r.URL.Query().Get("before"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"events": []map[string]any{{"type": "prompt_begin"}, chunkEvent("older")}, "hasMore": false})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/login" {
			counts.reads.Add(1)
			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value != strconv.Itoa(int(counts.logins.Load())) || counts.expire.Swap(false) || counts.deny.Load() {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	host, port := httpHostPort(t, srv)
	return host, port, counts
}

func TestAuthenticatedEventLogReaders(t *testing.T) {
	for _, cookieName := range []string{acpSessionCookieName} {
		t.Run(cookieName, func(t *testing.T) {
			host, port, counts := protectedEventLogServer(t, cookieName)
			ctx := context.Background()
			frames, session, err := FetchEventLogRawWithPassword(ctx, host, port, "bridge-secret")
			if err != nil || session != "protected-session" || len(frames) != 22 {
				t.Fatalf("full WS + HTTP history: frames=%d session=%q err=%v", len(frames), session, err)
			}
			if counts.logins.Load() != 1 || counts.reads.Load() != 2 {
				t.Fatalf("login cookie was not shared: logins=%d reads=%d", counts.logins.Load(), counts.reads.Load())
			}
			result, _, err := FetchEventLogWithPassword(ctx, host, port, "bridge-secret")
			if err != nil || !strings.HasPrefix(result.Narration, "olderrecent-1") {
				t.Fatalf("aggregate: result=%+v err=%v", result, err)
			}
			last, _, err := FetchEventLogLastTurnWithPassword(ctx, host, port, "bridge-secret")
			if err != nil || last.Narration != "recent-10" {
				t.Fatalf("last turn: result=%+v err=%v", last, err)
			}
			first, err := FetchEventLogPageWithPassword(ctx, host, port, "", 50, "bridge-secret")
			if err != nil || len(first.Events) != 20 {
				t.Fatalf("initial page: %+v err=%v", first, err)
			}
			older, err := FetchEventLogPageWithPassword(ctx, host, port, "1", 50, "bridge-secret")
			if err != nil || len(older.Events) != 2 || older.HasMore {
				t.Fatalf("HTTP page: %+v err=%v", older, err)
			}
		})
	}
}

func TestEventLogRejectedCredentialsNeverRetryAnonymous(t *testing.T) {
	host, port, counts := protectedEventLogServer(t, "agentchat_session")
	ctx := context.Background()
	if frames, _, err := FetchEventLogRawWithPassword(ctx, host, port, "wrong-token"); err == nil || len(frames) != 0 || strings.Contains(err.Error(), "wrong-token") {
		t.Fatalf("raw rejected credentials: frames=%d err=%v", len(frames), err)
	}
	for _, cursor := range []string{"", "1"} {
		if page, err := FetchEventLogPageWithPassword(ctx, host, port, cursor, 50, "wrong-token"); err == nil || page != nil || strings.Contains(err.Error(), "wrong-token") {
			t.Fatalf("page rejected credentials: page=%+v err=%v", page, err)
		}
	}
	if counts.logins.Load() != 3 || counts.reads.Load() != 0 {
		t.Fatalf("rejected token attempted history access: logins=%d reads=%d", counts.logins.Load(), counts.reads.Load())
	}
	frames, _, err := FetchEventLogRawWithPassword(ctx, host, port, "bridge-secret")
	if err != nil || len(frames) != 22 {
		t.Fatalf("retry: frames=%d err=%v", len(frames), err)
	}
}

func TestEventLogReaderReusesSessionAndRefreshesExpiredCookie(t *testing.T) {
	for _, cursor := range []string{"", "1"} {
		t.Run("cursor="+cursor, func(t *testing.T) {
			host, port, counts := protectedEventLogServer(t, "agentchat_session")
			reader := NewEventLogReader(host, port, "bridge-secret")
			ctx := context.Background()
			for i := 0; i < 3; i++ {
				if _, err := reader.Page(ctx, cursor, 50); err != nil {
					t.Fatal(err)
				}
			}
			if counts.logins.Load() != 1 {
				t.Fatalf("polls created %d sessions, want 1", counts.logins.Load())
			}
			counts.expire.Store(true)
			if _, err := reader.Page(ctx, cursor, 50); err != nil {
				t.Fatalf("expired cookie: %v", err)
			}
			if counts.logins.Load() != 2 {
				t.Fatalf("refresh created %d total sessions, want 2", counts.logins.Load())
			}
			if _, err := reader.Page(ctx, cursor, 50); err != nil {
				t.Fatal(err)
			}
			if counts.logins.Load() != 2 {
				t.Fatal("refreshed cookie was not reused")
			}
			// Revoked credentials abort the refresh, without an anonymous retry.
			counts.deny.Store(true)
			readsBefore := counts.reads.Load()
			if _, err := reader.Page(ctx, cursor, 50); err == nil {
				t.Fatal("revoked credentials accepted")
			}
			if counts.logins.Load() != 3 || counts.reads.Load() != readsBefore+1 {
				t.Fatalf("unbounded/anonymous retry: logins=%d reads=%d", counts.logins.Load(), counts.reads.Load())
			}
		})
	}
}
