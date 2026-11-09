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
	"sync/atomic"
	"testing"
	"time"
)

func workspaceJobTestClient(t *testing.T, handler http.HandlerFunc) WorkspaceJobClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return WorkspaceJobClient{Host: host, Port: port, Password: "private-job-bridge", Timeout: time.Second}
}

func TestWorkspaceJobClientUsesAuthenticatedBoundedControlProtocol(t *testing.T) {
	var requests atomic.Int32
	fence := WorkspaceJobFence{Owner: "private-runtime-owner", Generation: 13, ExpiresAt: time.Now().Add(20 * time.Second)}
	client := workspaceJobTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			var login struct{ Password string }
			if err := json.NewDecoder(r.Body).Decode(&login); err != nil || login.Password != "private-job-bridge" {
				t.Error("missing bridge authentication", err)
			}
			http.SetCookie(w, &http.Cookie{Name: acpSessionCookieName, Value: "authenticated"})
			_, _ = io.WriteString(w, `{}`)
			return
		}
		requests.Add(1)
		cookie, err := r.Cookie(acpSessionCookieName)
		if err != nil || cookie.Value != "authenticated" {
			t.Error("runtime job request lost bridge authentication")
		}
		if r.URL.Path == "/api/workspace/jobs/capabilities" {
			_, _ = io.WriteString(w, `{"version":1,"kinds":[],"fileCapture":false,"fileRestore":false}`)
			return
		}
		if r.Method == http.MethodPatch || strings.HasSuffix(r.URL.Path, "/cancel") {
			var input WorkspaceJobRenew
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Fence.Owner != fence.Owner || input.Fence.Generation != fence.Generation || !input.Fence.ExpiresAt.Equal(fence.ExpiresAt) || input.Lineage != "lineage-original" {
				t.Error("control renewal lost its immutable operation fence", err)
			}
		}
		_, _ = io.WriteString(w, `{"version":1,"id":"job-original","state":"running"}`)
	})
	if caps, err := client.Capabilities(t.Context()); err != nil || caps.Version != 1 || caps.FileCapture || caps.FileRestore {
		t.Fatal(caps, err)
	}
	if _, err := client.Create(t.Context(), WorkspaceJobCreate{Fence: fence}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(t.Context(), "job-original"); err != nil {
		t.Fatal(err)
	}
	renew := WorkspaceJobRenew{SHA256: strings.Repeat("a", 64), Lineage: "lineage-original", Fence: fence}
	if _, err := client.Renew(t.Context(), "job-original", renew); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Cancel(t.Context(), "job-original", renew); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 5 {
		t.Fatal("client replayed a mutation", requests.Load())
	}
}

func TestWorkspaceJobClientReportsOldRuntimeAndHonorsCancellation(t *testing.T) {
	client := workspaceJobTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: acpSessionCookieName, Value: "session"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"code":"workspace_job_not_found"}`)
	})
	_, err := client.Capabilities(t.Context())
	var failure *WorkspaceJobError
	if !errors.As(err, &failure) || failure.Status != 404 {
		t.Fatal("unsupported runtime was silently substituted", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Get(ctx, "job-original"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled control request remained active", err)
	}
}
