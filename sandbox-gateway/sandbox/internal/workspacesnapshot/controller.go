package workspacesnapshot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"backend/internal/workspacejob"
)

const MaxChunkBytes = 1 << 20

var ErrPending = errors.New("workspace artifact receiver is not running yet")

type ReadSpec struct {
	SourceJobID  string  `json:"sourceJobId"`
	SourceSHA256 string  `json:"sourceSha256"`
	Receipt      Receipt `json:"receipt"`
}

type RestoreSpec struct {
	Receipt Receipt `json:"receipt"`
}

// ControllerConfig is supplied by authenticated runtime startup. Roots and the
// retained execution lineage are never accepted from artifact HTTP requests.
type ControllerConfig struct {
	Engine      Config
	Jobs        workspacejob.Config
	RestoreRoot string
}

type FileCapabilities struct {
	Version       int                       `json:"version"`
	Jobs          workspacejob.Capabilities `json:"jobs"`
	Capture       bool                      `json:"capture"`
	Read          bool                      `json:"read"`
	Restore       bool                      `json:"restore"`
	PolicyVersion string                    `json:"policyVersion"`
	PolicySHA256  string                    `json:"policySha256"`
	Limits        Limits                    `json:"limits"`
	MaxChunkBytes int                       `json:"maxChunkBytes"`
	Consistency   string                    `json:"consistency"`
	ProcessMemory bool                      `json:"processMemory"`
	Metadata      []string                  `json:"metadata"`
}

type transfer struct {
	mu       sync.Mutex
	handle   *workspacejob.Handle
	kind     string
	read     ReadSpec
	restore  RestoreSpec
	stage    *os.Root
	done     chan struct{}
	finished bool
}

// Controller owns real executors and bounded artifact transport together. A
// control-only Service cannot advertise these file capabilities by accident.
type Controller struct {
	Engine  *Engine
	Jobs    *workspacejob.Service
	restore *os.Root
	mu      sync.Mutex
	live    map[string]*transfer
}

func NewController(cfg ControllerConfig) (*Controller, error) {
	if cfg.RestoreRoot == "" || len(cfg.Jobs.Executors) != 0 {
		return nil, workspacejob.ErrInvalid
	}
	// Restores beneath the authorized source are private platform directories,
	// never silently recaptured by selecting the whole workspace later.
	if rel, err := filepath.Rel(cfg.Engine.Root, cfg.RestoreRoot); err == nil && safePath(filepath.ToSlash(rel)) && rel != "." {
		cfg.Engine.PrivatePaths = append(cfg.Engine.PrivatePaths, filepath.ToSlash(rel))
	}
	engine, err := New(cfg.Engine)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(cfg.RestoreRoot)
	if err != nil {
		_ = engine.Close()
		return nil, err
	}
	c := &Controller{Engine: engine, restore: root, live: map[string]*transfer{}}
	cfg.Jobs.Executors = map[string]workspacejob.Executor{"capture_files": engine.capture, "read_snapshot": c.read, "restore_files": c.receive}
	c.Jobs, err = workspacejob.New(cfg.Jobs)
	if err != nil {
		_ = engine.Close()
		_ = root.Close()
		return nil, err
	}
	return c, nil
}

func (c *Controller) Close(ctx context.Context) error {
	if err := c.Jobs.Close(ctx); err != nil {
		return err // Keep roots open until every fenced executor has joined.
	}
	return errors.Join(c.Engine.Close(), c.restore.Close())
}

func (c *Controller) Capabilities() FileCapabilities {
	jobs := c.Jobs.Capabilities()
	jobs.FileCapture, jobs.FileRestore = true, true
	return FileCapabilities{Version: Version, Jobs: jobs, Capture: true, Read: true, Restore: true, PolicyVersion: PolicyVersion, PolicySHA256: c.Engine.policy, Limits: c.Engine.config.Limits, MaxChunkBytes: MaxChunkBytes, Consistency: "verified_copy", Metadata: []string{"bytes", "executable_mode", "relative_symlink", "git_refs_index"}}
}

func strictDecode(body []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return ErrUnsafe
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrUnsafe
	}
	return nil
}

func (c *Controller) register(h *workspacejob.Handle, item *transfer) func() {
	c.mu.Lock()
	c.live[h.Identity().ID] = item
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.live, h.Identity().ID)
		c.mu.Unlock()
	}
}

func (c *Controller) read(ctx context.Context, h *workspacejob.Handle, body json.RawMessage) (json.RawMessage, error) {
	var spec ReadSpec
	if strictDecode(body, &spec) != nil || !hashPattern.MatchString(spec.SourceSHA256) {
		return nil, ErrUnsafe
	}
	source, err := c.Jobs.Get(spec.SourceJobID)
	if err != nil || source.State != "completed" || source.Kind != "capture_files" || source.SHA256 != spec.SourceSHA256 {
		return nil, workspacejob.ErrConflict
	}
	var receipt Receipt
	if strictDecode(source.Result, &receipt) != nil || receipt != spec.Receipt || !validReceipt(receipt, c.Engine.config.Limits) {
		return nil, ErrIntegrity
	}
	item := &transfer{handle: h, kind: "read_snapshot", read: spec, done: make(chan struct{})}
	defer c.register(h, item)()
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-item.done:
		if err := h.Check(); err != nil {
			return nil, err
		}
		return json.Marshal(receipt)
	}
}

func validReceipt(value Receipt, limits Limits) bool {
	return value.Version == Version && value.Consistency == "verified_copy" && value.Files >= 0 && value.Files <= limits.MaxEntries && value.Bytes >= 0 && value.Bytes <= limits.MaxBytes && value.Archive.Name == ArchiveName && value.Archive.Bytes > 0 && value.Archive.Bytes <= limits.MaxArchiveBytes && hashPattern.MatchString(value.Archive.SHA256) && value.Manifest.Name == ManifestName && value.Manifest.Bytes > 0 && value.Manifest.Bytes <= limits.MaxManifestBytes && hashPattern.MatchString(value.Manifest.SHA256)
}

func (c *Controller) receive(ctx context.Context, h *workspacejob.Handle, body json.RawMessage) (json.RawMessage, error) {
	var spec RestoreSpec
	if strictDecode(body, &spec) != nil || !validReceipt(spec.Receipt, c.Engine.config.Limits) {
		return nil, ErrUnsafe
	}
	stage, err := h.OpenStage()
	if err != nil {
		return nil, err
	}
	defer func() { _ = stage.Close() }()
	item := &transfer{handle: h, kind: "restore_files", restore: spec, stage: stage, done: make(chan struct{})}
	defer c.register(h, item)()
	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case <-item.done:
	}
	manifest, err := readSmall(stage, ManifestName, c.Engine.config.Limits.MaxManifestBytes)
	if err != nil {
		return nil, err
	}
	archive, err := stage.OpenFile(ArchiveName, os.O_RDONLY|nofollow, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = archive.Close() }()
	result, err := c.Engine.Materialize(ctx, h, c.restore, h.Identity().ID, archive, manifest, spec.Receipt)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (c *Controller) transfer(id string, req workspacejob.RenewRequest) (*transfer, error) {
	h, err := c.Jobs.LiveHandle(id, req)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	item := c.live[id]
	c.mu.Unlock()
	if item == nil {
		return nil, ErrPending
	}
	if item.handle != h {
		return nil, workspacejob.ErrFence
	}
	return item, h.Check()
}
