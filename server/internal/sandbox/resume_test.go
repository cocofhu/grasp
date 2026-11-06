package sandbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeStatusPreservesObservationFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
	}{
		{"gateway unavailable", "unavailable", 503},
		{"record missing", "not found", 404},
		{"empty status", `{}`, 200},
		{"invalid JSON", `{`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.code); _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			mgr := NewManager(NewGatewayClient(srv.URL, ""), ManagerOptions{})
			if status, err := mgr.ProbeStatus(context.Background(), "resident"); err == nil || status == "not_found" {
				t.Fatalf("observation failure became resource absence: %q %v", status, err)
			}
		})
	}
	if _, err := NewManager(nil, ManagerOptions{}).ProbeStatus(context.Background(), ""); err == nil {
		t.Fatal("missing gateway must fail")
	}
}

func TestResumePreparesExistingContainerAndRetainsItOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready", true: "failed readiness"}[fail], func(t *testing.T) {
			store := NewBundleStore()
			var opts GWStartOptions
			deletes := 0
			started := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					deletes++
					w.WriteHeader(204)
					return
				}
				if r.Method == http.MethodPost {
					if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
						t.Error(err)
					}
					started = true
					w.WriteHeader(200)
					return
				}
				status, message := "running", ""
				if fail {
					status, message = "error", "bootstrap unavailable"
				}
				_ = json.NewEncoder(w).Encode(GWSandbox{ID: "original-id", Status: status, Error: message, Endpoints: map[string]string{"session": "127.0.0.1:8765"}})
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "rt.tgz")
			data := writeRuntime(t, path, testVersion('a'), 2)
			mgr := NewManager(NewGatewayClient(srv.URL, ""), ManagerOptions{Runtime: NewRuntimeBundle(path), InjectStore: store, InjectAdvertise: "http://server", CreateTimeout: time.Second})
			sb, err := mgr.Resume(context.Background(), "original-id")
			if (err != nil) != fail {
				t.Fatalf("resume=%+v %v", sb, err)
			}
			if !fail && sb.ID != "original-id" {
				t.Fatal("resume changed sandbox identity")
			}
			if !started || deletes != 0 {
				t.Fatalf("started=%v deletes=%d", started, deletes)
			}
			if !strings.Contains(opts.BootstrapScript, "fixture bootstrap") {
				t.Fatal("legacy container bootstrap was not included")
			}
			id := strings.TrimSuffix(strings.TrimPrefix(opts.RuntimeURL, "http://server/sandbox-inject/"), ".tgz")
			bundle, ok := store.Get(id, strings.TrimPrefix(opts.RuntimeHeaders, "Authorization: Bearer "))
			if !ok || string(bundle) != string(data) {
				t.Fatal("resume URL did not publish the runtime bundle")
			}
		})
	}
}

func TestResumeRejectsMissingPrerequisitesWithoutStarting(t *testing.T) {
	if _, err := NewManager(nil, ManagerOptions{}).Resume(context.Background(), "resident"); err == nil {
		t.Fatal("missing gateway")
	}
	gw, _ := newInlineGW(t)
	path := filepath.Join(t.TempDir(), "missing.tgz")
	mgr := NewManager(gw, ManagerOptions{Runtime: NewRuntimeBundle(path)})
	if _, err := mgr.Resume(context.Background(), "resident"); err == nil {
		t.Fatal("missing runtime")
	}
	writeRuntime(t, path, testVersion('a'), 2)
	if _, err := mgr.Resume(context.Background(), "resident"); err == nil {
		t.Fatal("missing inject store")
	}
	if _, err := runtimeBootstrapScript([]byte("not gzip")); err == nil {
		t.Fatal("invalid runtime accepted")
	}
	if _, err := runtimeBootstrapScript(runtimeTgz(t, "")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}
