package modelconnection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/backend/common"
)

const currentOpenCodeCatalog = `{"opencode":{"models":{"current":{"id":"current"},"retired":{"id":"retired","status":"deprecated"},"preview":{"id":"preview","status":"alpha"},"beta":{"id":"beta","status":"beta"},"invalid":null}},"opencode-go":{"models":{"go-model":{"id":"go-model"}}}}`

type catalogFixture struct {
	p                         Prepared
	binary, path, data, calls string
}

func newCatalogFixture(t *testing.T, script string) catalogFixture {
	t.Helper()
	root := t.TempDir()
	f := catalogFixture{binary: filepath.Join(root, "opencode"), path: filepath.Join(root, "cache", "opencode", "models.json"), data: filepath.Join(root, "source.json"), calls: filepath.Join(root, "calls")}
	f.p = Prepared{Platform: true, Model: "opencode/current", Env: []string{
		"HOME=" + filepath.Join(root, "home"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"GRASP_CATALOG_TEST_CACHE=" + f.path, "GRASP_CATALOG_TEST_DATA=" + f.data, "GRASP_CATALOG_TEST_CALLS=" + f.calls,
	}}
	f.write(t, f.data, currentOpenCodeCatalog)
	f.write(t, f.binary, `#!/bin/sh
set -eu
[ "$1" = models ]
[ "$3" = --refresh ]
printf 'refresh\n' >> "$GRASP_CATALOG_TEST_CALLS"
`+script)
	if err := os.Chmod(f.binary, 0o700); err != nil {
		t.Fatal(err)
	}
	return f
}

const refreshCatalogScript = `/bin/mkdir -p "${GRASP_CATALOG_TEST_CACHE%/*}"
/bin/cp "$GRASP_CATALOG_TEST_DATA" "$GRASP_CATALOG_TEST_CACHE"
`

func (f catalogFixture) write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f catalogFixture) expire(t *testing.T) {
	t.Helper()
	old := time.Now().Add(-2 * openCodeCatalogTTL)
	if err := os.Chtimes(f.path, old, old); err != nil {
		t.Fatal(err)
	}
}

func (f catalogFixture) count(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(f.calls)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "refresh\n")
}

func (f catalogFixture) check() error {
	return CheckOpenCodeCatalog(context.Background(), f.binary, f.p, "")
}

func TestOpenCodeCatalogRefreshThenReuseAndExpiry(t *testing.T) {
	f := newCatalogFixture(t, refreshCatalogScript)
	if err := f.check(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := f.check(); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("fresh catalog refreshed %d times", got)
	}
	f.expire(t)
	// A completed successful refresh does not throttle the next stale cache.
	f.write(t, f.data, `{"opencode":{"models":{"new-model":{"id":"new-model"}}}}`)
	f.p.Model = "opencode/new-model"
	if err := f.check(); err != nil {
		t.Fatal(err)
	}
	if got := f.count(t); got != 2 {
		t.Fatalf("expired catalog was not updated: %d refreshes", got)
	}
}

func TestOpenCodeCatalogConcurrentRefresh(t *testing.T) {
	f := newCatalogFixture(t, refreshCatalogScript)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if err := f.check(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if got := f.count(t); got != 1 {
		t.Fatalf("concurrent callers refreshed %d times", got)
	}
}

func TestOpenCodeCatalogSwallowedRefreshFailureUsesValidStaleCache(t *testing.T) {
	f := newCatalogFixture(t, "printf 'Models cache refreshed\\n'\n")
	f.write(t, f.path, currentOpenCodeCatalog)
	f.expire(t)
	for range 3 {
		if err := f.check(); err != nil {
			t.Fatalf("usable stale model rejected: %v", err)
		}
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("failed refresh was not throttled: %d attempts", got)
	}
	f.p.Model = "opencode/missing"
	if err := f.check(); err == nil || !strings.Contains(err.Error(), "automatic update did not succeed") {
		t.Fatalf("unknown model accepted from stale catalog: %v", err)
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("unsupported stale model bypassed refresh throttle: %d attempts", got)
	}
}

func TestOpenCodeCatalogSwallowedFailureWithoutCache(t *testing.T) {
	f := newCatalogFixture(t, "printf 'synthetic-secret Models cache refreshed\\n'\nprintf 'synthetic-secret\\n' >&2\n")
	for range 2 {
		err := f.check()
		if err == nil || !strings.Contains(err.Error(), "could not be updated automatically") || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatalf("refresh success banner trusted or command output exposed: %v", err)
		}
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("missing cache retried each call: %d", got)
	}
}

func TestOpenCodeCatalogSelectedModelValidation(t *testing.T) {
	for _, tc := range []struct {
		model, experimental, want string
	}{
		{"opencode/current", "", ""},
		{"opencode/current#high", "", ""},
		{"opencode/beta", "", ""},
		{"opencode/retired", "", "retired"},
		{"opencode/missing", "", "unavailable"},
		{"opencode/preview", "false", "unavailable"},
		{"opencode/preview", "true", ""},
		{"opencode/preview", "1", ""},
		{"opencode/invalid", "", "unavailable"},
		{"", "", ""},
		{"auto", "", ""},
	} {
		t.Run(tc.model+"/"+tc.experimental, func(t *testing.T) {
			f := newCatalogFixture(t, "exit 99\n")
			f.write(t, f.path, currentOpenCodeCatalog)
			f.p.Model = tc.model
			f.p.Env = append(f.p.Env, "OPENCODE_ENABLE_EXPERIMENTAL_MODELS="+tc.experimental)
			err := f.check()
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("model %q: %v; want %q", tc.model, err, tc.want)
			}
			wantRefreshes := 0
			if tc.model == "opencode/missing" {
				wantRefreshes = 1
			}
			if got := f.count(t); got != wantRefreshes {
				t.Fatalf("fresh catalog caused %d refreshes; want %d", got, wantRefreshes)
			}
		})
	}
}

func TestOpenCodeCatalogFreshCacheMissingNewModelRefreshesOnce(t *testing.T) {
	f := newCatalogFixture(t, refreshCatalogScript)
	f.write(t, f.path, currentOpenCodeCatalog)
	f.write(t, f.data, `{"opencode":{"models":{"new-model":{"id":"new-model"}}}}`)
	f.p.Model = "opencode/new-model"
	if err := f.check(); err != nil {
		t.Fatalf("new model rejected due to otherwise fresh catalog: %v", err)
	}
	if got := f.count(t); got != 1 {
		t.Fatalf("new model did not cause one refresh: %d", got)
	}
	f.p.Model = "opencode/unavailable"
	for range 3 {
		if err := f.check(); err == nil || !strings.Contains(err.Error(), "unavailable") {
			t.Fatalf("unknown model accepted after successful native refresh: %v", err)
		}
	}
	if got := f.count(t); got != 2 {
		t.Fatalf("repeated missing model requests caused refresh loop: %d", got)
	}
}

func TestOpenCodeCatalogEmptyPrimaryUsesConfiguredFamily(t *testing.T) {
	f := newCatalogFixture(t, "exit 99\n")
	f.write(t, f.path, `{"opencode-go":{"models":{"go-model":{"id":"go-model"}}}}`)
	f.p.Model = ""
	f.p.Env = append(f.p.Env, `GRASP_CONFIGURED_MODELS=[{"id":""},{"id":"opencode-go/go-model"}]`)
	if err := f.check(); err != nil {
		t.Fatal(err)
	}
	f.write(t, f.path, `{"opencode-go":{"models":{}}}`)
	if err := f.check(); err == nil || !strings.Contains(err.Error(), "no available models") {
		t.Fatalf("empty native provider accepted for automatic selection: %v", err)
	}
}

func TestOpenCodeCatalogDetectsUpdatedFile(t *testing.T) {
	f := newCatalogFixture(t, "exit 99\n")
	f.write(t, f.path, currentOpenCodeCatalog)
	if err := f.check(); err != nil {
		t.Fatal(err)
	}
	f.write(t, f.path, `{"opencode":{"models":{"current":{"id":"current","status":"deprecated"}}}}`)
	if err := f.check(); err == nil || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("cached parsed models hid catalog changes: %v", err)
	}
}

func TestOpenCodeCatalogInvalidFilesRefresh(t *testing.T) {
	for _, data := range []string{"{", "null", "{}", "[]"} {
		t.Run(data, func(t *testing.T) {
			f := newCatalogFixture(t, refreshCatalogScript)
			f.write(t, f.path, data)
			if err := f.check(); err != nil {
				t.Fatal(err)
			}
			if got := f.count(t); got != 1 {
				t.Fatalf("corrupt cache not refreshed: %d", got)
			}
		})
	}
}

func TestOpenCodeCatalogPathMatchesNativeEnvironment(t *testing.T) {
	for _, tc := range []struct {
		env  []string
		want string
	}{
		{[]string{"HOME=/sandbox/user"}, "/sandbox/user/.cache/opencode/models.json"},
		{[]string{"HOME=/sandbox/user", "OPENCODE_TEST_HOME=/temporary/config"}, "/sandbox/user/.cache/opencode/models.json"},
		{[]string{"HOME=/sandbox/user", "XDG_CACHE_HOME=/sandbox/custom"}, "/sandbox/custom/opencode/models.json"},
		{[]string{"HOME=/sandbox/user", "XDG_CACHE_HOME=relative"}, "/sandbox/user/.cache/opencode/models.json"},
	} {
		got, err := openCodeCatalogPath(tc.env)
		if err != nil || got != tc.want {
			t.Fatalf("cache path %v: got %q, %v; want %q", tc.env, got, err, tc.want)
		}
	}
	// Different sandbox homes must not reuse another sandbox's failed attempt.
	for range 2 {
		f := newCatalogFixture(t, refreshCatalogScript)
		home := filepath.Join(t.TempDir(), "home")
		f.p.Env = common.UpsertEnv(f.p.Env, "HOME", home)
		f.p.Env = common.UpsertEnv(f.p.Env, "XDG_CACHE_HOME", "")
		f.path = filepath.Join(home, ".cache", "opencode", "models.json")
		f.p.Env = common.UpsertEnv(f.p.Env, "GRASP_CATALOG_TEST_CACHE", f.path)
		if err := f.check(); err != nil || f.count(t) != 1 {
			t.Fatalf("HOME-scoped native refresh failed: %v", err)
		}
	}
}

func TestOpenCodeCatalogCancelledRefreshAllowsImmediateRetry(t *testing.T) {
	f := newCatalogFixture(t, `if [ ! -f "$GRASP_CATALOG_TEST_DATA.ready" ]; then
  exec /bin/sleep 20
fi
`+refreshCatalogScript)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- CheckOpenCodeCatalog(ctx, f.binary, f.p, "") }()
	deadline := time.Now().Add(3 * time.Second)
	for f.count(t) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.count(t) == 0 {
		t.Fatal("refresh did not start")
	}
	// Another caller can cancel while it waits for this cache's refresh lock.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer waitCancel()
	if err := CheckOpenCodeCatalog(waitCtx, f.binary, f.p, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled cache waiter blocked: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("refresh cancellation hidden: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("refresh did not stop on cancellation")
	}
	f.write(t, f.data+".ready", "ready")
	if err := f.check(); err != nil {
		t.Fatalf("cancelled refresh throttled a legitimate retry: %v", err)
	}
	if got := f.count(t); got != 2 {
		t.Fatalf("retry did not start: %d refresh attempts", got)
	}
}

func TestOpenCodeCatalogConnectionModeDoesNotDiscoverNativeModels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckOpenCodeCatalog(ctx, "/nonexistent-opencode", Prepared{}, ""); err != nil {
		t.Fatalf("connected service ran native discovery: %v", err)
	}
}
