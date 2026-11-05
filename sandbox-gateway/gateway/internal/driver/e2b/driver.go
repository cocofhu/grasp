package e2b

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"sandbox-gateway/internal/driver"

	"github.com/rs/zerolog/log"
)

const ownerLabel = "grasp.gateway.owner"
const idLabel = "grasp.gateway.id"
const driverLabel = "grasp.gateway.driver"

// TemplateProfile is an immutable, prepared E2B template. DiskGi is the
// minimum initial available workspace capacity, not a dynamically sized PVC.
type TemplateProfile struct {
	Image      string  `json:"image" yaml:"image"`
	TemplateID string  `json:"templateID" yaml:"templateID"`
	CPUCores   float64 `json:"cpuCores" yaml:"cpuCores"`
	MemoryMB   int64   `json:"memoryMB" yaml:"memoryMB"`
	DiskGi     int64   `json:"diskGi" yaml:"diskGi"`
}

type Options struct {
	ClientOptions
	OwnerID       string
	StateDir      string
	Profiles      []TemplateProfile
	BootTimeout   time.Duration
	Lease         time.Duration
	RenewInterval time.Duration
}

type Driver struct {
	opts      Options
	client    *Client
	tunnels   *TunnelManager
	mu        sync.Mutex
	entries   map[string]*entry
	cancel    context.CancelFunc
	done      chan struct{}
	lockFile  *os.File
	closeOnce sync.Once
	closeErr  error
}

var _ driver.Driver = (*Driver)(nil)

func New(opts Options) (*Driver, error) {
	if !providerIDPattern.MatchString(opts.OwnerID) || opts.StateDir == "" || len(opts.Profiles) == 0 {
		return nil, errors.New("E2B requires a stable ownerID, private stateDir and prepared template profiles")
	}
	for i, p := range opts.Profiles {
		if p.Image == "" || !validTemplateRef(p.TemplateID) || p.CPUCores <= 0 || math.IsInf(p.CPUCores, 0) || math.Trunc(p.CPUCores) != p.CPUCores || p.MemoryMB <= 0 || p.DiskGi <= 0 || p.DiskGi > 1<<30 {
			return nil, errors.New("invalid E2B prepared template profile")
		}
		for _, q := range opts.Profiles[:i] {
			if p.Image == q.Image && p.CPUCores == q.CPUCores && p.MemoryMB == q.MemoryMB && p.DiskGi == q.DiskGi {
				return nil, errors.New("duplicate E2B template profile")
			}
		}
	}
	if opts.BootTimeout <= 0 {
		opts.BootTimeout = 5 * time.Minute
	}
	if opts.Lease <= 0 {
		opts.Lease = time.Hour
	}
	if opts.RenewInterval <= 0 {
		opts.RenewInterval = opts.Lease / 3
	}
	if opts.Lease < 30*time.Second || opts.RenewInterval >= opts.Lease/2 {
		return nil, errors.New("E2B renewal must leave at least half the lease for retries")
	}
	c, err := NewClient(opts.ClientOptions)
	if err != nil {
		return nil, err
	}
	t, err := NewTunnelManager(opts.SandboxURL)
	if err != nil {
		return nil, err
	}
	d := &Driver{opts: opts, client: c, tunnels: t, entries: map[string]*entry{}, done: make(chan struct{})}
	if err = d.load(); err != nil {
		if d.lockFile != nil {
			_ = d.lockFile.Close()
		}
		_ = t.CloseAll()
		return nil, err
	}
	// Restore before serving API traffic. The exact loopback ports are part of
	// persisted Grasp records; changing them would break running sessions.
	for _, e := range d.entries {
		if e.record.Phase != "running" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), opts.BootTimeout)
		_, err = d.refresh(ctx, &e.record)
		cancel()
		if err != nil {
			_ = t.CloseAll()
			if d.lockFile != nil {
				_ = d.lockFile.Close()
			}
			return nil, fmt.Errorf("restore E2B endpoints: %w", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go d.maintain(ctx)
	return d, nil
}

func (*Driver) Name() string { return "e2b" }

// Close releases local resources only. Provider guests survive a gateway
// restart and remain bounded by their provider leases.
func (d *Driver) Close() error {
	d.closeOnce.Do(func() {
		d.cancel()
		<-d.done
		d.closeErr = d.tunnels.CloseAll()
		if d.lockFile != nil {
			if err := d.lockFile.Close(); d.closeErr == nil {
				d.closeErr = err
			}
		}
	})
	return d.closeErr
}

func (d *Driver) owns(s *Sandbox, id string) bool {
	return s.Metadata[ownerLabel] == d.opts.OwnerID && s.Metadata[idLabel] == id && s.Metadata[driverLabel] == "e2b"
}
func (d *Driver) lookup(id string) (*entry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e := d.entries[id]
	if e == nil {
		return nil, ErrNotFound
	}
	return e, nil
}
func randomKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errors.New("cannot generate E2B relay secret")
	}
	return hex.EncodeToString(b[:]), nil
}

func (d *Driver) profile(spec driver.Spec) (TemplateProfile, error) {
	for _, p := range d.opts.Profiles {
		if p.Image == spec.Image && p.CPUCores == spec.Resources.CPUCores && p.MemoryMB == spec.Resources.MemoryMB && p.DiskGi == spec.Resources.DiskGi {
			return p, nil
		}
	}
	return TemplateProfile{}, errors.New("no prepared E2B template matches image, CPU, memory and initial disk capacity")
}

func (d *Driver) Create(parent context.Context, spec driver.Spec) (result *driver.Handle, err error) {
	if !providerIDPattern.MatchString(spec.ID) {
		return nil, errors.New("invalid E2B gateway sandbox ID")
	}
	p, err := d.profile(spec)
	if err != nil {
		return nil, err
	}
	if len(spec.Mounts) > 0 || spec.Config != nil && spec.Config.HostPath != "" {
		return nil, errors.New("E2B cannot mount gateway host paths; use bundle injection")
	}
	env := make(map[string]string, len(spec.Env)+3)
	for k, v := range spec.Env {
		env[k] = v
	}
	if spec.WorkspaceDir != "" {
		env["WORKSPACE_DIR"] = spec.WorkspaceDir
	}
	if env["PATH"] == "" {
		env["PATH"] = "/root/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/go/bin:/root/go/bin"
	}
	if env["PLAYWRIGHT_BROWSERS_PATH"] == "" {
		env["PLAYWRIGHT_BROWSERS_PATH"] = "/ms-playwright"
	}
	if env["WORKSPACE_DIR"] == "" {
		env["WORKSPACE_DIR"] = "/root/workspace"
	}
	if spec.Config != nil {
		if spec.Config.ConfigRoot != "" {
			env["CONFIG_ROOT"] = spec.Config.ConfigRoot
		}
		if spec.Config.BundleURL != "" {
			env["SANDBOX_INJECT"] = spec.Config.BundleURL
			if spec.Config.ConfigRoot != "" {
				env["SANDBOX_INJECT"] += "|" + spec.Config.ConfigRoot
			}
			env["SANDBOX_INJECT_HEADERS"] = spec.Config.Headers
		}
	}
	if env["GRASP_RUNTIME_URL"] == "" {
		return nil, errors.New("E2B requires GRASP_RUNTIME_URL")
	}
	ports := append(append([]int{}, spec.Ports...), spec.InternalPorts...)
	if env["PREVIEW_DIRECT"] == "1" || env["PREVIEW_DIRECT"] == "true" {
		previewPort := driver.DefaultK8sPreviewPort
		if raw := env["PREVIEW_PORT"]; raw != "" {
			n, parseErr := strconv.Atoi(raw)
			if parseErr != nil || !validTunnelPort(n) {
				return nil, errors.New("invalid E2B preview port")
			}
			previewPort = n
		}
		switch previewPort {
		case 22, 8765, 8744, 9222, 6080, 17980:
			return nil, errors.New("E2B preview port conflicts with a sandbox service")
		}
		env["PREVIEW_PORT"] = strconv.Itoa(previewPort)
		ports = driver.AppendPort(ports, previewPort)
	}
	// Serialize after E2B has supplied its guest-local preview ingress.
	envText, err := shellEnvironment(env)
	if err != nil {
		return nil, err
	}

	for _, port := range ports {
		if !validTunnelPort(port) {
			return nil, errors.New("invalid or reserved E2B sandbox port")
		}
	}
	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, d.opts.BootTimeout)
	defer cancel()
	e := &entry{record: record{ID: spec.ID, Profile: p, Phase: "creating", RelayKey: key, CreatedAt: time.Now().UTC(), Ports: map[int]int{}, Targets: map[int]int{}}, cancel: cancel}
	for _, v := range env {
		if v != "" {
			e.record.Secrets = append(e.record.Secrets, v)
		}
	}
	for _, port := range ports {
		target := port
		if (env["PREVIEW_DIRECT"] == "1" || env["PREVIEW_DIRECT"] == "true") && strconv.Itoa(port) == env["PREVIEW_PORT"] {
			target = 17980
		}
		e.record.Targets[port] = target
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	d.mu.Lock()
	if d.entries[spec.ID] != nil {
		d.mu.Unlock()
		return nil, errors.New("E2B gateway sandbox already exists")
	}
	d.entries[spec.ID] = e
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		e.cancel = nil
		d.mu.Unlock()
		if err != nil {
			e.record.Phase = "deleting"
			_ = d.save(&e.record)
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			_ = d.cleanup(cleanupCtx, &e.record)
		}
	}()
	if err = d.save(&e.record); err != nil {
		return nil, err
	}
	metadata := map[string]string{}
	for k, v := range spec.Labels {
		metadata[k] = v
	}
	metadata[ownerLabel] = d.opts.OwnerID
	metadata[idLabel] = spec.ID
	metadata[driverLabel] = "e2b"
	e.record.UncertainUntil = time.Now().Add(d.opts.Lease + time.Minute)
	if err = d.save(&e.record); err != nil {
		return nil, err
	}
	s, createErr := d.client.Create(ctx, CreateOptions{TemplateID: p.TemplateID, TimeoutSeconds: int(d.opts.Lease / time.Second), EnvVars: env, Metadata: metadata})
	if s != nil {
		e.record.UncertainUntil = time.Time{}
		e.record.Provider = *s
		e.record.Provider.Metadata = metadata
		if saveErr := d.save(&e.record); saveErr != nil {
			return nil, saveErr
		}
	}
	if createErr != nil {
		var apiErr *APIError
		if errors.As(createErr, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
			e.record.UncertainUntil = time.Time{}
		}
		return nil, createErr
	}
	if s == nil {
		return nil, errors.New("E2B create returned no sandbox")
	}
	actual, getErr := d.client.Get(ctx, s.ID)
	if getErr != nil {
		return nil, getErr
	}
	if !d.owns(actual, spec.ID) || actual.CPUCount != int(p.CPUCores) || actual.MemoryMB != p.MemoryMB || actual.TemplateID != s.TemplateID {
		return nil, errors.New("E2B provider template resources or ownership differ from requested profile")
	}
	if err = d.provision(ctx, &e.record, envText, env["WORKSPACE_DIR"]); err != nil {
		return nil, err
	}
	if err = d.waitHealthy(ctx, &e.record, true); err != nil {
		return nil, err
	}
	if err = d.publish(ctx, &e.record); err != nil {
		return nil, err
	}
	e.record.Phase = "running"
	if err = d.save(&e.record); err != nil {
		return nil, err
	}
	return handle(&e.record, d.tunnels.List(s.ID), driver.StatusRunning), nil
}

func shellEnvironment(env map[string]string) (string, error) {
	keys := make([]string, 0, len(env))
	for k, v := range env {
		if !validEnvKey(k) || strings.ContainsRune(v, 0) {
			return "", errors.New("invalid E2B environment variable")
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, k := range keys {
		out.WriteString("export " + k + "=" + shellQuote(env[k]) + "\n")
	}
	return out.String(), nil
}
func validEnvKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func (d *Driver) credentials(r *record) (RelayCredentials, error) {
	u, _, err := d.client.SandboxEndpoint(&r.Provider, relayPort)
	if err != nil {
		return RelayCredentials{}, err
	}
	return RelayCredentials{SandboxID: r.Provider.ID, TrafficToken: r.Provider.TrafficAccessToken, RelayKey: r.RelayKey, EndpointURL: u.String()}, nil
}

func (d *Driver) provision(ctx context.Context, r *record, environment, workspace string) error {
	root := "/run/grasp-e2b"
	// envd writes inherit its umask. Create a root-only parent before uploading
	// any credential, instead of staging readable files in /tmp.
	if _, err := d.client.StartDetached(ctx, &r.Provider, "install -d -m 700 "+root); err != nil {
		return err
	}
	// StartDetached returns the first PID, so wait for mkdir by retrying the
	// non-secret relay source. Secret-bearing files are written only afterwards.
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		err := d.client.WriteFile(deadline, &r.Provider, root+"/relay.py", []byte(GuestRelaySource))
		if err == nil {
			break
		}
		select {
		case <-deadline.Done():
			return errors.New("cannot initialize private E2B guest state")
		case <-time.After(50 * time.Millisecond):
		}
	}
	config, _ := json.Marshal(map[string]any{"secret": r.RelayKey, "marker": r.ID, "ports": []int{22, 8765, 8744}, "workspace": workspace})
	bootstrap := "#!/bin/bash\nset -u\nsource /run/grasp-e2b/env.sh\nmkdir -p -- \"$WORKSPACE_DIR\"\n/grasp-bootstrap.sh fetch\ncode=$?\nprintf '%s' \"$code\" > /run/grasp-e2b/bootstrap.exit\nexit \"$code\"\n"
	files := map[string][]byte{"relay.json": config, "env.sh": []byte(environment), "bootstrap.sh": []byte(bootstrap)}
	for _, name := range []string{"relay.json", "env.sh", "bootstrap.sh"} {
		if err := d.client.WriteFile(ctx, &r.Provider, root+"/"+name, files[name]); err != nil {
			return err
		}
	}
	command := "umask 077; chmod 600 /run/grasp-e2b/*; nohup python3 /run/grasp-e2b/relay.py /run/grasp-e2b >/run/grasp-e2b/relay.log 2>&1 </dev/null & nohup bash /run/grasp-e2b/bootstrap.sh >/run/grasp-e2b/bootstrap.log 2>&1 </dev/null & wait"
	_, err := d.client.StartDetached(ctx, &r.Provider, command)
	return err
}

func validTemplateRef(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_-/.:", c)) {
			return false
		}
	}
	return true
}

type health struct {
	Marker        string          `json:"marker"`
	Ports         map[string]bool `json:"ports"`
	BootstrapExit *int            `json:"bootstrapExit"`
	Workspace     *struct {
		AvailableBytes int64 `json:"availableBytes"`
	} `json:"workspace"`
}

func (d *Driver) waitHealthy(ctx context.Context, r *record, checkCapacity bool) error {
	creds, err := d.credentials(r)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	last := "relay not ready"
	for {
		var h health
		if err := d.tunnels.Control(ctx, creds, http.MethodGet, "/_bridge/health", nil, &h); err != nil {
			last = err.Error()
		} else {
			if h.Marker != r.ID {
				return errors.New("E2B relay identity mismatch")
			}
			if h.BootstrapExit != nil && *h.BootstrapExit != 0 {
				return fmt.Errorf("E2B bootstrap exited with code %d", *h.BootstrapExit)
			}
			if h.Ports["22"] && h.Ports["8765"] && h.Ports["8744"] {
				if checkCapacity && (h.Workspace == nil || h.Workspace.AvailableBytes < r.Profile.DiskGi*(1<<30)) {
					return errors.New("E2B prepared template has insufficient initial workspace capacity")
				}
				return nil
			}
			last = "waiting for SSH, session backend and IDE"
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("E2B guest readiness failed (%s): %w", last, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (d *Driver) publish(ctx context.Context, r *record) error {
	creds, err := d.credentials(r)
	if err != nil {
		return err
	}
	for port, target := range r.Targets {
		endpoint, err := d.tunnels.StartMapped(ctx, creds, port, target, r.Ports[port])
		if err != nil {
			return err
		}
		_, local, _ := net.SplitHostPort(endpoint)
		r.Ports[port], _ = strconv.Atoi(local)
	}
	return nil
}

func (d *Driver) refresh(ctx context.Context, r *record) (*driver.Handle, error) {
	if r.Phase == "deleting" {
		return handle(r, nil, driver.StatusNotFound), nil
	}
	s, err := d.client.Get(ctx, r.Provider.ID)
	if errors.Is(err, ErrNotFound) {
		_ = d.tunnels.Close(r.Provider.ID)
		return handle(r, nil, driver.StatusNotFound), nil
	}
	if err != nil {
		return nil, err
	}
	if !d.owns(s, r.ID) {
		return nil, errors.New("E2B provider ownership mismatch")
	}
	if s.State == "paused" || s.State == "stopped" {
		if r.Phase != "paused" {
			r.Phase = "paused"
			if err := d.save(r); err != nil {
				return nil, err
			}
		}
		_ = d.tunnels.Close(r.Provider.ID)
		return handle(r, nil, driver.StatusStopped), nil
	}
	if r.Phase == "creating" {
		return handle(r, nil, driver.StatusPending), nil
	}
	if err = d.waitHealthy(ctx, r, false); err != nil {
		return nil, err
	}
	if err = d.publish(ctx, r); err != nil {
		return nil, err
	}
	if r.Phase != "running" {
		r.Phase = "running"
		if err := d.save(r); err != nil {
			return nil, err
		}
	}
	return handle(r, d.tunnels.List(r.Provider.ID), driver.StatusRunning), nil
}

func (d *Driver) Get(ctx context.Context, id string) (*driver.Handle, error) {
	e, err := d.lookup(id)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return d.refresh(ctx, &e.record)
}
func (d *Driver) Status(ctx context.Context, id string) (driver.Status, error) {
	e, err := d.lookup(id)
	if errors.Is(err, ErrNotFound) {
		return driver.StatusNotFound, nil
	}
	if err != nil {
		return driver.StatusError, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Every private preview asset checks sandbox liveness. Bound remote probes
	// to once per two seconds while retaining immediate local lifecycle changes.
	if e.record.Phase == "running" && e.status == driver.StatusRunning && time.Since(e.statusAt) < 2*time.Second {
		return e.status, nil
	}
	h, err := d.refresh(ctx, &e.record)
	if err != nil {
		return driver.StatusError, err
	}
	e.status, e.statusAt = h.Status, time.Now()
	return h.Status, nil
}

func (d *Driver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	h, err := d.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if h.Status != driver.StatusRunning {
		return nil, errors.New("E2B sandbox is not running")
	}
	return h.Endpoints, nil
}
func (d *Driver) List(ctx context.Context) ([]*driver.Handle, error) {
	d.mu.Lock()
	ids := make([]string, 0, len(d.entries))
	for id := range d.entries {
		ids = append(ids, id)
	}
	d.mu.Unlock()
	sort.Strings(ids)
	var result []*driver.Handle
	for _, id := range ids {
		h, err := d.Get(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if h.Status != driver.StatusNotFound {
			result = append(result, h)
		}
	}
	return result, nil
}

func (d *Driver) PublishPort(ctx context.Context, id string, port int) error {
	if !validTunnelPort(port) {
		return errors.New("invalid or reserved E2B port")
	}
	e, err := d.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.record.Phase != "running" {
		return errors.New("E2B sandbox is not running")
	}
	current, getErr := d.client.Get(ctx, e.record.Provider.ID)
	if getErr != nil {
		return getErr
	}
	if !d.owns(current, id) {
		return errors.New("E2B provider ownership mismatch")
	}
	if _, exists := e.record.Targets[port]; !exists {
		e.record.Targets[port] = port
	}
	if err = d.publish(ctx, &e.record); err != nil {
		return err
	}
	return d.save(&e.record)
}

func (d *Driver) Stop(ctx context.Context, id string) error {
	e, err := d.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.record.Phase == "deleting" {
		return ErrNotFound
	}
	current, getErr := d.client.Get(ctx, e.record.Provider.ID)
	if getErr != nil {
		return getErr
	}
	if !d.owns(current, id) {
		return errors.New("E2B provider ownership mismatch")
	}
	if current.State != "paused" && current.State != "stopped" {
		if err = d.client.Pause(ctx, e.record.Provider.ID); err != nil {
			return err
		}
	}
	e.record.Phase = "paused"
	_ = d.tunnels.Close(e.record.Provider.ID)
	return d.save(&e.record)
}
func (d *Driver) Start(ctx context.Context, id string) error {
	e, err := d.lookup(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.record.Phase == "running" {
		h, err := d.refresh(ctx, &e.record)
		if err != nil {
			return err
		}
		if h.Status != driver.StatusRunning {
			return errors.New("E2B sandbox is not running")
		}
		return nil
	}
	if e.record.Phase != "paused" {
		return errors.New("E2B sandbox is not paused")
	}
	current, getErr := d.client.Get(ctx, e.record.Provider.ID)
	if getErr != nil {
		return getErr
	}
	if !d.owns(current, id) {
		return errors.New("E2B provider ownership mismatch")
	}
	s, err := d.client.Connect(ctx, e.record.Provider.ID, int(d.opts.Lease/time.Second))
	if s != nil {
		if s.EnvdAccessToken != "" {
			e.record.Provider.EnvdAccessToken = s.EnvdAccessToken
		}
		if s.TrafficAccessToken != "" {
			e.record.Provider.TrafficAccessToken = s.TrafficAccessToken
		}
		if s.Domain != "" {
			e.record.Provider.Domain = s.Domain
		}
		if saveErr := d.save(&e.record); saveErr != nil {
			return saveErr
		}
	}
	if err != nil {
		return err
	}
	if err = d.waitHealthy(ctx, &e.record, false); err != nil {
		return err
	}
	if err = d.publish(ctx, &e.record); err != nil {
		return err
	}
	e.record.Phase = "running"
	return d.save(&e.record)
}

func (d *Driver) cleanup(ctx context.Context, r *record) error {
	_ = d.tunnels.Close(r.Provider.ID)
	// A timed-out POST may have created a sandbox before its response was
	// received. Discover only our reserved owner+gateway metadata to retire it.
	if r.Provider.ID == "" {
		sandboxes, err := d.client.List(ctx, map[string]string{ownerLabel: d.opts.OwnerID, idLabel: r.ID, driverLabel: "e2b"})
		if err != nil {
			return err
		}
		for _, s := range sandboxes {
			if !d.owns(&s, r.ID) {
				continue
			}
			if err = d.client.Delete(ctx, s.ID); err != nil {
				return err
			}
		}
		if len(sandboxes) == 0 && time.Now().Before(r.UncertainUntil) {
			return errors.New("E2B create response uncertain; awaiting owned-sandbox reconciliation")
		}
	} else {
		current, err := d.client.Get(ctx, r.Provider.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil && !d.owns(current, r.ID) {
			return errors.New("refusing to delete E2B sandbox owned by another gateway")
		}
		if err == nil {
			if err = d.client.Delete(ctx, r.Provider.ID); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(d.statePath(r.ID)); err != nil && !os.IsNotExist(err) {
		return errors.New("cannot remove E2B recovery record")
	}
	d.mu.Lock()
	delete(d.entries, r.ID)
	d.mu.Unlock()
	return nil
}
func (d *Driver) Destroy(ctx context.Context, id string) error {
	d.mu.Lock()
	e := d.entries[id]
	if e != nil && e.cancel != nil {
		e.cancel()
	}
	d.mu.Unlock()
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.record.Phase = "deleting"
	saveErr := d.save(&e.record)
	cleanupErr := d.cleanup(ctx, &e.record)
	return errors.Join(saveErr, cleanupErr)
}
func (d *Driver) Reinstall(ctx context.Context, spec driver.Spec, preserveData bool) error {
	if preserveData {
		return errors.New("E2B reinstall with data preservation requires an explicit snapshot migration")
	}
	if _, err := d.profile(spec); err != nil {
		return err
	}
	if err := d.Destroy(ctx, spec.ID); err != nil {
		return err
	}
	_, err := d.Create(ctx, spec)
	return err
}
func (d *Driver) Logs(ctx context.Context, id string, tail int) (string, error) {
	e, err := d.lookup(id)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	current, getErr := d.client.Get(ctx, e.record.Provider.ID)
	if getErr != nil {
		return "", getErr
	}
	if !d.owns(current, id) {
		return "", errors.New("E2B provider ownership mismatch")
	}
	creds, err := d.credentials(&e.record)
	if err != nil {
		return "", err
	}
	var response struct {
		Content string `json:"content"`
	}
	if err = d.tunnels.Control(ctx, creds, http.MethodGet, "/_bridge/logs", nil, &response); err != nil {
		return "", err
	}

	secrets := append(append([]string{}, e.record.Secrets...), d.opts.APIKey, e.record.RelayKey, e.record.Provider.EnvdAccessToken, e.record.Provider.TrafficAccessToken)
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	for _, secret := range secrets {
		if secret != "" {
			response.Content = strings.ReplaceAll(response.Content, secret, "[redacted]")
		}
	}

	if tail <= 0 {
		tail = 200
	}
	lines := strings.Split(response.Content, "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return strings.Join(lines, "\n"), nil
}
func (d *Driver) maintain(ctx context.Context) {
	defer close(d.done)
	d.sweep(ctx)
	ticker := time.NewTicker(d.opts.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.sweep(ctx)
		}
	}
}
func (d *Driver) sweep(ctx context.Context) {
	d.mu.Lock()
	entries := make([]*entry, 0, len(d.entries))
	for _, e := range d.entries {
		entries = append(entries, e)
	}
	d.mu.Unlock()
	for _, e := range entries {
		if !e.mu.TryLock() {
			continue
		}
		func() {
			defer e.mu.Unlock()
			bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if e.record.Phase == "deleting" {
				if err := d.cleanup(bounded, &e.record); err != nil {
					log.Warn().Err(err).Str("sandbox_id", e.record.ID).Msg("E2B deletion pending retry")
				}
			} else if e.record.Phase == "running" {
				s, err := d.client.Get(bounded, e.record.Provider.ID)
				if err == nil && d.owns(s, e.record.ID) && s.State != "paused" && s.State != "stopped" {
					err = d.client.SetTimeout(bounded, s.ID, int(d.opts.Lease/time.Second))
					if err != nil {
						log.Warn().Err(err).Str("sandbox_id", e.record.ID).Msg("E2B lease renewal failed; provider expiry remains in force")
					}
				}
			}
		}()
	}
}
