package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// runtimeReloadInterval throttles re-stat of the bundle file, so a rebuilt
// bundle (./start.sh runtime) is picked up without restarting the server.
const runtimeReloadInterval = 30 * time.Second

var runtimeVersionRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// RuntimeManifest is the MANIFEST at the root of a runtime bundle.
type RuntimeManifest struct {
	Version  string
	Arch     string
	MinImage int
}

// RuntimeBundle serves the sandbox runtime bundle (Grasp's scripts and
// programs, built by scripts/build-sandbox-runtime.sh) and remembers which
// version each sandbox runs, so EnsureRuntime can push updates.
type RuntimeBundle struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	data     []byte
	manifest RuntimeManifest
	modTime  time.Time
	size     int64
	checked  time.Time

	stateMu  sync.Mutex
	state    map[string]*runtimeState
	inflight map[string]bool
}

type runtimeState struct {
	version        string
	backendPending bool
	skip           string // non-empty: never retry (image too old)
}

// NewRuntimeBundle reads the bundle lazily from path on first use.
func NewRuntimeBundle(path string) *RuntimeBundle {
	return &RuntimeBundle{
		path:     strings.TrimSpace(path),
		now:      time.Now,
		state:    map[string]*runtimeState{},
		inflight: map[string]bool{},
	}
}

// Path is the bundle file this instance serves.
func (r *RuntimeBundle) Path() string { return r.path }

// Current returns the bundle bytes and manifest, reloading the file when it
// changed. A bundle that disappears or fails to parse keeps serving the last
// good copy; with no good copy yet it is an error.
func (r *RuntimeBundle) Current() ([]byte, RuntimeManifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if r.data != nil && now.Sub(r.checked) < runtimeReloadInterval {
		return r.data, r.manifest, nil
	}
	r.checked = now
	if err := r.reloadLocked(); err != nil {
		if r.data != nil {
			log.Warn().Err(err).Str("path", r.path).Msg("sandbox runtime bundle reload failed; keeping previous")
			return r.data, r.manifest, nil
		}
		return nil, RuntimeManifest{}, err
	}
	return r.data, r.manifest, nil
}

func (r *RuntimeBundle) reloadLocked() error {
	if r.path == "" {
		return errors.New("sandbox runtime bundle path not configured")
	}
	fi, err := os.Stat(r.path)
	if err != nil {
		return fmt.Errorf("sandbox runtime bundle missing at %s: run scripts/build-sandbox-runtime.sh: %w", r.path, err)
	}
	if r.data != nil && fi.ModTime().Equal(r.modTime) && fi.Size() == r.size {
		return nil
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return fmt.Errorf("read sandbox runtime bundle: %w", err)
	}
	man, err := ParseRuntimeManifest(data)
	if err != nil {
		return fmt.Errorf("sandbox runtime bundle %s: %w", r.path, err)
	}
	if r.data == nil || man.Version != r.manifest.Version {
		log.Info().Str("version", man.Version).Int("min_image", man.MinImage).Str("path", r.path).
			Msg("sandbox runtime bundle loaded")
	}
	r.data, r.manifest, r.modTime, r.size = data, man, fi.ModTime(), fi.Size()
	return nil
}

// ParseRuntimeManifest reads MANIFEST out of a runtime bundle .tgz.
func ParseRuntimeManifest(tgz []byte) (RuntimeManifest, error) {
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return RuntimeManifest{}, fmt.Errorf("not a gzip archive: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return RuntimeManifest{}, errors.New("MANIFEST not found")
		}
		if err != nil {
			return RuntimeManifest{}, fmt.Errorf("read tar: %w", err)
		}
		if strings.TrimPrefix(h.Name, "./") != "MANIFEST" {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(tr, 4096))
		if err != nil {
			return RuntimeManifest{}, fmt.Errorf("read MANIFEST: %w", err)
		}
		return parseManifestText(string(raw))
	}
}

func parseManifestText(s string) (RuntimeManifest, error) {
	var m RuntimeManifest
	minImage := ""
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "version":
			m.Version = v
		case "arch":
			m.Arch = v
		case "min_image":
			minImage = v
		}
	}
	if !runtimeVersionRe.MatchString(m.Version) {
		return RuntimeManifest{}, fmt.Errorf("MANIFEST version %q is not a sha256", m.Version)
	}
	n, err := strconv.Atoi(minImage)
	if err != nil || n < 0 {
		return RuntimeManifest{}, fmt.Errorf("MANIFEST min_image %q is not a non-negative integer", minImage)
	}
	m.MinImage = n
	return m, nil
}

// noteInstalled records the version a sandbox was created with.
func (r *RuntimeBundle) noteInstalled(id, version string) {
	if r == nil || id == "" {
		return
	}
	r.stateMu.Lock()
	r.state[id] = &runtimeState{version: version}
	r.stateMu.Unlock()
}

// begin claims the per-sandbox update slot; false when one is already running.
func (r *RuntimeBundle) begin(id string) (runtimeState, bool) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	if r.inflight[id] {
		return runtimeState{}, false
	}
	r.inflight[id] = true
	if st := r.state[id]; st != nil {
		return *st, true
	}
	return runtimeState{}, true
}

func (r *RuntimeBundle) end(id string, st runtimeState) {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	delete(r.inflight, id)
	r.state[id] = &st
}

// upToDate reports whether a sandbox needs no work, without claiming the slot.
func (r *RuntimeBundle) upToDate(id, version string) bool {
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	st := r.state[id]
	return st != nil && (st.skip != "" || (st.version == version && !st.backendPending))
}

// forget drops a destroyed sandbox's state.
func (r *RuntimeBundle) forget(id string) {
	if r == nil {
		return
	}
	r.stateMu.Lock()
	delete(r.state, id)
	r.stateMu.Unlock()
}
