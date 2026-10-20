// Package sandboxtest provides an in-memory fake of the sandbox-gateway REST
// control plane for tests. It replaces the old docker-CLI stub: a test builds a
// FakeGateway, hands its Client() to sandbox.NewManager, and drives sandbox
// statuses/endpoints without a real gateway or Docker. Data-plane (SSH exec/
// files) is stubbed separately via sandbox.SetExecHook.
package sandboxtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
)

const managedLabel = "grasp.managed"

// record is one sandbox the fake gateway knows about.
type record struct {
	id        string
	status    string // gateway vocab: "running"/"stopped"/"pending"/"error"
	image     string
	labels    map[string]string
	env       map[string]string
	endpoints map[string]string
	logs      string // canned PID1 stdout/stderr for GET …/logs
}

// FakeGateway is an httptest-backed stand-in for the sandbox-gateway. Zero
// value is not usable; construct with New.
type FakeGateway struct {
	Server *httptest.Server

	mu   sync.Mutex
	recs map[string]*record
	seq  int

	// Tunables (set before the calls they affect).
	ACPPort    int  // session endpoint port for created/seeded sandboxes (0 -> 34567)
	SSHPort    int  // ssh endpoint port (0 -> 2222)
	FailCreate bool // POST /sandboxes returns 500
	FailList   bool // GET /sandboxes returns 500
	FailGet    bool // GET /sandboxes/:id returns 500
	FailLogs   bool // GET /sandboxes/:id/logs returns 500
	// FailDestroy makes DELETE /sandboxes/:id return 500 and keep the sandbox.
	FailDestroy bool

	ListCalls int // number of GET /sandboxes (list) calls served
}

// New starts a fake gateway and registers cleanup on the test.
func New(t *testing.T) *FakeGateway {
	t.Helper()
	fg := &FakeGateway{recs: map[string]*record{}}
	fg.Server = httptest.NewServer(http.HandlerFunc(fg.handle))
	t.Cleanup(fg.Server.Close)
	return fg
}

// Client returns a gateway client wired to this fake (no auth).
func (fg *FakeGateway) Client() *sandbox.GatewayClient {
	return sandbox.NewGatewayClient(fg.Server.URL, "")
}

// LastCreateEnv returns a copy of the most recently created sandbox env.
func (fg *FakeGateway) LastCreateEnv() map[string]string {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	var latest *record
	for _, rec := range fg.recs {
		if latest == nil || rec.id > latest.id {
			latest = rec
		}
	}
	if latest == nil {
		return nil
	}
	out := make(map[string]string, len(latest.env))
	for k, v := range latest.env {
		out[k] = v
	}
	return out
}

func (fg *FakeGateway) acpPort() int {
	if fg.ACPPort > 0 {
		return fg.ACPPort
	}
	return 34567
}

func (fg *FakeGateway) sshPort() int {
	if fg.SSHPort > 0 {
		return fg.SSHPort
	}
	return 2222
}

func (fg *FakeGateway) endpointsFor() map[string]string {
	return map[string]string{
		"session": fmt.Sprintf("127.0.0.1:%d", fg.acpPort()),
		"ide":     fmt.Sprintf("127.0.0.1:%d", fg.acpPort()+1),
		"ssh":     fmt.Sprintf("127.0.0.1:%d", fg.sshPort()),
	}
}

// SetStatus registers/updates a sandbox by id with a gateway-vocab status
// ("running"/"stopped"/…). Ids registered this way appear in List (with the
// grasp.managed label) and answer Get/status, so a test can pre-seed DB
// rows whose Name is the sandbox id. Empty status removes the record.
func (fg *FakeGateway) SetStatus(id, status string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if status == "" {
		delete(fg.recs, id)
		return
	}
	r := fg.recs[id]
	if r == nil {
		r = &record{id: id, labels: map[string]string{managedLabel: "1"}, endpoints: fg.endpointsFor()}
		fg.recs[id] = r
	}
	r.status = status
}

// SetLabel sets a single label on an existing seeded sandbox (no-op if missing).
func (fg *FakeGateway) SetLabel(id, key, value string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	rec := fg.recs[id]
	if rec == nil {
		return
	}
	if rec.labels == nil {
		rec.labels = map[string]string{}
	}
	rec.labels[key] = value
}

// Has reports whether the fake still knows about id (not yet Destroyed).
func (fg *FakeGateway) Has(id string) bool {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	return fg.recs[id] != nil
}

// Seed registers a running sandbox by id (convenience for SetStatus(id,"running")).
func (fg *FakeGateway) Seed(id string) { fg.SetStatus(id, "running") }

// SetLogs sets canned PID1 logs returned by GET /sandboxes/:id/logs.
func (fg *FakeGateway) SetLogs(id, content string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	if rec := fg.recs[id]; rec != nil {
		rec.logs = content
	}
}

// SetEndpoints overrides the published endpoints map for a seeded sandbox.
// Used by proxy tests to point ide/session at a non-loopback (or test upstream)
// host:port without changing Create defaults.
func (fg *FakeGateway) SetEndpoints(id string, endpoints map[string]string) {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	rec := fg.recs[id]
	if rec == nil {
		return
	}
	cp := make(map[string]string, len(endpoints))
	for k, v := range endpoints {
		cp[k] = v
	}
	rec.endpoints = cp
}

func (fg *FakeGateway) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/sandboxes"), "/"), "/")
	// parts: [] for collection, [id], [id, "status"], [id, "stop"|"start"],
	// [id, "hosts", port].
	switch {
	case r.URL.Path == "/api/v1/sandboxes" && r.Method == http.MethodPost:
		fg.create(w, r)
	case r.URL.Path == "/api/v1/sandboxes" && r.Method == http.MethodGet:
		fg.list(w)
	case len(parts) == 1 && parts[0] != "" && r.Method == http.MethodGet:
		fg.get(w, parts[0])
	case len(parts) == 1 && parts[0] != "" && r.Method == http.MethodDelete:
		fg.delete(w, parts[0])
	case len(parts) == 2 && parts[1] == "status" && r.Method == http.MethodGet:
		fg.status(w, parts[0])
	case len(parts) == 2 && (parts[1] == "stop" || parts[1] == "start") && r.Method == http.MethodPost:
		fg.stopStart(w, parts[0], parts[1])
	case len(parts) == 2 && parts[1] == "logs" && r.Method == http.MethodGet:
		fg.logs(w, parts[0])
	case len(parts) == 3 && parts[1] == "hosts" && r.Method == http.MethodGet:
		fg.hostForPort(w, parts[0], parts[2])
	default:
		http.NotFound(w, r)
	}
}

func (fg *FakeGateway) create(w http.ResponseWriter, r *http.Request) {
	if fg.FailCreate {
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	var req struct {
		Image  string            `json:"image"`
		Labels map[string]string `json:"labels"`
		Env    map[string]string `json:"env"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	fg.mu.Lock()
	fg.seq++
	id := fmt.Sprintf("gw-sb-%03d", fg.seq)
	labels := map[string]string{managedLabel: "1"}
	for k, v := range req.Labels {
		labels[k] = v
	}
	rec := &record{
		id: id, status: "running", image: req.Image, labels: labels,
		env: req.Env, endpoints: fg.endpointsFor(),
	}
	fg.recs[id] = rec
	fg.mu.Unlock()
	writeJSON(w, fg.dto(rec))
}

func (fg *FakeGateway) list(w http.ResponseWriter) {
	fg.mu.Lock()
	fg.ListCalls++
	if fg.FailList {
		fg.mu.Unlock()
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	out := struct {
		Sandboxes []map[string]any `json:"sandboxes"`
	}{}
	for _, rec := range fg.recs {
		out.Sandboxes = append(out.Sandboxes, fg.dto(rec))
	}
	fg.mu.Unlock()
	writeJSON(w, out)
}

func (fg *FakeGateway) get(w http.ResponseWriter, id string) {
	if fg.FailGet {
		http.Error(w, "get failed", http.StatusInternalServerError)
		return
	}
	fg.mu.Lock()
	rec := fg.recs[id]
	fg.mu.Unlock()
	if rec == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, fg.dto(rec))
}

func (fg *FakeGateway) delete(w http.ResponseWriter, id string) {
	fg.mu.Lock()
	if fg.FailDestroy {
		fg.mu.Unlock()
		http.Error(w, "destroy failed", http.StatusInternalServerError)
		return
	}
	delete(fg.recs, id)
	fg.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (fg *FakeGateway) status(w http.ResponseWriter, id string) {
	fg.mu.Lock()
	rec := fg.recs[id]
	fg.mu.Unlock()
	if rec == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"status": rec.status})
}

func (fg *FakeGateway) stopStart(w http.ResponseWriter, id, action string) {
	fg.mu.Lock()
	if rec := fg.recs[id]; rec != nil {
		if action == "stop" {
			rec.status = "stopped"
		} else {
			rec.status = "running"
		}
	}
	fg.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (fg *FakeGateway) hostForPort(w http.ResponseWriter, id, port string) {
	fg.mu.Lock()
	rec := fg.recs[id]
	fg.mu.Unlock()
	if rec == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"address": "127.0.0.1:" + port})
}

func (fg *FakeGateway) logs(w http.ResponseWriter, id string) {
	if fg.FailLogs {
		http.Error(w, "logs failed", http.StatusInternalServerError)
		return
	}
	fg.mu.Lock()
	rec := fg.recs[id]
	fg.mu.Unlock()
	if rec == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"content": rec.logs})
}

func (fg *FakeGateway) dto(rec *record) map[string]any {
	return map[string]any{
		"id":        rec.id,
		"name":      rec.id,
		"status":    rec.status,
		"image":     rec.image,
		"endpoints": rec.endpoints,
		"labels":    rec.labels,
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
