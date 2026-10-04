package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// ReloadCallback is invoked after a successful reload with the old and new
// configs so callers can diff and react.
type ReloadCallback func(old, new_ *Config)

// test hooks for injecting fsnotify failures in unit tests.
var (
	watcherNewFn = func() (*fsnotify.Watcher, error) {
		return fsnotify.NewWatcher()
	}
	watcherAddFn = func(w *fsnotify.Watcher, path string) error {
		return w.Add(path)
	}
	watcherCloseFn = func(w *fsnotify.Watcher) error {
		return w.Close()
	}
)

type reloadScheduler struct {
	path      string
	callbacks []ReloadCallback
	debounce  *time.Timer
}

func (s *reloadScheduler) trigger() {
	if s.debounce != nil {
		s.debounce.Stop()
	}
	s.debounce = time.AfterFunc(1*time.Second, func() {
		old := GetConfig()
		newCfg, err := Reload(s.path)
		if err != nil {
			log.Warn().Err(err).Msg("config: reload failed (keeping old config)")
			return
		}
		logReloadDiff(old, newCfg)
		for _, cb := range s.callbacks {
			cb(old, newCfg)
		}
	})
}

func (s *reloadScheduler) stop() {
	if s.debounce != nil {
		s.debounce.Stop()
	}
}

type fileSnapshot struct {
	modTime time.Time
	size    int64
}

func statSnapshot(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileSnapshot{}, err
	}
	return fileSnapshot{modTime: info.ModTime(), size: info.Size()}, nil
}

func (s fileSnapshot) changed(other fileSnapshot) bool {
	return !s.modTime.Equal(other.modTime) || s.size != other.size
}

func isInotifyLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "inotify") ||
		strings.Contains(msg, "emfile") ||
		strings.Contains(msg, "too many open files")
}

// WatchAndReload monitors the config file for changes (e.g. ConfigMap volume
// updates) and reloads on WRITE/CREATE/RENAME events, debounced by 1s to
// avoid partial reads mid-write.
//
// It watches the directory (not the file) because ConfigMap updates
// rename-and-replace the file, which drops an inode-level watch. Cancel ctx to
// stop the watcher. callbacks run sequentially after each successful reload.
//
// When fsnotify fails due to inotify instance limits (EMFILE), it falls back
// to polling the file every 1s (mtime+size). Non-inotify errors (missing path,
// permission denied) still return an error.
//
// K8s preview manual verification (no CI E2E required):
//   - EMFILE / inotify exhausted: expect warn (fsnotify failure) then info with
//     mode=polling, interval=1s; update ConfigMap and confirm GetConfig() changes ~1–2s.
//   - fsnotify healthy: info without mode=polling; ConfigMap update still hot-reloads.
func WatchAndReload(ctx context.Context, path string, callbacks ...ReloadCallback) error {
	watcher, err := watcherNewFn()
	if err != nil {
		if isInotifyLimitError(err) {
			startPollingReload(ctx, path, callbacks, err)
			return nil
		}
		return err
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := watcherAddFn(watcher, dir); err != nil {
		_ = watcherCloseFn(watcher)
		if isInotifyLimitError(err) {
			startPollingReload(ctx, path, callbacks, err)
			return nil
		}
		return err
	}

	go func() {
		defer func() { _ = watcherCloseFn(watcher) }()
		scheduler := &reloadScheduler{path: path, callbacks: callbacks}
		defer scheduler.stop()
		for {
			select {
			case <-ctx.Done():
				return

			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if filepath.Base(ev.Name) != base {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
					continue
				}
				scheduler.trigger()

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warn().Err(err).Msg("config: watcher error")
			}
		}
	}()

	log.Info().Str("path", path).Msg("config: watching for changes (hot-reload enabled)")
	return nil
}

func startPollingReload(ctx context.Context, path string, callbacks []ReloadCallback, reason error) {
	log.Warn().Err(reason).Msg("config: fsnotify unavailable, falling back to polling")
	log.Info().
		Str("path", path).
		Str("mode", "polling").
		Str("interval", "1s").
		Msg("config: watching for changes (hot-reload enabled)")

	go func() {
		scheduler := &reloadScheduler{path: path, callbacks: callbacks}
		defer scheduler.stop()

		snapshot, err := statSnapshot(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Warn().Err(err).Str("path", path).Msg("config: polling initial stat failed")
		}

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current, err := statSnapshot(path)
				if err != nil {
					continue
				}
				if snapshot.changed(current) {
					snapshot = current
					scheduler.trigger()
				}
			}
		}
	}()
}

func logReloadDiff(old, new_ *Config) {
	if old == nil {
		log.Info().Msg("config: reload (initial)")
		return
	}
	log.Info().Msg("config: reloaded successfully")
	if old.Server.Port != new_.Server.Port {
		log.Warn().Int("old", old.Server.Port).Int("new", new_.Server.Port).
			Msg("config: server.port changed — requires restart to take effect")
	}
	if old.Database.Path != new_.Database.Path {
		log.Warn().Str("old", old.Database.Path).Str("new", new_.Database.Path).
			Msg("config: database.path changed — requires restart to take effect")
	}
}
