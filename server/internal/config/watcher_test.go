package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestIsInotifyLimitError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{errors.New("inotify_init: too many open files"), true},
		{errors.New("EMFILE: no inotify instances available"), true},
		{errors.New("Too Many Open Files"), true},
		{errors.New("no such file or directory"), false},
		{errors.New("permission denied"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := isInotifyLimitError(tc.err); got != tc.want {
			t.Errorf("isInotifyLimitError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestWatchAndReloadPollingFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("sandbox:\n  max_sandboxes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}

	origNew := watcherNewFn
	origAdd := watcherAddFn
	t.Cleanup(func() {
		watcherNewFn = origNew
		watcherAddFn = origAdd
	})

	t.Run("newWatcher EMFILE", func(t *testing.T) {
		watcherNewFn = func() (*fsnotify.Watcher, error) {
			return nil, errors.New("fsnotify.NewWatcher: too many open files (EMFILE)")
		}
		watcherAddFn = origAdd

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		reloaded := make(chan struct{}, 1)
		if err := WatchAndReload(ctx, path, func(old, new_ *Config) {
			select {
			case reloaded <- struct{}{}:
			default:
			}
		}); err != nil {
			t.Fatalf("watch: %v", err)
		}

		time.Sleep(100 * time.Millisecond)
		if err := os.WriteFile(path, []byte("sandbox:\n  max_sandboxes: 9\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-reloaded:
			if GetConfig().Sandbox.MaxSandboxes != 9 {
				t.Errorf("reload did not apply: %d", GetConfig().Sandbox.MaxSandboxes)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("polling watcher did not reload within 5s")
		}
	})

	t.Run("addWatch EMFILE", func(t *testing.T) {
		watcherNewFn = origNew
		watcherAddFn = func(w *fsnotify.Watcher, path string) error {
			return errors.New("inotify_add_watch: EMFILE")
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		reloaded := make(chan struct{}, 1)
		if err := WatchAndReload(ctx, path, func(old, new_ *Config) {
			select {
			case reloaded <- struct{}{}:
			default:
			}
		}); err != nil {
			t.Fatalf("watch: %v", err)
		}

		time.Sleep(100 * time.Millisecond)
		if err := os.WriteFile(path, []byte("sandbox:\n  max_sandboxes: 3\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		select {
		case <-reloaded:
			if GetConfig().Sandbox.MaxSandboxes != 3 {
				t.Errorf("reload did not apply: %d", GetConfig().Sandbox.MaxSandboxes)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("polling watcher did not reload within 5s")
		}
	})
}

func TestWatchAndReloadNonInotifyAddError(t *testing.T) {
	origNew := watcherNewFn
	origAdd := watcherAddFn
	origClose := watcherCloseFn
	t.Cleanup(func() {
		watcherNewFn = origNew
		watcherAddFn = origAdd
		watcherCloseFn = origClose
	})

	stub := &fsnotify.Watcher{
		Events: make(chan fsnotify.Event),
		Errors: make(chan error),
	}
	watcherNewFn = func() (*fsnotify.Watcher, error) { return stub, nil }
	watcherAddFn = func(w *fsnotify.Watcher, path string) error {
		return errors.New("permission denied")
	}
	watcherCloseFn = func(w *fsnotify.Watcher) error { return nil }

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("sandbox:\n  max_sandboxes: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	err := WatchAndReload(ctx, path)
	if err == nil {
		t.Fatal("expected error for non-inotify add failure")
	}
	if isInotifyLimitError(err) {
		t.Fatalf("expected non-inotify error, got: %v", err)
	}
}
