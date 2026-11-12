package e2b

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
)

// lifecycleProvider exercises the real HTTP client, envd upload/Connect framing,
// authenticated relay control and loopback listener implementation. Only the
// provider and guest service responses are fixtures; no provider API is called.
type lifecycleProvider struct {
	getRequests                                                                                                                 int
	mu                                                                                                                          sync.Mutex
	server                                                                                                                      *httptest.Server
	sandboxes                                                                                                                   map[string]*sandboxWire
	files                                                                                                                       map[string][]byte
	relayKeys                                                                                                                   map[string]string
	env                                                                                                                         map[string]string
	command                                                                                                                     string
	created, deleted, connected, paused, renewed                                                                                int
	connectFailures                                                                                                             int
	publishFailures                                                                                                             int
	createStatus, getStatus, deleteStatus, uploadStatus, processStatus, pauseStatus, connectStatus, healthStatus, publishStatus int
	ready                                                                                                                       bool
	capacity                                                                                                                    int64
	marker                                                                                                                      string
	bootstrapExit                                                                                                               *int
	healthSeen                                                                                                                  chan struct{}
	resourceMismatch                                                                                                            bool
	partialCreate                                                                                                               bool
	logs                                                                                                                        string
	beforeRequest                                                                                                               func(*http.Request)
	// maxEndAt caps renewals like E2B's per-team maximum sandbox length.
	maxEndAt  time.Time
	autoPause bool
}

func newLifecycleProvider(t *testing.T) *lifecycleProvider {
	t.Helper()
	f := &lifecycleProvider{sandboxes: map[string]*sandboxWire{}, files: map[string][]byte{}, relayKeys: map[string]string{}, ready: true, capacity: 32 << 30, healthSeen: make(chan struct{}, 1)}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *lifecycleProvider) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.beforeRequest != nil {
		f.beforeRequest(r)
	}
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/sandboxes" && r.Method == http.MethodPost {
		var request struct {
			TemplateID string            `json:"templateID"`
			Metadata   map[string]string `json:"metadata"`
			EnvVars    map[string]string `json:"envVars"`
			AutoPause  bool              `json:"autoPause"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		f.created++
		f.autoPause = request.AutoPause
		id := fmt.Sprintf("guest-%d", f.created)
		s := &sandboxWire{ID: id, TemplateID: request.TemplateID, EnvdVersion: "0.4.6", EnvdAccessToken: "envd-secret", TrafficAccessToken: "traffic-secret", Metadata: request.Metadata, CPUCount: 2, MemoryMB: 3072, State: "running", StartedAt: time.Now().UTC()}
		f.sandboxes[id] = s
		f.env = request.EnvVars
		if f.createStatus != 0 {
			w.WriteHeader(f.createStatus)
			return
		}
		response := *s
		if f.partialCreate {
			response.TrafficAccessToken = ""
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(response)
		return
	}
	if r.URL.Path == "/v2/sandboxes" {
		var all []sandboxWire
		for _, s := range f.sandboxes {
			all = append(all, *s)
		}
		_ = json.NewEncoder(w).Encode(all)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/sandboxes/") {
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/sandboxes/"), "/")
		s := f.sandboxes[parts[0]]
		if s == nil {
			w.WriteHeader(404)
			return
		}
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				f.getRequests++
				if f.getStatus != 0 {
					w.WriteHeader(f.getStatus)
					return
				}
				response := *s
				response.EnvdAccessToken = ""
				response.TrafficAccessToken = ""
				if f.resourceMismatch {
					response.MemoryMB = 512
				}
				_ = json.NewEncoder(w).Encode(response)
			case http.MethodDelete:
				if f.deleteStatus != 0 {
					w.WriteHeader(f.deleteStatus)
					return
				}
				delete(f.sandboxes, s.ID)
				f.deleted++
				w.WriteHeader(204)
			default:
				w.WriteHeader(405)
			}
			return
		}
		switch parts[1] {
		case "pause":
			if f.pauseStatus != 0 {
				w.WriteHeader(f.pauseStatus)
				return
			}
			s.State = "paused"
			f.paused++
			w.WriteHeader(204)
		case "connect":
			if f.connectFailures > 0 {
				f.connectFailures--
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if f.connectStatus != 0 {
				w.WriteHeader(f.connectStatus)
				return
			}
			s.State = "running"
			s.TrafficAccessToken = "resumed-traffic-secret"
			s.EnvdAccessToken = "resumed-envd-secret"
			f.connected++
			_ = json.NewEncoder(w).Encode(s)
		case "timeout":
			var body struct {
				Timeout int `json:"timeout"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			end := time.Now().UTC().Add(time.Duration(body.Timeout) * time.Second)
			if !f.maxEndAt.IsZero() && end.After(f.maxEndAt) {
				end = f.maxEndAt
			}
			s.EndAt = end
			f.renewed++
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
		return
	}
	s := f.sandboxes[r.Header.Get("E2b-Sandbox-Id")]
	if s == nil || r.Header.Get("e2b-traffic-access-token") != s.TrafficAccessToken {
		w.WriteHeader(403)
		return
	}
	if r.URL.Path == "/files" {
		if f.uploadStatus != 0 {
			w.WriteHeader(f.uploadStatus)
			return
		}
		if r.Header.Get("E2b-Sandbox-Port") != "49983" || r.Header.Get("X-Access-Token") != s.EnvdAccessToken {
			w.WriteHeader(403)
			return
		}
		reader, err := r.MultipartReader()
		if err != nil {
			w.WriteHeader(400)
			return
		}
		part, err := reader.NextPart()
		if err != nil {
			w.WriteHeader(400)
			return
		}
		data, err := io.ReadAll(part)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		path := r.URL.Query().Get("path")
		f.files[path] = data
		if strings.HasSuffix(path, "/relay.json") || strings.HasSuffix(path, "-relay.json") {
			var config struct{ Secret string }
			_ = json.Unmarshal(data, &config)
			f.relayKeys[s.ID] = config.Secret
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{{"path": path, "type": "file"}})
		return
	}
	if r.URL.Path == "/process.Process/Start" {
		if f.processStatus != 0 {
			w.WriteHeader(f.processStatus)
			return
		}
		var header [5]byte
		if _, err := io.ReadFull(r.Body, header[:]); err != nil {
			w.WriteHeader(400)
			return
		}
		payload := make([]byte, binary.BigEndian.Uint32(header[1:]))
		if _, err := io.ReadFull(r.Body, payload); err != nil {
			w.WriteHeader(400)
			return
		}
		var body struct {
			Process struct {
				Args []string `json:"args"`
			} `json:"process"`
		}
		if json.Unmarshal(payload, &body) != nil || len(body.Process.Args) != 3 {
			w.WriteHeader(400)
			return
		}
		f.command = body.Process.Args[2]
		w.Header().Set("Content-Type", "application/connect+json")
		_, _ = w.Write(processTestFrame(0, `{"event":{"start":{"pid":321}}}`))
		return
	}
	if r.Header.Get("E2b-Sandbox-Port") != "8081" {
		w.WriteHeader(403)
		return
	}
	if r.Header.Get("X-Grasp-Relay-Key") != f.relayKeys[s.ID] {
		w.WriteHeader(403)
		return
	}
	switch r.URL.Path {
	case "/_bridge/health":
		select {
		case f.healthSeen <- struct{}{}:
		default:
		}
		if f.healthStatus != 0 {
			w.WriteHeader(f.healthStatus)
			return
		}
		marker := s.Metadata[idLabel]
		if f.marker != "" {
			marker = f.marker
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"marker": marker, "bootstrapExit": f.bootstrapExit, "ports": map[string]bool{"22": f.ready, "8765": f.ready, "8744": f.ready}, "workspace": map[string]int64{"availableBytes": f.capacity}})
	case "/_bridge/ports":
		if f.publishFailures > 0 {
			f.publishFailures--
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if f.publishStatus != 0 {
			w.WriteHeader(f.publishStatus)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	case "/_bridge/logs":
		_ = json.NewEncoder(w).Encode(map[string]string{"content": f.logs})
	default:
		w.WriteHeader(404)
	}
}

func lifecycleOptions(t *testing.T, f *lifecycleProvider) Options {
	t.Helper()
	return Options{ClientOptions: ClientOptions{APIURL: f.server.URL, SandboxURL: f.server.URL, APIKey: "api-secret"}, OwnerID: "test-owner", StateDir: t.TempDir(), Profiles: []TemplateProfile{{Image: "prepared-image", TemplateID: "template-one", CPUCores: 2, MemoryMB: 3072, DiskGi: 20}}, BootTimeout: time.Second, Lease: time.Hour, RenewInterval: 10 * time.Minute}
}

func lifecycleSpec(id string) driver.Spec {
	return driver.Spec{ID: id, Image: "prepared-image", Resources: driver.Resources{CPUCores: 2, MemoryMB: 3072, DiskGi: 20}, Env: map[string]string{"GRASP_RUNTIME_URL": "http://runtime.invalid/bundle.tgz", "ROOT_PASSWORD": "workspace-password", "PREVIEW_DIRECT": "1", "PREVIEW_PORT": "18080"}, Ports: []int{22, 8765, 8744, 18080}, InternalPorts: []int{9222, 6080}, WorkspaceDir: "/root/workspace", Labels: map[string]string{"test": "lifecycle"}}
}

func openLifecycleDriver(t *testing.T, opts Options) *Driver {
	t.Helper()
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d
}

func TestDriverLifecyclePrivateStateAndStableRestart(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	spec := lifecycleSpec("gw-one")
	spec.Config = &driver.ConfigInject{ConfigRoot: "/root/tool-config", BundleURL: "http://bundle.invalid/config", Headers: "Authorization: synthetic-config-secret"}
	h, err := d.Create(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "e2b" || h.ID != spec.ID || h.Name != "guest-1" || h.Status != driver.StatusRunning || len(h.Endpoints) != 6 {
		t.Fatalf("incorrect handle: %+v", h)
	}
	for _, endpoint := range h.Endpoints {
		if !strings.HasPrefix(endpoint, "127.0.0.1:") {
			t.Fatalf("public endpoint %s", endpoint)
		}
	}
	f.mu.Lock()
	environment := f.env
	command := f.command
	files := make(map[string][]byte)
	for p, b := range f.files {
		files[p] = b
	}
	f.mu.Unlock()
	if environment["SANDBOX_INJECT"] != spec.Config.BundleURL+"|"+spec.Config.ConfigRoot || environment["CONFIG_ROOT"] != spec.Config.ConfigRoot || environment["WORKSPACE_DIR"] != "/root/workspace" || !strings.Contains(command, "nohup python3") || strings.Contains(command, "workspace-password") {
		t.Fatal("bootstrap environment/credential separation failed")
	}
	var envFile, bootstrap string
	for path, b := range files {
		if strings.HasSuffix(path, "/env.sh") || strings.HasSuffix(path, "-env.sh") {
			envFile = string(b)
		}
		if strings.HasSuffix(path, "/bootstrap.sh") || strings.HasSuffix(path, "-bootstrap.sh") {
			bootstrap = string(b)
		}
	}
	if !strings.Contains(envFile, "workspace-password") || !strings.Contains(bootstrap, "/grasp-bootstrap.sh fetch") {
		t.Fatal("exact bootstrap did not receive requested environment")
	}
	if _, err := d.Create(t.Context(), spec); err == nil {
		t.Fatal("duplicate gateway ID created another guest")
	}
	if err := d.PublishPort(t.Context(), spec.ID, 18080); err != nil {
		t.Fatalf("idempotent preview mapping: %v", err)
	}
	if err := d.PublishPort(t.Context(), spec.ID, 5174); err != nil {
		t.Fatal(err)
	}
	e, _ := d.lookup(spec.ID)
	e.mu.Lock()
	if e.record.Targets[18080] != 17980 {
		t.Error("preview injector target lost")
	}
	localPorts := make(map[int]int)
	for p, v := range e.record.Ports {
		localPorts[p] = v
	}
	key := e.record.RelayKey
	e.mu.Unlock()
	f.mu.Lock()
	f.logs = "prefix\napi-secret envd-secret traffic-secret " + key + " workspace-password\nlast"
	f.mu.Unlock()
	logs, err := d.Logs(t.Context(), spec.ID, 2)
	if err != nil || strings.Contains(logs, "secret") || strings.Contains(logs, key) || !strings.HasSuffix(logs, "last") {
		t.Fatalf("log redaction/tail: %q, %v", logs, err)
	}
	if err := d.Stop(t.Context(), spec.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(t.Context(), spec.ID); err != nil {
		t.Fatalf("repeated Stop is not idempotent: %v", err)
	}
	f.mu.Lock()
	pauseCalls := f.paused
	f.mu.Unlock()
	if pauseCalls != 1 {
		t.Fatal("repeated Stop called provider pause twice")
	}
	if status, err := d.Status(t.Context(), spec.ID); err != nil || status != driver.StatusStopped {
		t.Fatalf("paused status %s: %v", status, err)
	}
	f.mu.Lock()
	f.connectFailures = 1
	f.publishFailures = 1
	f.mu.Unlock()
	if _, err := d.Endpoints(t.Context(), spec.ID); err == nil {
		t.Fatal("paused guest advertised endpoints")
	}
	if err := d.Start(t.Context(), spec.ID); err != nil {
		t.Fatal(err)
	}
	endpoints, err := d.Endpoints(t.Context(), spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	for p, local := range localPorts {
		_, text, _ := net.SplitHostPort(endpoints[p])
		if text != strconv.Itoa(local) {
			t.Fatalf("resume changed endpoint for%d", p)
		}
	}
	if err := d.Start(t.Context(), spec.ID); err != nil {
		t.Fatalf("running Start is not idempotent: %v", err)
	}
	f.mu.Lock()
	connectCalls := f.connected
	f.mu.Unlock()
	if connectCalls != 1 {
		t.Fatal("running Start called provider Connect")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if f.deleted != 0 {
		t.Error("gateway Close destroyed provider guest")
	}
	f.mu.Unlock()
	f.mu.Lock()
	f.publishFailures = 1
	f.mu.Unlock()
	d2 := openLifecycleDriver(t, opts)
	restored, err := d2.Endpoints(t.Context(), spec.ID)
	if err != nil || !reflect.DeepEqual(restored, endpoints) {
		t.Fatalf("restart changed persisted endpoints: %v", err)
	}
	listed, err := d2.List(t.Context())
	if err != nil || len(listed) != 1 {
		t.Fatalf("owned list: %v %v", listed, err)
	}
	d2.sweep(t.Context())
	f.mu.Lock()
	renewed := f.renewed
	f.mu.Unlock()
	if renewed < 1 {
		t.Fatal("running lease was not renewed")
	}
	if err := d2.Destroy(t.Context(), spec.ID); err != nil {
		t.Fatal(err)
	}
	if err := d2.Destroy(t.Context(), spec.ID); err != nil {
		t.Fatal("destroy is not idempotent")
	}
	if _, err := os.Stat(d2.statePath(spec.ID)); !os.IsNotExist(err) {
		t.Fatal("destroy retained private record")
	}
	if status, err := d2.Status(t.Context(), spec.ID); err != nil || status != driver.StatusNotFound {
		t.Fatalf("destroyed status %s: %v", status, err)
	}
}

func TestDriverRejectsUnsupportedInputsBeforeProviderCreate(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	for name, mutate := range map[string]func(*driver.Spec){
		"invalid-id": func(s *driver.Spec) { s.ID = "../bad" }, "image": func(s *driver.Spec) { s.Image = "other" }, "cpu": func(s *driver.Spec) { s.Resources.CPUCores = 1 }, "memory": func(s *driver.Spec) { s.Resources.MemoryMB = 4096 }, "disk": func(s *driver.Spec) { s.Resources.DiskGi = 19 },
		"mount": func(s *driver.Spec) { s.Mounts = []string{"/host:/guest"} }, "config-path": func(s *driver.Spec) { s.Config = &driver.ConfigInject{HostPath: "/host"} }, "runtime": func(s *driver.Spec) { delete(s.Env, "GRASP_RUNTIME_URL") }, "env-key": func(s *driver.Spec) { s.Env["bad-key"] = "x" }, "env-nul": func(s *driver.Spec) { s.Env["KEY"] = "bad\x00value" }, "management-port": func(s *driver.Spec) { s.Ports = []int{49983} },
	} {
		t.Run(name, func(t *testing.T) {
			spec := lifecycleSpec("invalid")
			mutate(&spec)
			if _, err := d.Create(t.Context(), spec); err == nil {
				t.Fatal("unsupported input accepted")
			}
		})
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.created != 0 {
		t.Fatal("invalid input reached provider")
	}
}

func TestDriverCreateFailureRetiresOnlyOwnedGuests(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*lifecycleProvider)
	}{
		{"ambiguous-create", func(f *lifecycleProvider) { f.createStatus = 500 }},
		{"missing-private-token", func(f *lifecycleProvider) { f.partialCreate = true }},
		{"wrong-resources", func(f *lifecycleProvider) { f.resourceMismatch = true }},
		{"upload", func(f *lifecycleProvider) { f.uploadStatus = 500 }},
		{"process", func(f *lifecycleProvider) { f.processStatus = 500 }},
		{"health-identity", func(f *lifecycleProvider) { f.marker = "different-gateway" }},
		{"bootstrap-exit", func(f *lifecycleProvider) { code := 7; f.bootstrapExit = &code }},
		{"disk-capacity", func(f *lifecycleProvider) { f.capacity = 19 << 30 }},
		{"publish", func(f *lifecycleProvider) { f.publishStatus = 500 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLifecycleProvider(t)
			test.configure(f)
			f.sandboxes["foreign"] = &sandboxWire{ID: "foreign", Metadata: map[string]string{ownerLabel: "other", idLabel: "gw-fail", driverLabel: "e2b"}}
			d := openLifecycleDriver(t, lifecycleOptions(t, f))
			if _, err := d.Create(t.Context(), lifecycleSpec("gw-fail")); err == nil {
				t.Fatal("failed provisioning succeeded")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.sandboxes) != 1 || f.sandboxes["foreign"] == nil || f.deleted != 1 {
				t.Fatalf("owned failure cleanup wrong: count%d deleted%d", len(f.sandboxes), f.deleted)
			}
		})
	}
}

func TestDriverCancelCreateAndCleanupRetry(t *testing.T) {
	f := newLifecycleProvider(t)
	f.ready = false
	opts := lifecycleOptions(t, f)
	opts.BootTimeout = 5 * time.Second
	d := openLifecycleDriver(t, opts)
	created := make(chan error, 1)
	go func() { _, err := d.Create(context.Background(), lifecycleSpec("cancelled")); created <- err }()
	select {
	case <-f.healthSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("bootstrap did not reach real relay control")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := d.Destroy(ctx, "cancelled"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-created:
		if err == nil {
			t.Fatal("cancelled create succeeded")
		}
	case <-ctx.Done():
		t.Fatal("Destroy could not cancel blocked bootstrap")
	}
	f.mu.Lock()
	f.ready = true
	f.mu.Unlock()
	if _, err := d.Create(t.Context(), lifecycleSpec("retry")); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.deleteStatus = 503
	f.mu.Unlock()
	if err := d.Destroy(t.Context(), "retry"); err == nil {
		t.Fatal("provider delete failure was hidden")
	}
	if _, err := os.Stat(d.statePath("retry")); err != nil {
		t.Fatal("failed delete lost recovery record")
	}
	if status, err := d.Status(t.Context(), "retry"); err != nil || status != driver.StatusNotFound {
		t.Fatal("deleting guest remained publicly running")
	}
	if err := d.Stop(t.Context(), "retry"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleting guest accepted pause")
	}
	f.mu.Lock()
	f.deleteStatus = 0
	f.mu.Unlock()
	d.sweep(t.Context())
	if _, err := d.lookup("retry"); !errors.Is(err, ErrNotFound) {
		t.Fatal("renewal sweep did not retry owned deletion")
	}
}

func TestDriverOwnershipExpiryAndStoppedRestart(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	if _, err := d.Create(t.Context(), lifecycleSpec("owned")); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.sandboxes["guest-1"].Metadata[ownerLabel] = "foreign"
	f.mu.Unlock()
	if _, err := d.Get(t.Context(), "owned"); err == nil {
		t.Fatal("Get trusted foreign ownership")
	}
	if err := d.Destroy(t.Context(), "owned"); err == nil {
		t.Fatal("Destroy deleted foreign guest")
	}
	f.mu.Lock()
	if f.deleted != 0 {
		t.Error("foreign provider deleted")
	}
	f.sandboxes["guest-1"].Metadata[ownerLabel] = opts.OwnerID
	f.mu.Unlock()
	if err := d.Destroy(t.Context(), "owned"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create(t.Context(), lifecycleSpec("paused")); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(t.Context(), "paused"); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d2 := openLifecycleDriver(t, opts)
	f.mu.Lock()
	connected := f.connected
	beforeRenewed := f.renewed
	f.mu.Unlock()
	if connected != 0 {
		t.Fatal("gateway restart implicitly resumed a paused guest")
	}
	d2.sweep(t.Context())
	f.mu.Lock()
	renewed := f.renewed
	f.mu.Unlock()
	if renewed != beforeRenewed {
		t.Fatal("paused guest lease was renewed")
	}
	if err := d2.Start(t.Context(), "paused"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	delete(f.sandboxes, "guest-2")
	f.mu.Unlock()
	if status, err := d2.Status(t.Context(), "paused"); err != nil || status != driver.StatusNotFound {
		t.Fatalf("expired provider status: %s %v", status, err)
	}
	listed, err := d2.List(t.Context())
	if err != nil || len(listed) != 0 {
		t.Fatal("expired guest remained listed")
	}
	if err := d2.Destroy(t.Context(), "paused"); err != nil {
		t.Fatal(err)
	}
}

func TestDriverRestartRefusesUnavailablePreferredEndpoint(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	h, err := d.Create(t.Context(), lifecycleSpec("restart"))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	occupied, err := net.Listen("tcp4", h.Endpoints[22])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = occupied.Close() }()
	if recovered, err := New(opts); err == nil {
		_ = recovered.Close()
		t.Fatal("restart silently changed required local endpoint")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleted != 0 || f.connected != 0 {
		t.Fatal("failed restoration destroyed or resumed provider guest")
	}
}

func TestDriverReinstallAndErrorResponses(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	spec := lifecycleSpec("reinstall")
	if _, err := d.Create(t.Context(), spec); err != nil {
		t.Fatal(err)
	}
	if err := d.Reinstall(t.Context(), spec, true); err == nil {
		t.Fatal("data-preserving reinstall silently discarded data")
	}
	invalid := spec
	invalid.Resources.MemoryMB = 512
	if err := d.Reinstall(t.Context(), invalid, false); err == nil {
		t.Fatal("unsupported profile accepted")
	}
	if err := d.Reinstall(t.Context(), spec, false); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	count := f.created
	f.getStatus = 503
	f.mu.Unlock()
	if count != 2 {
		t.Fatal("reinstall did not replace provider guest")
	}
	if status, err := d.Status(t.Context(), spec.ID); err == nil || status != driver.StatusError {
		t.Fatal("provider status error hidden")
	}
	if _, err := d.Endpoints(t.Context(), spec.ID); err == nil {
		t.Fatal("provider error advertised endpoints")
	}
	if _, err := d.List(t.Context()); err == nil {
		t.Fatal("provider list error hidden")
	}
	f.mu.Lock()
	f.getStatus = 0
	f.pauseStatus = 503
	f.mu.Unlock()
	if err := d.Stop(t.Context(), spec.ID); err == nil {
		t.Fatal("pause error hidden")
	}
	f.mu.Lock()
	f.pauseStatus = 0
	f.mu.Unlock()
	if err := d.Stop(t.Context(), spec.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.PublishPort(t.Context(), spec.ID, 5174); err == nil {
		t.Fatal("paused guest accepted a new endpoint")
	}
	f.mu.Lock()
	f.connectStatus = 503
	f.mu.Unlock()
	if err := d.Start(t.Context(), spec.ID); err == nil {
		t.Fatal("connect error hidden")
	}
	for _, operation := range []func() error{func() error { return d.Stop(t.Context(), "missing") }, func() error { return d.Start(t.Context(), "missing") }, func() error { return d.PublishPort(t.Context(), "missing", 5174) }} {
		if !errors.Is(operation(), ErrNotFound) {
			t.Fatal("unknown guest lookup did not fail closed")
		}
	}
	if _, err := d.Logs(t.Context(), "missing", 0); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown guest logs did not fail closed")
	}
	if err := d.PublishPort(t.Context(), spec.ID, 8081); err == nil {
		t.Fatal("published relay management port")
	}
}

func TestDriverOptionValidationAndShellEnvironment(t *testing.T) {
	f := newLifecycleProvider(t)
	for _, change := range []func(*Options){func(o *Options) { o.OwnerID = "" }, func(o *Options) { o.StateDir = "" }, func(o *Options) { o.Profiles = nil }, func(o *Options) { o.Profiles[0].CPUCores = .5 }, func(o *Options) { o.Profiles[0].CPUCores = math.NaN() }, func(o *Options) { o.Profiles[0].CPUCores = math.Inf(1) }, func(o *Options) { o.Profiles[0].MemoryMB = 0 }, func(o *Options) { o.Profiles[0].DiskGi = 0 }, func(o *Options) { o.Profiles = append(o.Profiles, o.Profiles[0]) }, func(o *Options) { o.Lease = 10 * time.Second }, func(o *Options) { o.RenewInterval = o.Lease / 2 }, func(o *Options) { o.APIURL = "file:///bad" }, func(o *Options) { o.SandboxURL = "http://host/path" }} {
		opts := lifecycleOptions(t, f)
		change(&opts)
		if d, err := New(opts); err == nil {
			_ = d.Close()
			t.Fatal("invalid driver options accepted")
		}
	}
	text, err := shellEnvironment(map[string]string{"Z": "a'b", "_A2": "$(untouched)\nline"})
	if err != nil || text != "export Z='a'\"'\"'b'\nexport _A2='$(untouched)\nline'\n" {
		t.Fatalf("environment quoting/order: %q %v", text, err)
	}
	for _, key := range []string{"", "2BAD", "A-B", "ä"} {
		if validEnvKey(key) {
			t.Fatalf("invalid environment key accepted: %q", key)
		}
	}
	if _, err := shellEnvironment(map[string]string{"SAFE": "\x00"}); err == nil {
		t.Fatal("NUL environment accepted")
	}
	for _, ref := range []string{"template-one", "team/template:v1", "team/stable.alias"} {
		if !validTemplateRef(ref) {
			t.Fatalf("official template reference rejected: %s", ref)
		}
	}
	for _, ref := range []string{"", "bad ref", "bad?token", strings.Repeat("a", 257)} {
		if validTemplateRef(ref) {
			t.Fatal("invalid template reference accepted")
		}
	}
}

func TestDriverPauseAndResumeCrashWindowsRecover(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	d := openLifecycleDriver(t, opts)
	if _, err := d.Create(t.Context(), lifecycleSpec("crash-window")); err != nil {
		t.Fatal(err)
	}
	// Provider pause succeeded, but the gateway stopped before persisting its
	// phase. Restoration must adopt the stopped state without auto-resuming.
	f.mu.Lock()
	f.sandboxes["guest-1"].State = "paused"
	f.mu.Unlock()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d2 := openLifecycleDriver(t, opts)
	if err := d2.Start(t.Context(), "crash-window"); err != nil {
		t.Fatalf("provider-paused durable-running guest cannot be resumed: %v", err)
	}
	if err := d2.Stop(t.Context(), "crash-window"); err != nil {
		t.Fatal(err)
	}
	// Conversely, a provider resume can succeed just before the phase commit.
	// Fresh credentials are already persisted; Get should recover. Existing
	// guests may have consumed storage since creation, so capacity is not reset.
	f.mu.Lock()
	f.sandboxes["guest-1"].State = "running"
	f.capacity = 0
	f.mu.Unlock()
	if h, err := d2.Get(t.Context(), "crash-window"); err != nil || h.Status != driver.StatusRunning {
		t.Fatalf("uncertain resume did not recover: %v", err)
	}
	e, _ := d2.lookup("crash-window")
	e.mu.Lock()
	phase := e.record.Phase
	e.mu.Unlock()
	if phase != "running" {
		t.Fatal("recovered running phase was not committed")
	}
	if err := d2.Close(); err != nil {
		t.Fatal(err)
	}
	d3 := openLifecycleDriver(t, opts)
	if err := d3.Destroy(t.Context(), "crash-window"); err != nil {
		t.Fatal(err)
	}
}

func TestDriverHealthRetriesDeadlineAndBootstrapSuccess(t *testing.T) {
	t.Run("transient-control-error", func(t *testing.T) {
		f := newLifecycleProvider(t)
		f.healthStatus = 503
		d := openLifecycleDriver(t, lifecycleOptions(t, f))
		result := make(chan error, 1)
		go func() { _, err := d.Create(context.Background(), lifecycleSpec("retry-health")); result <- err }()
		select {
		case <-f.healthSeen:
		case <-time.After(time.Second):
			t.Fatal("no relay readiness request")
		}
		f.mu.Lock()
		f.healthStatus = 0
		code := 0
		f.bootstrapExit = &code
		f.mu.Unlock()
		select {
		case err := <-result:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("readiness did not retry")
		}
	})
	t.Run("bounded-deadline", func(t *testing.T) {
		f := newLifecycleProvider(t)
		f.ready = false
		opts := lifecycleOptions(t, f)
		opts.BootTimeout = 40 * time.Millisecond
		d := openLifecycleDriver(t, opts)
		if _, err := d.Create(t.Context(), lifecycleSpec("deadline")); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("health deadline cause lost: %v", err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		// A very short boot deadline may expire before the provider accepts the
		// create request. In that case there is no guest to clean up; when the
		// request did land, every guest accepted by this test provider must have
		// been deleted by Create's failure path.
		if f.deleted != f.created {
			t.Fatalf("deadline cleanup mismatch: created=%d deleted=%d", f.created, f.deleted)
		}
	})
}

func TestDriverDefaultsLockAndPeriodicLeaseRenewal(t *testing.T) {
	f := newLifecycleProvider(t)
	opts := lifecycleOptions(t, f)
	opts.BootTimeout = 0
	opts.Lease = 0
	opts.RenewInterval = 0
	d := openLifecycleDriver(t, opts)
	if d.opts.BootTimeout <= 0 || d.opts.Lease != time.Hour || d.opts.RenewInterval != time.Hour/3 {
		t.Fatal("driver defaults are invalid")
	}
	if second, err := New(opts); err == nil {
		_ = second.Close()
		t.Fatal("two gateways acquired the same private state")
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	opts.RenewInterval = 5 * time.Millisecond
	d2 := openLifecycleDriver(t, opts)
	if _, err := d2.Create(t.Context(), lifecycleSpec("renewal")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		f.mu.Lock()
		count := f.renewed
		f.mu.Unlock()
		if count >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("periodic lease renewal did not run")
		}
		time.Sleep(time.Millisecond)
	}
	if err := d2.Destroy(t.Context(), "renewal"); err != nil {
		t.Fatal(err)
	}
}

func TestDriverReadAndMutationFailuresDoNotChangeOwnership(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("access")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.credentials(&record{}); err == nil {
		t.Fatal("incomplete provider identity generated relay credentials")
	}
	if _, err := d.Status(t.Context(), "access"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	before := f.getRequests
	f.getStatus = 503
	f.mu.Unlock()
	if status, err := d.Status(t.Context(), "access"); err != nil || status != driver.StatusRunning {
		t.Fatal("short status cache did not preserve successful probe")
	}
	f.mu.Lock()
	after := f.getRequests
	f.mu.Unlock()
	if after != before {
		t.Fatal("cached status still queried provider")
	}
	e, _ := d.lookup("access")
	e.mu.Lock()
	e.statusAt = time.Now().Add(-3 * time.Second)
	e.mu.Unlock()
	if _, err := d.Status(t.Context(), "access"); err == nil {
		t.Fatal("expired status cache hid provider error")
	}
	if err := d.Stop(t.Context(), "access"); err == nil {
		t.Fatal("provider Get failure did not block mutation")
	}
	if err := d.PublishPort(t.Context(), "access", 5174); err == nil {
		t.Fatal("provider Get failure did not block port publication")
	}
	if _, err := d.Logs(t.Context(), "access", 0); err == nil {
		t.Fatal("provider Get failure did not block logs")
	}
	f.mu.Lock()
	f.getStatus = 0
	f.healthStatus = 503
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := d.Get(ctx, "access"); err == nil {
		t.Fatal("unhealthy relay advertised running")
	}
	if _, err := d.Logs(t.Context(), "access", 0); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.healthStatus = 0
	f.sandboxes["guest-1"].Metadata[ownerLabel] = "another-owner"
	f.mu.Unlock()
	if err := d.Stop(t.Context(), "access"); err == nil {
		t.Fatal("pause mutated foreign provider guest")
	}
	if err := d.PublishPort(t.Context(), "access", 5174); err == nil {
		t.Fatal("publication mutated foreign provider guest")
	}
	if _, err := d.Logs(t.Context(), "access", 0); err == nil {
		t.Fatal("logs accessed foreign provider guest")
	}
	e.mu.Lock()
	e.record.Phase = "paused"
	e.mu.Unlock()
	if err := d.Start(t.Context(), "access"); err == nil {
		t.Fatal("resume mutated foreign provider guest")
	}
	f.mu.Lock()
	f.sandboxes["guest-1"].Metadata[ownerLabel] = d.opts.OwnerID
	f.mu.Unlock()
	if err := d.Destroy(t.Context(), "access"); err != nil {
		t.Fatal(err)
	}
}

func TestDriverRunningStartNeverImplicitlyResumesUnexpectedPause(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	if _, err := d.Create(t.Context(), lifecycleSpec("explicit-start")); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.getStatus = 503
	f.mu.Unlock()
	if err := d.Start(t.Context(), "explicit-start"); err == nil {
		t.Fatal("idempotent Start hid an unhealthy provider")
	}
	f.mu.Lock()
	f.getStatus = 0
	f.sandboxes["guest-1"].State = "paused"
	f.mu.Unlock()
	if err := d.Start(t.Context(), "explicit-start"); err == nil {
		t.Fatal("running-phase Start accepted an unexpected provider pause")
	}
	f.mu.Lock()
	connected := f.connected
	f.mu.Unlock()
	if connected != 0 {
		t.Fatal("read-only running-phase check resumed the guest")
	}
	if err := d.Start(t.Context(), "explicit-start"); err != nil {
		t.Fatal("explicit retry could not resume the now-known paused guest")
	}
	e, _ := d.lookup("explicit-start")
	e.mu.Lock()
	e.record.Phase = "creating"
	e.mu.Unlock()
	if err := d.Start(t.Context(), "explicit-start"); err == nil {
		t.Fatal("incomplete guest accepted Start")
	}
	if err := d.Destroy(t.Context(), "explicit-start"); err != nil {
		t.Fatal(err)
	}
}

func TestDriverPreviewIngressDefaultsReachGuestAndPublishedEndpoints(t *testing.T) {
	for _, test := range []struct {
		name, enabled, raw string
		port               int
	}{
		{name: "default", enabled: "1", port: 18080},
		{name: "boolean-enabled", enabled: "true", port: 18080},
		{name: "explicit-alternative", enabled: "1", raw: "19090", port: 19090},
		{name: "normalized-alternative", enabled: "1", raw: "019090", port: 19090},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLifecycleProvider(t)
			d := openLifecycleDriver(t, lifecycleOptions(t, f))
			spec := lifecycleSpec("preview-ingress")
			spec.Env["PREVIEW_DIRECT"] = test.enabled
			delete(spec.Env, "PREVIEW_PORT")
			if test.raw != "" {
				spec.Env["PREVIEW_PORT"] = test.raw
			}
			// This is the ordinary product request: only native services have ports
			// before the E2B driver supplies its private injected preview entry.
			spec.Ports = []int{22, 8765, 8744}
			h, err := d.Create(t.Context(), spec)
			if err != nil {
				t.Fatal(err)
			}
			want := strconv.Itoa(test.port)
			f.mu.Lock()
			providerEnv := f.env["PREVIEW_PORT"]
			guestEnv := string(f.files["/run/grasp-e2b/env.sh"])
			f.mu.Unlock()
			if providerEnv != want || !strings.Contains(guestEnv, "export PREVIEW_PORT='"+want+"'\n") {
				t.Fatal("preview entry did not reach both provider and exact bootstrap environment")
			}
			e, err := d.lookup(spec.ID)
			if err != nil {
				t.Fatal(err)
			}
			e.mu.Lock()
			target := e.record.Targets[test.port]
			local := e.record.Ports[test.port]
			e.mu.Unlock()
			if target != 17980 || local <= 0 {
				t.Fatalf("preview entry has target%d/local%d, expected injector17980", target, local)
			}
			if h.Endpoints[test.port] != net.JoinHostPort("127.0.0.1", strconv.Itoa(local)) {
				t.Fatal("preview entry was not published on loopback")
			}
			if test.port != 18080 {
				if _, exists := h.Endpoints[18080]; exists {
					t.Fatal("explicit alternative also published an unwanted default entry")
				}
			}
			if err := d.PublishPort(t.Context(), spec.ID, test.port); err != nil {
				t.Fatalf("registered preview entry was not idempotent: %v", err)
			}
			if err := d.Destroy(t.Context(), spec.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDriverInvalidPreviewEntriesFailBeforeProviderCreate(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	for _, port := range []string{"not-a-port", "1.5", "-1", "0", "65536", "999999999999999999999999", "8081", "49983", "22", "8765", "8744", "9222", "6080", "17980"} {
		t.Run(port, func(t *testing.T) {
			spec := lifecycleSpec("invalid-preview")
			spec.Ports = []int{22, 8765, 8744}
			spec.Env["PREVIEW_PORT"] = port
			if _, err := d.Create(t.Context(), spec); err == nil {
				t.Fatal("invalid or reserved preview entry accepted")
			}
		})
	}
	f.mu.Lock()
	created := f.created
	f.mu.Unlock()
	if created != 0 {
		t.Fatal("invalid preview configuration reached provider create")
	}
	if len(d.entries) != 0 {
		t.Fatal("invalid preview configuration created recovery records")
	}
}

func TestDriverDisabledDirectPreviewDoesNotAddIngress(t *testing.T) {
	f := newLifecycleProvider(t)
	d := openLifecycleDriver(t, lifecycleOptions(t, f))
	spec := lifecycleSpec("no-direct-preview")
	spec.Env["PREVIEW_DIRECT"] = "0"
	delete(spec.Env, "PREVIEW_PORT")
	spec.Ports = []int{22, 8765, 8744, 5174}
	h, err := d.Create(t.Context(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := h.Endpoints[18080]; exists {
		t.Fatal("disabled direct preview added an ingress")
	}
	f.mu.Lock()
	previewPort := f.env["PREVIEW_PORT"]
	f.mu.Unlock()
	if previewPort != "" {
		t.Fatal("disabled direct preview injected PREVIEW_PORT")
	}
	e, _ := d.lookup(spec.ID)
	e.mu.Lock()
	target := e.record.Targets[5174]
	e.mu.Unlock()
	if target != 5174 {
		t.Fatal("ordinary application port was redirected to injector")
	}
	if err := d.Destroy(t.Context(), spec.ID); err != nil {
		t.Fatal(err)
	}
}
