package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func coldResumeManager(t *testing.T, handler http.HandlerFunc) (*Manager, Spec, *BundleStore) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "runtime.tgz")
	writeRuntime(t, path, testVersion('a'), 2)
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), []byte(`{"mcpServers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	bundles := NewBundleStore()
	mgr := NewManager(NewGatewayClient(srv.URL, "synthetic-platform-key"), ManagerOptions{Runtime: NewRuntimeBundle(path), InjectStore: bundles, InjectAdvertise: "http://platform", CreateTimeout: time.Second, WorkspaceOperations: &WorkspaceRuntimeConfig{Lineage: "current-lineage-original"}})
	return mgr, Spec{Env: map[string]string{"ROOT_PASSWORD": "synthetic-root", "ACP_BRIDGE_PASSWORD": "synthetic-bridge", "AGENT_PROVIDER": "opencode", "MODEL_SECRET": "synthetic-current"}, ConfigHome: home, ConfigRoot: "/root/.config/opencode", GitBindingsConfigured: true}, bundles
}

func TestConfiguredResumeLostACKOnlyReadsOriginalColdReceipt(t *testing.T) {
	var mu sync.Mutex
	var opts GWStartOptions
	var receipt *GWColdStartReceipt
	posts, mutations := 0, 0
	mgr, spec, bundles := coldResumeManager(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != "GET" {
			mutations++
		}
		switch {
		case r.URL.Path == "/api/v2/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"driver": "kubernetes", "capabilities": map[string]bool{"coldConfiguration": true}})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/cold-starts/request-original"):
			if receipt == nil {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(receipt)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/cold-starts"):
			_ = json.NewEncoder(w).Encode(GWColdStartTarget{NativeIdentity: "original-deployment-uid", WorkspaceDir: "/root/workspace/retained"})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/cold-starts"):
			posts++
			if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			c := opts.ColdConfig
			receipt = &GWColdStartReceipt{RequestID: c.RequestID, InputSHA256: c.InputSHA256, PayloadSHA256: c.PayloadSHA256, NativeIdentity: c.ExpectedNativeIdentity, WorkspaceDir: c.Env["WORKSPACE_DIR"], State: "started"}
			hijacker := w.(http.Hijacker)
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
		default:
			_ = json.NewEncoder(w).Encode(GWSandbox{ID: "original-resource", Status: "running", Endpoints: map[string]string{"session": "127.0.0.1:8765"}})
		}
	})
	if ok, err := mgr.CanResumeConfigured(context.Background(), "original-resource"); !ok || err != nil {
		t.Fatalf("capability %v %v", ok, err)
	}
	if _, err := mgr.ResumeConfigured(context.Background(), "original-resource", spec, "request-original"); !errors.Is(err, ErrColdConfigurationUnknown) {
		t.Fatalf("lost ACK=%v", err)
	}
	mu.Lock()
	if posts != 1 || opts.ColdConfig.Env["GIT_REPOS"] != "" || opts.ColdConfig.Env["WORKSPACE_DIR"] != "/root/workspace/retained" || opts.ColdConfig.Env["GRASP_EXECUTION_LINEAGE"] != "current-lineage-original" || opts.ColdConfig.Env["GRASP_SANDBOX_RESOURCE_ID"] != "original-resource" || len(opts.ColdConfig.Injections) != 2 {
		t.Fatal("cold start did not preserve identity/current complete configuration")
	}
	for _, part := range opts.ColdConfig.Injections {
		id := bundleIDFromURL(part.URL)
		data, ok := bundles.Get(id, strings.TrimPrefix(opts.RuntimeHeaders, "Authorization: Bearer "))
		if !ok || int64(len(data)) != part.Bytes || coldDigest(data) != part.SHA256 {
			t.Fatal("lost ACK revoked or changed original configuration bytes")
		}
	}
	if _, ok := bundles.Get(bundleIDFromURL(opts.RuntimeURL), strings.TrimPrefix(opts.RuntimeHeaders, "Authorization: Bearer ")); !ok {
		t.Fatal("lost ACK removed in-flight runtime")
	}
	mu.Unlock()
	// A reconstructed config directory's file times are not original input.
	if err := os.Chtimes(filepath.Join(spec.ConfigHome, "mcp.json"), time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	sb, err := mgr.ResumeConfigured(context.Background(), "original-resource", spec, "request-original")
	if err != nil || sb.ID != "original-resource" || sb.WorkspaceDir != "/root/workspace/retained" || sb.Password != "synthetic-bridge" {
		t.Fatalf("original receipt not recovered: %+v %v", sb, err)
	}
	spec.Env["MODEL_SECRET"] = "synthetic-rotated"
	if _, err := mgr.ResumeConfigured(context.Background(), "original-resource", spec, "request-original"); !errors.Is(err, ErrColdConfigurationUnknown) {
		t.Fatalf("changed input reused nonce: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 || mutations != 1 {
		t.Fatalf("unknown recovery replayed native writes: posts=%d mutations=%d", posts, mutations)
	}
}

func TestConfiguredResumeUnsupportedProvidersNeverDispatch(t *testing.T) {
	for _, backend := range []string{"docker", "e2b", "kubernetes"} {
		t.Run(backend, func(t *testing.T) {
			reads, writes := 0, 0
			mgr, spec, _ := coldResumeManager(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
				}
				reads++
				_ = json.NewEncoder(w).Encode(map[string]any{"driver": backend, "capabilities": map[string]bool{"coldConfiguration": false}})
			})
			if ok, err := mgr.CanResumeConfigured(context.Background(), "original-resource"); ok || err != nil {
				t.Fatalf("unsupported capability %v %v", ok, err)
			}
			if _, err := mgr.ResumeConfigured(context.Background(), "original-resource", spec, "request-original"); !errors.Is(err, ErrColdConfigurationUnsupported) {
				t.Fatal(err)
			}
			if reads != 2 || writes != 0 {
				t.Fatalf("unsupported driver observed/mutated native environment: reads=%d writes=%d", reads, writes)
			}
		})
	}
}

func TestConfiguredResumeUnknownLookupNeverDispatches(t *testing.T) {
	for _, code := range []int{http.StatusConflict, http.StatusServiceUnavailable, http.StatusForbidden} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			writes, targets := 0, 0
			mgr, spec, _ := coldResumeManager(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
				}
				if r.URL.Path == "/api/v2/info" {
					_ = json.NewEncoder(w).Encode(map[string]any{"driver": "kubernetes", "capabilities": map[string]bool{"coldConfiguration": true}})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/cold-starts") {
					targets++
				}
				w.WriteHeader(code)
			})
			if _, err := mgr.ResumeConfigured(context.Background(), "original-resource", spec, "request-original"); !errors.Is(err, ErrColdConfigurationUnknown) {
				t.Fatal(err)
			}
			if writes != 0 || targets != 0 {
				t.Fatalf("ambiguous original receipt caused fresh admission: writes=%d targets=%d", writes, targets)
			}
		})
	}
}
