package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceCodeClientRequiresBridgeAuthAndPhysicalFence(t *testing.T) {
	var requests int
	fence := WorkspaceCodeFence{Generation: 17, ExpiresAt: time.Now().Add(30 * time.Second)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			var body struct {
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "private-bridge-token" {
				t.Error("missing bridge authorization")
			}
			http.SetCookie(w, &http.Cookie{Name: acpSessionCookieName, Value: "authenticated"})
			_, _ = io.WriteString(w, `{}`)
			return
		}
		requests++
		cookie, err := r.Cookie(acpSessionCookieName)
		if err != nil || cookie.Value != "authenticated" {
			t.Error("runtime endpoint called without bridge cookie")
		}
		if r.Method == http.MethodPost {
			if r.Header.Get("X-Grasp-Operation-Generation") != "17" || r.Header.Get("X-Grasp-Operation-Expires-At") != fence.ExpiresAt.UTC().Format(time.RFC3339Nano) {
				t.Error("provider request lost its physical fence")
			}
		}
		switch r.URL.Path {
		case "/api/workspace/code":
			_, _ = io.WriteString(w, `{"version":1,"repositories":[],"workingChanges":false}`)
		case "/api/workspace/code/export":
			var body struct {
				Repositories []WorkspaceCodeSelection `json:"repositories"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Repositories) != 1 || body.Repositories[0].Path != "repo" {
				t.Error("wrong export selection")
			}
			_, _ = io.WriteString(w, "archive-stream")
		case "/api/workspace/code/imports/request":
			if r.Method == http.MethodPost {
				b, _ := io.ReadAll(r.Body)
				if string(b) != "archive-stream" || r.Header.Get("X-Grasp-Archive-SHA256") != strings.Repeat("a", 64) {
					t.Error("archive changed during transport")
				}
			}
			_, _ = io.WriteString(w, `{"id":"request","directory":".grasp-code-imports/request","archiveSha256":"`+strings.Repeat("a", 64)+`","repositories":[]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	host, portString, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if inventory, err := InspectWorkspaceCode(ctx, host, port, "private-bridge-token"); err != nil || inventory.Version != 1 {
		t.Fatal(inventory, err)
	}
	archive, err := ExportWorkspaceCode(ctx, host, port, "private-bridge-token", []WorkspaceCodeSelection{{Path: "repo", Commit: strings.Repeat("b", 40)}}, fence)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(archive)
	_ = archive.Close()
	if err != nil || string(data) != "archive-stream" {
		t.Fatal(string(data), err)
	}
	if result, err := ImportWorkspaceCode(ctx, host, port, "private-bridge-token", "request", strings.Repeat("a", 64), strings.NewReader(string(data)), fence); err != nil || result.ID != "request" {
		t.Fatal(result, err)
	}
	if result, err := GetWorkspaceCodeReceipt(ctx, host, port, "private-bridge-token", "request"); err != nil || result.ID != "request" {
		t.Fatal(result, err)
	}
	if requests != 4 {
		t.Fatalf("unexpected network mutation count %d", requests)
	}
	_, err = ImportWorkspaceCode(ctx, host, port, "private-bridge-token", "request", strings.Repeat("a", 64), strings.NewReader("data"), WorkspaceCodeFence{Generation: 1, ExpiresAt: time.Now().Add(-time.Second)})
	var remote *WorkspaceCodeError
	if !errors.As(err, &remote) || requests != 4 {
		t.Fatal("expired fence reached provider")
	}
}

func TestWorkspaceCodeClientReportsOldRuntimeWithoutFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: acpSessionCookieName, Value: "session"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	host, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	_, err := InspectWorkspaceCode(context.Background(), host, port, "token")
	var remote *WorkspaceCodeError
	if !errors.As(err, &remote) || remote.Status != http.StatusNotFound {
		t.Fatalf("old runtime was not capability gated: %v", err)
	}
}

func TestWorkspaceCodeRenewalRequiresTheExactAuthenticatedAcknowledgement(t *testing.T) {
	fence := WorkspaceCodeFence{Generation: 23, ExpiresAt: time.Now().UTC().Add(30 * time.Second)}
	for _, test := range []struct {
		name       string
		generation uint64
		expiry     time.Time
		status     int
		wantOK     bool
	}{
		{"exact", fence.Generation, fence.ExpiresAt, http.StatusOK, true},
		{"different generation", fence.Generation + 1, fence.ExpiresAt, http.StatusOK, false},
		{"short acknowledgement", fence.Generation, fence.ExpiresAt.Add(-time.Second), http.StatusOK, false},
		{"uncommitted extension", fence.Generation, fence.ExpiresAt.Add(time.Second), http.StatusOK, false},
		{"expired acknowledgement", fence.Generation, time.Now().Add(-time.Second), http.StatusOK, false},
		{"runtime rejected", fence.Generation, fence.ExpiresAt, http.StatusConflict, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/login" {
					http.SetCookie(w, &http.Cookie{Name: acpSessionCookieName, Value: "authorized"})
					return
				}
				calls++
				cookie, err := r.Cookie(acpSessionCookieName)
				if err != nil || cookie.Value != "authorized" || r.Method != http.MethodPatch || r.URL.Path != "/api/workspace/code/fence" || r.Header.Get("X-Grasp-Operation-Generation") != "23" || r.Header.Get("X-Grasp-Operation-Expires-At") != fence.ExpiresAt.Format(time.RFC3339Nano) {
					t.Error("renewal lost bridge authentication, generation or committed expiry")
				}
				w.WriteHeader(test.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"generation": test.generation, "expiresAt": test.expiry})
			}))
			defer server.Close()
			host, portText, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
			port, _ := strconv.Atoi(portText)
			err := RenewWorkspaceCode(t.Context(), host, port, "private-token", fence)
			if (err == nil) != test.wantOK || calls != 1 {
				t.Fatalf("wrong renewal result or replay: %v, requests=%d", err, calls)
			}
			if err := RenewWorkspaceCode(t.Context(), host, port, "private-token", WorkspaceCodeFence{Generation: 23, ExpiresAt: time.Now().Add(-time.Second)}); err == nil || calls != 1 {
				t.Fatal("expired lease reached the runtime")
			}
		})
	}
}
