package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

type routeGateway struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []string
	records  map[string]GWSandbox
}

func newRouteGateway(t *testing.T, name string) *routeGateway {
	t.Helper()
	f := &routeGateway{records: map[string]GWSandbox{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+name {
			http.Error(w, "bad key", 401)
			return
		}
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		if r.URL.Path == "/api/v1/sandboxes" {
			if r.Method == http.MethodPost {
				var req GWCreateRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					http.Error(w, "body", 400)
					return
				}
				sb := GWSandbox{ID: "same-id", Status: "running", Image: req.Image, Labels: req.Labels, Endpoints: map[string]string{"session": "192.0.2.1:8765"}}
				f.records[sb.ID] = sb
				_ = json.NewEncoder(w).Encode(sb)
				return
			}
			var list []GWSandbox
			for _, sb := range f.records {
				list = append(list, sb)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"sandboxes": list})
			return
		}
		id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/v1/sandboxes/"), "/")
		sb, ok := f.records[id]
		if !ok {
			http.Error(w, "missing", 404)
			return
		}
		switch {
		case action == "logs":
			_ = json.NewEncoder(w).Encode(map[string]string{"content": name})
		case action == "status":
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "running"})
		case strings.HasPrefix(action, "hosts/") || action == "ports":
			_ = json.NewEncoder(w).Encode(map[string]string{"address": name + ":5173"})
		case r.Method == http.MethodDelete:
			delete(f.records, id)
			w.WriteHeader(204)
		default:
			_ = json.NewEncoder(w).Encode(sb)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func TestNamedGatewayBindingSurvivesDefaultSwitchAndRestart(t *testing.T) {
	legacy, docker, e2b := newRouteGateway(t, "legacy"), newRouteGateway(t, "docker"), newRouteGateway(t, "e2b")
	cfg := config.SandboxConfig{GatewayURL: legacy.server.URL, GatewayAPIKey: "legacy", Gateways: []config.GatewayConnectionConfig{
		{ID: "docker", URL: docker.server.URL, APIKey: "docker", Transport: "direct"},
		{ID: "e2b", URL: e2b.server.URL, APIKey: "e2b", Transport: "direct"},
	}, DefaultGateway: "e2b"}
	g, err := NewConfiguredGatewayClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	ctx := context.Background()
	labels := map[string]string{"run": "same-run"}
	old, err := g.Create(ctx, GWCreateRequest{Image: "image", Labels: labels})
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 || old.ID != qualifiedGatewayID("e2b", "same-id") {
		t.Fatalf("binding or input mutated: %+v, %v", old, labels)
	}
	// Restart with a new default and retain the prior stable connection entry.
	cfg.DefaultGateway = "docker"
	g2, err := NewConfiguredGatewayClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g2.Close() })
	current, err := g2.Create(ctx, GWCreateRequest{})
	if err != nil || current.ID == old.ID {
		t.Fatalf("same provider IDs collided: %+v %v", current, err)
	}
	bare, err := NewGatewayClient(legacy.server.URL, "legacy").Create(ctx, GWCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{old.ID, current.ID, bare.ID} {
		if _, err := g2.Get(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := g2.Stop(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := g2.StartPrepared(ctx, id, GWStartOptions{RuntimeURL: "http://runtime.test/bundle"}); err != nil {
			t.Fatal(err)
		}
		if err := g2.Start(ctx, id); err != nil {
			t.Fatal(err)
		}
		if status, err := g2.LiveStatus(ctx, id); err != nil || status != "running" {
			t.Fatalf("status: %s %v", status, err)
		}
	}
	for id, want := range map[string]string{old.ID: "e2b", current.ID: "docker", bare.ID: "legacy"} {
		if logs, err := g2.Logs(ctx, id, 12); err != nil || logs != want {
			t.Fatalf("logs: %s %v", logs, err)
		}
		mgr := NewManager(g2, ManagerOptions{})
		if addr, err := mgr.HostForPort(ctx, id, 5173); err != nil || addr != want+":5173" {
			t.Fatalf("host: %s %v", addr, err)
		}
		if addr, err := mgr.PublishPort(ctx, id, 5173); err != nil || addr != want+":5173" {
			t.Fatalf("publish: %s %v", addr, err)
		}
	}
	list, err := g2.List(ctx, "run:same-run")
	if err != nil || len(list) != 3 {
		t.Fatalf("list: %+v %v", list, err)
	}
	for _, f := range []*routeGateway{legacy, docker, e2b} {
		f.mu.Lock()
		for _, request := range f.requests {
			if strings.Contains(request, gatewayIDPrefix) {
				t.Errorf("qualified ID escaped into provider request: %s", request)
			}
		}
		f.mu.Unlock()
	}
	for _, id := range []string{old.ID, current.ID, bare.ID} {
		if err := g2.Destroy(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if list, err := g2.List(ctx); err != nil || len(list) != 0 {
		t.Fatalf("destroy: %v %v", list, err)
	}
}

func TestGatewayBindingsFailClosed(t *testing.T) {
	f := newRouteGateway(t, "legacy")
	g := NewGatewayClient(f.server.URL, "legacy")
	for _, id := range []string{"gw1_bad", "gw1_e2b_%%", qualifiedGatewayID("removed", "same-id"), qualifiedGatewayID("e2b", "../other"), qualifiedGatewayID("e2b", "")} {
		if _, err := g.Get(context.Background(), id); err == nil {
			t.Fatalf("accepted binding %s", id)
		}
		if err := g.Stop(context.Background(), id); err == nil {
			t.Fatalf("routed binding %s", id)
		}
		if !g.UsesGatewayTransport(id) {
			t.Fatal("unavailable binding allowed stale persisted endpoint")
		}
	}
	if g.UsesGatewayTransport("legacy-id") {
		t.Fatal("legacy direct changed")
	}
	if len(f.requests) != 0 {
		t.Fatalf("fallback network calls: %v", f.requests)
	}
	_, _, err := g.routePath("/api/v1/sandboxes/%zz/logs")
	if err == nil {
		t.Fatal("invalid URL escape accepted")
	}
}

func TestGatewayListMigrationDeduplicatesAndRejectsIncompleteResults(t *testing.T) {
	f := newRouteGateway(t, "key")
	old, err := NewGatewayClient(f.server.URL, "key").Create(context.Background(), GWCreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.records["old"] = *old
	f.records["old"] = GWSandbox{ID: "old", Status: "running"}
	f.mu.Unlock()
	cfg := config.SandboxConfig{GatewayURL: f.server.URL, GatewayAPIKey: "key", DefaultGateway: "docker", Gateways: []config.GatewayConnectionConfig{{ID: "docker", URL: f.server.URL, APIKey: "key", Transport: "direct"}}}
	g, err := NewConfiguredGatewayClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Create(context.Background(), GWCreateRequest{}); err != nil {
		t.Fatal(err)
	}
	list, err := g.List(context.Background())
	if err != nil || len(list) != 2 {
		t.Fatalf("migration list: %v %v", list, err)
	}
	f.mu.Lock()
	f.records["wrong"] = GWSandbox{ID: "wrong", Labels: map[string]string{gatewayBindingLabel: "removed"}}
	f.mu.Unlock()
	if _, err := g.List(context.Background()); err == nil {
		t.Fatal("unknown list owner accepted")
	}
	f.server.Close()
	if list, err := g.List(context.Background()); err == nil || list != nil {
		t.Fatalf("partial list accepted: %v %v", list, err)
	}
	if _, err := NewConfiguredGatewayClient(config.SandboxConfig{GatewayURL: "bad"}); err == nil {
		t.Fatal("bad config accepted")
	}
	cfg.Gateways[0].APIKeyEnv = "GRASP_TEST_MISSING_GATEWAY_KEY"
	t.Setenv("GRASP_TEST_MISSING_GATEWAY_KEY", "")
	if _, err := NewConfiguredGatewayClient(cfg); err == nil || strings.Contains(fmt.Sprint(err), "Bearer") {
		t.Fatalf("missing env key: %v", err)
	}
}
