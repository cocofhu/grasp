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
	"time"
)

// gatewayFake is a fuller fake of the sandbox-gateway REST API for GatewayClient
// unit tests (create/get/list/destroy/stop/start/status + auth).
type gatewayFake struct {
	mu      sync.Mutex
	recs    map[string]map[string]any
	seq     int
	wantKey string
	seenKey string
}

func newGatewayFake(t *testing.T, apiKey string) (*GatewayClient, *gatewayFake) {
	t.Helper()
	fg := &gatewayFake{recs: map[string]map[string]any{}, wantKey: apiKey}
	srv := httptest.NewServer(http.HandlerFunc(fg.handle))
	t.Cleanup(srv.Close)
	return NewGatewayClient(srv.URL, apiKey), fg
}

func (fg *gatewayFake) handle(w http.ResponseWriter, r *http.Request) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	fg.seenKey = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if fg.wantKey != "" && fg.seenKey != fg.wantKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	p := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/sandboxes"), "/")
	parts := strings.Split(p, "/")
	enc := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case p == "" && r.Method == http.MethodPost:
		fg.seq++
		id := "gw-" + itoa(fg.seq)
		var body struct {
			Image     string            `json:"image"`
			Labels    map[string]string `json:"labels"`
			Resources *GWResources      `json:"resources"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec := map[string]any{
			"id": id, "name": id, "status": "creating",
			"image": body.Image, "labels": body.Labels,
			"endpoints": map[string]string{},
		}
		if body.Resources != nil {
			rec["resources"] = body.Resources
		}
		fg.recs[id] = rec
		enc(rec)
	case p == "" && r.Method == http.MethodGet:
		labels := r.URL.Query()["label"]
		out := []map[string]any{}
		for _, rec := range fg.recs {
			if len(labels) > 0 && !matchLabelFilters(rec, labels) {
				continue
			}
			out = append(out, rec)
		}
		enc(map[string]any{"sandboxes": out})
	case len(parts) == 1 && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		enc(rec)
	case len(parts) == 1 && r.Method == http.MethodDelete:
		delete(fg.recs, parts[0])
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "stop" && r.Method == http.MethodPost:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rec["status"] = "stopped"
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "start" && r.Method == http.MethodPost:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rec["status"] = "running"
		rec["endpoints"] = map[string]string{"session": "127.0.0.1:34567"}
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "status" && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		enc(map[string]any{"status": rec["status"]})
	case len(parts) == 2 && parts[1] == "logs" && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		content, _ := rec["logs"].(string)
		enc(map[string]any{"content": content})
	case len(parts) == 2 && parts[1] == "exit" && r.Method == http.MethodGet:
		rec := fg.recs[parts[0]]
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if fn, ok := rec["exitFn"].(func() any); ok {
			enc(map[string]any{"exit": fn()})
			return
		}
		enc(map[string]any{"exit": rec["exit"]})
	default:
		http.NotFound(w, r)
	}
}

func matchLabelFilters(rec map[string]any, filters []string) bool {
	labels, _ := rec["labels"].(map[string]string)
	if labels == nil {
		// JSON round-trip may yield map[string]any
		if raw, ok := rec["labels"].(map[string]any); ok {
			labels = map[string]string{}
			for k, v := range raw {
				labels[k] = fmt.Sprint(v)
			}
		}
	}
	for _, f := range filters {
		key, val, ok := strings.Cut(f, ":")
		if !ok || labels[key] != val {
			return false
		}
	}
	return true
}

func TestGatewayClientCRUD(t *testing.T) {
	cli, fg := newGatewayFake(t, "secret-key")
	if cli.BaseURL() == "" {
		t.Fatal("BaseURL empty")
	}
	ctx := context.Background()

	sb, err := cli.Create(ctx, GWCreateRequest{
		Image:     "img:test",
		Labels:    map[string]string{"grasp.managed": "1"},
		Resources: &GWResources{CPUCores: 2, MemoryMB: 8192, DiskGi: 40},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sb.ID == "" || sb.Status != "creating" {
		t.Fatalf("create response = %+v", sb)
	}
	if fg.seenKey != "secret-key" {
		t.Errorf("auth key = %q", fg.seenKey)
	}
	if sb.Image != "img:test" {
		t.Errorf("image = %q", sb.Image)
	}
	if sb.Resources == nil || sb.Resources.MemoryMB != 8192 {
		t.Errorf("resources = %+v", sb.Resources)
	}

	got, err := cli.Get(ctx, sb.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != sb.ID {
		t.Errorf("Get id = %q", got.ID)
	}

	list, err := cli.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %v err=%v", list, err)
	}
	filtered, err := cli.List(ctx, "grasp.managed:1")
	if err != nil || len(filtered) != 1 {
		t.Fatalf("List label filter = %v err=%v", filtered, err)
	}
	empty, err := cli.List(ctx, "grasp.managed:nope")
	if err != nil || len(empty) != 0 {
		t.Fatalf("List miss filter = %v err=%v", empty, err)
	}

	st, err := cli.LiveStatus(ctx, sb.ID)
	if err != nil || st != "creating" {
		t.Fatalf("LiveStatus = %q err=%v", st, err)
	}

	if err := cli.Stop(ctx, sb.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	st, _ = cli.LiveStatus(ctx, sb.ID)
	if st != "stopped" {
		t.Errorf("after Stop status = %q", st)
	}

	if err := cli.Start(ctx, sb.ID); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st, _ = cli.LiveStatus(ctx, sb.ID)
	if st != "running" {
		t.Errorf("after Start status = %q", st)
	}

	if err := cli.Destroy(ctx, sb.ID); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, err := cli.Get(ctx, sb.ID); err == nil {
		t.Fatal("Get after Destroy should fail")
	}
}

func TestGatewayClientHTTPErrors(t *testing.T) {
	cli, _ := newGatewayFake(t, "need-auth")
	// Wrong client key → 401.
	bad := NewGatewayClient(cli.BaseURL(), "wrong")
	if _, err := bad.Create(context.Background(), GWCreateRequest{}); err == nil {
		t.Fatal("expected auth error")
	}
	if _, err := bad.Get(context.Background(), "missing"); err == nil {
		t.Fatal("expected get error")
	}
}

func TestGatewayClientLogs(t *testing.T) {
	cli, fg := newGatewayFake(t, "")
	ctx := context.Background()
	sb, err := cli.Create(ctx, GWCreateRequest{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	fg.mu.Lock()
	fg.recs[sb.ID]["logs"] = "line1\nline2"
	fg.mu.Unlock()

	out, err := cli.Logs(ctx, sb.ID, 100)
	if err != nil || out != "line1\nline2" {
		t.Fatalf("Logs: %q err=%v", out, err)
	}
	// Empty content is success.
	fg.mu.Lock()
	fg.recs[sb.ID]["logs"] = ""
	fg.mu.Unlock()
	out, err = cli.Logs(ctx, sb.ID, 0)
	if err != nil || out != "" {
		t.Fatalf("empty Logs: %q err=%v", out, err)
	}
	if _, err := cli.Logs(ctx, "missing", 10); err == nil {
		t.Fatal("missing sandbox logs should error")
	}
}

func TestGatewayWaitRunning(t *testing.T) {
	cli, fg := newGatewayFake(t, "")
	ctx := context.Background()
	sb, err := cli.Create(ctx, GWCreateRequest{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Promote to running with a session endpoint after a short delay so the
	// poll path is exercised.
	go func() {
		time.Sleep(50 * time.Millisecond)
		fg.mu.Lock()
		rec := fg.recs[sb.ID]
		rec["status"] = "running"
		rec["endpoints"] = map[string]string{"session": "10.0.0.2:8765"}
		fg.mu.Unlock()
	}()

	got, err := cli.WaitRunning(ctx, sb.ID, 2*time.Second)
	if err != nil {
		t.Fatalf("WaitRunning: %v", err)
	}
	if got.Endpoint("session") != "10.0.0.2:8765" {
		t.Errorf("session endpoint = %q", got.Endpoint("session"))
	}
	h, p := hostPortOf(got.Endpoint("session"))
	if h != "10.0.0.2" || p != 8765 {
		t.Errorf("hostPortOf = %q %d", h, p)
	}
}

func TestGatewayWaitRunningErrorStatus(t *testing.T) {
	cli, fg := newGatewayFake(t, "")
	sb, err := cli.Create(context.Background(), GWCreateRequest{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	fg.mu.Lock()
	fg.recs[sb.ID]["status"] = "error"
	fg.recs[sb.ID]["error"] = "image pull failed"
	fg.mu.Unlock()

	_, err = cli.WaitRunning(context.Background(), sb.ID, time.Second)
	if err == nil || !strings.Contains(err.Error(), "image pull failed") {
		t.Fatalf("expected error status, got %v", err)
	}
}

func TestGatewayWaitRunningTimeout(t *testing.T) {
	cli, _ := newGatewayFake(t, "")
	sb, err := cli.Create(context.Background(), GWCreateRequest{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = cli.WaitRunning(context.Background(), sb.ID, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected timeout, got %v", err)
	}
}
