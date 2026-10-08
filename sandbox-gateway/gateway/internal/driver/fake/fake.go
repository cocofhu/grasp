// Package fake provides an in-memory Driver for gateway unit tests.
package fake

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"sandbox-gateway/internal/driver"
)

// Driver is a thread-safe in-memory Driver. Create/Reinstall record Specs so
// tests can assert env (e.g. SANDBOX_INJECT) and Config.
type Driver struct {
	mu sync.Mutex

	ln          net.Listener // optional local listener for finalize waitTCP
	sessionPort int

	sandboxes map[string]*entry

	LastCreate        *driver.Spec
	LastReinstall     *driver.Spec
	ReinstallPreserve *bool
}

type entry struct {
	spec      driver.Spec
	status    driver.Status
	eps       map[int]string
	createdAt time.Time
	logs      string
	exit      *driver.ExitInfo
}

// New returns an empty fake Driver.
func New() *Driver {
	return &Driver{sandboxes: map[string]*entry{}}
}

// WithSessionListener binds 127.0.0.1:0 and publishes it as the session
// endpoint so service.finalize's waitTCP succeeds.
func (d *Driver) WithSessionListener(sessionPort int) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	d.ln = ln
	d.sessionPort = sessionPort
	go acceptLoop(ln)
	return nil
}

// Close releases the optional session listener.
func (d *Driver) Close() {
	if d.ln != nil {
		_ = d.ln.Close()
	}
}

func acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}
}

func (d *Driver) Name() string { return "fake" }

func (d *Driver) endpointsFor(spec driver.Spec) map[int]string {
	eps := map[int]string{}
	addr := "127.0.0.1:9" // default closed port; override if listener present
	if d.ln != nil {
		addr = d.ln.Addr().String()
	}
	for _, p := range spec.Ports {
		eps[p] = fmt.Sprintf("127.0.0.1:%d", 30000+p)
	}
	for _, p := range spec.InternalPorts {
		if _, ok := eps[p]; ok {
			continue
		}
		eps[p] = fmt.Sprintf("10.88.0.2:%d", p)
	}
	if d.sessionPort > 0 {
		eps[d.sessionPort] = addr
	} else if len(spec.Ports) > 0 {
		// Best effort: first port maps to listener if any.
		if d.ln != nil {
			eps[spec.Ports[0]] = addr
		}
	}
	return eps
}

func (d *Driver) Create(_ context.Context, spec driver.Spec) (*driver.Handle, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := spec
	d.LastCreate = &cp
	eps := d.endpointsFor(spec)
	d.sandboxes[spec.ID] = &entry{spec: spec, status: driver.StatusRunning, eps: eps, createdAt: time.Now()}
	return &driver.Handle{
		ID:        spec.ID,
		Name:      "fake-" + spec.ID,
		Status:    driver.StatusRunning,
		Endpoints: eps,
		CreatedAt: time.Now(),
	}, nil
}

func (d *Driver) Start(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	e.status = driver.StatusRunning
	return nil
}

func (d *Driver) Stop(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return fmt.Errorf("not found")
	}
	e.status = driver.StatusStopped
	return nil
}

func (d *Driver) Destroy(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sandboxes, id)
	return nil
}

func (d *Driver) Reinstall(_ context.Context, spec driver.Spec, preserveData bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := spec
	d.LastReinstall = &cp
	p := preserveData
	d.ReinstallPreserve = &p
	eps := d.endpointsFor(spec)
	d.sandboxes[spec.ID] = &entry{spec: spec, status: driver.StatusRunning, eps: eps}
	return nil
}

func (d *Driver) Get(_ context.Context, id string) (*driver.Handle, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &driver.Handle{ID: id, Name: "fake-" + id, Status: e.status, Endpoints: e.eps}, nil
}

func (d *Driver) List(_ context.Context) ([]*driver.Handle, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*driver.Handle, 0, len(d.sandboxes))
	for id, e := range d.sandboxes {
		out = append(out, &driver.Handle{
			ID: id, Name: "fake-" + id, Status: e.status, Endpoints: e.eps, CreatedAt: e.createdAt,
		})
	}
	return out, nil
}

// SeedOrphan inserts a workload without going through Create (for orphan-GC tests).
func (d *Driver) SeedOrphan(id string, createdAt time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sandboxes[id] = &entry{
		spec:      driver.Spec{ID: id},
		status:    driver.StatusRunning,
		eps:       map[int]string{},
		createdAt: createdAt,
	}
}

func (d *Driver) Status(_ context.Context, id string) (driver.Status, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return driver.StatusNotFound, nil
	}
	return e.status, nil
}

func (d *Driver) Endpoints(_ context.Context, id string) (map[int]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return e.eps, nil
}

// SetLogs sets canned PID1 logs for a seeded/created sandbox (unit tests).
func (d *Driver) SetLogs(id, content string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.sandboxes[id]; ok {
		e.logs = content
	}
}

// Logs returns canned container logs for tests (empty when unset).
func (d *Driver) Logs(_ context.Context, id string, _ int) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return e.logs, nil
}

// SetExit sets the canned last exit for a sandbox (unit tests).
func (d *Driver) SetExit(id string, e *driver.ExitInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.sandboxes[id]; ok {
		s.exit = e
	}
}

// LastExit returns the canned last exit (nil when unset).
func (d *Driver) LastExit(_ context.Context, id string) (*driver.ExitInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return e.exit, nil
}

// SpecOf returns the last stored Spec for id (for assertions).
func (d *Driver) SpecOf(id string) (driver.Spec, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.sandboxes[id]
	if !ok {
		return driver.Spec{}, false
	}
	return e.spec, true
}
