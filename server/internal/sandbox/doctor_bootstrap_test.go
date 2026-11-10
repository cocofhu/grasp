package sandbox

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func doctorBootstrapManager(t *testing.T) (*Manager, *BundleStore, []byte) {
	t.Helper()
	previous := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(previous) })
	config.StoreConfig(&config.Config{})
	path := filepath.Join(t.TempDir(), "runtime.tgz")
	data := writeRuntime(t, path, testVersion('d'), 1)
	store := NewBundleStore()
	return NewManager(nil, ManagerOptions{Runtime: NewRuntimeBundle(path), InjectStore: store, InjectAdvertise: "http://api.example.test"}), store, data
}

func TestDoctorBootstrapPublishesActualRuntimeAndOnlyRevokesItsOwnBundle(t *testing.T) {
	manager, store, runtime := doctorBootstrapManager(t)
	first, cleanup, err := manager.PrepareDoctorBootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	second, cleanupSecond, err := manager.PrepareDoctorBootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanupSecond)
	if first.Env[BridgePasswordEnv] == second.Env[BridgePasswordEnv] || first.Env["ROOT_PASSWORD"] == second.Env["ROOT_PASSWORD"] || first.Env["SANDBOX_INJECT_HEADERS"] == second.Env["SANDBOX_INJECT_HEADERS"] {
		t.Fatal("diagnostic sessions reused startup credentials")
	}
	fetch := func(bootstrap DoctorBootstrap, authorized bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, bootstrap.Env["GRASP_RUNTIME_URL"], nil)
		if authorized {
			req.Header.Set("Authorization", strings.TrimPrefix(bootstrap.Env["SANDBOX_INJECT_HEADERS"], "Authorization: "))
		}
		w := httptest.NewRecorder()
		store.ServeHTTP(w, req, bundleIDFromURL(bootstrap.Env["GRASP_RUNTIME_URL"]))
		return w
	}
	if w := fetch(first, false); w.Code != http.StatusUnauthorized {
		t.Fatal("runtime publication was accessible without its private token", w.Code)
	}
	if w := fetch(first, true); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), runtime) {
		t.Fatal("diagnostic bootstrap did not serve the Manager runtime", w.Code)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(cleanup)
	}
	wg.Wait()
	if w := fetch(first, true); w.Code != http.StatusUnauthorized {
		t.Fatal("cleanup retained its runtime publication", w.Code)
	}
	if w := fetch(second, true); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), runtime) {
		t.Fatal("cleanup affected another diagnostic session", w.Code)
	}
}

func TestDoctorBootstrapRejectsUnavailableConfigurationWithoutLeakingBundles(t *testing.T) {
	for _, bad := range []string{"nil manager", "nil runtime", "missing runtime", "nil store", "no advertise", "invalid advertise", "canceled"} {
		t.Run(bad, func(t *testing.T) {
			manager, store, _ := doctorBootstrapManager(t)
			ctx := t.Context()
			switch bad {
			case "nil manager":
				manager = nil
			case "nil runtime":
				manager.runtime = nil
			case "missing runtime":
				manager.runtime = NewRuntimeBundle(filepath.Join(t.TempDir(), "missing.tgz"))
			case "nil store":
				manager.bundles = nil
			case "no advertise":
				manager.injectAdvertiseFallback = ""
			case "invalid advertise":
				manager.injectAdvertiseFallback = "file:///private"
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, cleanup, err := manager.PrepareDoctorBootstrap(ctx); err == nil || cleanup != nil || bad == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("unusable bootstrap was accepted", err)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.items) != 0 {
				t.Fatal("failed bootstrap leaked a runtime publication")
			}
		})
	}
}

func TestDoctorBootstrapValidationRejectsIncompleteOrAdditionalAuthorization(t *testing.T) {
	manager, _, _ := doctorBootstrapManager(t)
	bootstrap, cleanup, err := manager.PrepareDoctorBootstrap(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	for _, bad := range []string{"runtime", "url credential", "header", "password", "shared password", "workspace", "docker", "model credential"} {
		t.Run(bad, func(t *testing.T) {
			env := make(map[string]string)
			for key, value := range bootstrap.Env {
				env[key] = value
			}
			switch bad {
			case "runtime":
				delete(env, "GRASP_RUNTIME_URL")
			case "url credential":
				env["GRASP_RUNTIME_URL"] = "https://secret@api.example.test/sandbox-inject/owned.tgz"
			case "header":
				env["SANDBOX_INJECT_HEADERS"] += "\r\nX-Untrusted: value"
			case "password":
				env[BridgePasswordEnv] = "short"
			case "shared password":
				env["ROOT_PASSWORD"] = env[BridgePasswordEnv]
			case "workspace":
				env["WORKSPACE_DIR"] = "/user/worktree"
			case "docker":
				env["SKIP_INNER_DOCKER"] = "0"
			case "model credential":
				env["OPENAI_API_KEY"] = "untrusted"
			}
			if err := (DoctorBootstrap{Env: env}).Validate(); err == nil {
				t.Fatal("unsafe bootstrap was accepted")
			}
		})
	}
}
