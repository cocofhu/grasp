package e2b

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"sandbox-gateway/internal/driver"

	"golang.org/x/sys/unix"
)

// This is a private recovery journal, separate from gateway API records. E2B's
// GET response does not return private traffic credentials. Losing this volume
// loses the ability to reconnect; the provider lease still bounds guest life.
type record struct {
	UncertainUntil time.Time       `json:"uncertainUntil,omitempty"`
	Version        int             `json:"version"`
	Owner          string          `json:"owner"`
	ID             string          `json:"id"`
	Provider       Sandbox         `json:"provider"`
	EnvdToken      string          `json:"envdToken"`
	TrafficToken   string          `json:"trafficToken"`
	RelayKey       string          `json:"relayKey"`
	Phase          string          `json:"phase"`
	CreatedAt      time.Time       `json:"createdAt"`
	Ports          map[int]int     `json:"ports"`
	Targets        map[int]int     `json:"targets"`
	Secrets        []string        `json:"secrets"`
	Profile        TemplateProfile `json:"profile"`
}

type entry struct {
	mu       sync.Mutex
	statusAt time.Time
	status   driver.Status
	record   record
	// cancel is protected by Driver.mu so Destroy can interrupt bootstrap
	// before waiting for the entry's lifecycle lock.
	cancel func()
}

func (d *Driver) statePath(id string) string { return filepath.Join(d.opts.StateDir, id+".json") }

func (d *Driver) save(r *record) error {
	r.Version, r.Owner = 1, d.opts.OwnerID
	r.EnvdToken, r.TrafficToken = r.Provider.EnvdAccessToken, r.Provider.TrafficAccessToken
	data, err := json.Marshal(r)
	if err != nil {
		return errors.New("cannot encode private E2B recovery state")
	}
	f, err := os.CreateTemp(d.opts.StateDir, ".pending-")
	if err != nil {
		return errors.New("cannot create private E2B recovery state")
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot write private E2B recovery state")
	}
	if err = os.Rename(name, d.statePath(r.ID)); err != nil {
		return errors.New("cannot replace private E2B recovery state")
	}
	dir, err := os.Open(d.opts.StateDir)
	if err != nil {
		return errors.New("cannot open private E2B state directory")
	}
	err = dir.Sync()
	_ = dir.Close()
	if err != nil {
		return errors.New("cannot sync private E2B recovery state")
	}
	return nil
}

func (d *Driver) load() error {
	if !filepath.IsAbs(d.opts.StateDir) {
		return errors.New("E2B stateDir must be absolute")
	}
	if err := os.MkdirAll(d.opts.StateDir, 0700); err != nil {
		return errors.New("cannot create E2B state directory")
	}
	info, err := os.Lstat(d.opts.StateDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid E2B state directory")
	}
	if err := os.Chmod(d.opts.StateDir, 0700); err != nil {
		return errors.New("cannot protect E2B state directory")
	}
	lock, err := os.OpenFile(filepath.Join(d.opts.StateDir, ".lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return errors.New("cannot open E2B state lock")
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		return errors.New("E2B state directory is already in use")
	}
	d.lockFile = lock

	files, err := os.ReadDir(d.opts.StateDir)
	if err != nil {
		return errors.New("cannot read E2B recovery state")
	}
	for _, f := range files {
		if filepath.Ext(f.Name()) != ".json" {
			continue
		}
		info, err := f.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("E2B recovery files must be private regular files")
		}
		data, err := os.ReadFile(filepath.Join(d.opts.StateDir, f.Name()))
		if err != nil || len(data) > 8<<20 {
			return errors.New("cannot read E2B recovery record")
		}
		var r record
		if json.Unmarshal(data, &r) != nil || r.Version != 1 || r.Owner != d.opts.OwnerID || !providerIDPattern.MatchString(r.ID) || f.Name() != r.ID+".json" {
			return errors.New("invalid E2B recovery record or owner mismatch")
		}
		if r.Provider.ID != "" && !d.owns(&r.Provider, r.ID) {
			return errors.New("E2B recovery provider ownership mismatch")
		}
		switch r.Phase {
		case "creating", "running", "paused", "deleting":
		default:
			return errors.New("invalid E2B recovery phase")
		}
		for port, target := range r.Targets {
			if !validTunnelPort(port) || !validTunnelPort(target) {
				return errors.New("invalid E2B recovery port mapping")
			}
		}
		for port, local := range r.Ports {
			if !validTunnelPort(port) || local <= 0 || local > 65535 || r.Targets[port] == 0 {
				return errors.New("invalid E2B recovery endpoint")
			}
		}
		if (r.Phase == "running" || r.Phase == "paused") && (r.Provider.ID == "" || r.RelayKey == "" || r.TrafficToken == "" || r.EnvdToken == "") {
			return errors.New("incomplete private E2B recovery credentials")
		}

		r.Provider.EnvdAccessToken, r.Provider.TrafficAccessToken = r.EnvdToken, r.TrafficToken
		if r.Ports == nil {
			r.Ports = map[int]int{}
		}
		if r.Targets == nil {
			r.Targets = map[int]int{}
		}
		// An interrupted create never completed gateway readiness. Retire it
		// rather than exposing a partially configured guest after restart.
		if r.Phase == "creating" {
			r.Phase = "deleting"
		}
		d.entries[r.ID] = &entry{record: r}
	}
	return nil
}

func handle(r *record, endpoints map[int]string, status driver.Status) *driver.Handle {
	return &driver.Handle{ID: r.ID, Name: r.Provider.ID, CreatedAt: r.CreatedAt, Status: status, Endpoints: endpoints}
}
