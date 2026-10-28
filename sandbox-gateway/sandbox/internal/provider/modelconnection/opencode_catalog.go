package modelconnection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend/internal/backend/common"
)

const openCodeCatalogTTL = 5 * time.Minute

type openCodeCatalogModel struct {
	Status string `json:"status"`
}

type openCodeCatalog map[string]struct {
	Models map[string]*openCodeCatalogModel `json:"models"`
}

type openCodeCatalogState struct {
	gate        chan struct{}
	lastAttempt time.Time
	mtime       time.Time
	size        int64
	parsed      openCodeCatalog
}

var openCodeCatalogStates sync.Map

// CheckOpenCodeCatalog waits for native discovery before an official model is
// used. Native refresh failures are swallowed by the CLI, so the actual cache,
// rather than the command's exit status or success banner, is authoritative.
// Connected services own their model definitions and need no native discovery.
func CheckOpenCodeCatalog(ctx context.Context, binary string, p Prepared, cwd string) error {
	if !p.Platform {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	configured, err := configuredOfficialModelIDs(p.Env)
	if err != nil {
		return err
	}
	model := stripVariant(strings.TrimSpace(p.Model))
	if model == "auto" {
		model = ""
	}
	family, err := officialProviderID(model, configured)
	if err != nil {
		return err
	}
	path, err := openCodeCatalogPath(p.Env)
	if err != nil {
		return err
	}
	value, _ := openCodeCatalogStates.LoadOrStore(path, &openCodeCatalogState{gate: make(chan struct{}, 1)})
	state := value.(*openCodeCatalogState)
	select {
	case state.gate <- struct{}{}:
		defer func() { <-state.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	catalog, fresh := state.read(path)
	id := strings.TrimPrefix(model, family+"/")
	entry, exists := catalog[family].Models[id]
	// A newly released model can reach Grasp before this otherwise fresh cache.
	// Refresh a missing ID once, while known retired/disabled entries fail fast.
	missing := model != "" && !exists
	if (!fresh || missing) && time.Since(state.lastAttempt) >= openCodeCatalogTTL {
		state.lastAttempt = time.Now()
		refreshCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		cmd := exec.CommandContext(refreshCtx, binary, "models", family, "--refresh")
		cmd.Env, cmd.Dir = p.Env, cwd
		// No CLI output is copied into errors: it can contain credentials and
		// provider configuration. Nil output streams use os.DevNull directly,
		// so descendants cannot keep a capture pipe open after cancellation.
		// Failed refreshes preserve a usable old cache.
		_ = cmd.Run()
		cancel()
		if err := ctx.Err(); err != nil {
			// A cancelled connection attempt must not delay the next connection.
			state.lastAttempt = time.Time{}
			return err
		}
		state.parsed = nil
		catalog, fresh = state.read(path)
		entry, exists = catalog[family].Models[id]
		if fresh && (model == "" || exists && openCodeCatalogModelActive(entry, p.Env)) {
			state.lastAttempt = time.Time{}
		}
	}
	if catalog == nil {
		return errors.New("OpenCode model catalog could not be updated automatically; retry later or check the sandbox's connection to the OpenCode model catalog")
	}
	if model != "" {
		if exists && openCodeCatalogModelActive(entry, p.Env) {
			return nil
		}
		if exists && entry != nil && entry.Status == "deprecated" {
			return fmt.Errorf("OpenCode model %q has been retired; select an available model in Grasp's model settings", model)
		}
		if !fresh {
			return fmt.Errorf("OpenCode model %q is unavailable in its cached catalog and automatic update did not succeed; retry later or select an available model in Grasp's model settings", model)
		}
		return fmt.Errorf("OpenCode model %q is unavailable in its current catalog; select an available model in Grasp's model settings", model)
	}
	for _, entry := range catalog[family].Models {
		if openCodeCatalogModelActive(entry, p.Env) {
			return nil
		}
	}
	return fmt.Errorf("OpenCode has no available models for %q; check the selected model family in Grasp's model settings", family)
}

func openCodeCatalogModelActive(model *openCodeCatalogModel, env []string) bool {
	if model == nil || model.Status == "deprecated" {
		return false
	}
	if model.Status != "alpha" {
		return true
	}
	flag := strings.ToLower(common.GetEnvValue(env, "OPENCODE_ENABLE_EXPERIMENTAL_MODELS"))
	return flag == "true" || flag == "1"
}

func openCodeCatalogPath(env []string) (string, error) {
	cache := common.GetEnvValue(env, "XDG_CACHE_HOME")
	// xdg-basedir ignores relative XDG paths, as required by the XDG spec.
	if !filepath.IsAbs(cache) {
		home := common.GetEnvValue(env, "HOME")
		if home == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", errors.New("OpenCode model catalog: cannot locate the sandbox cache directory")
			}
		}
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "opencode", "models.json"), nil
}

func (s *openCodeCatalogState) read(path string) (openCodeCatalog, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil, false
	}
	if s.parsed == nil || !info.ModTime().Equal(s.mtime) || info.Size() != s.size {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, false
		}
		var parsed openCodeCatalog
		if json.Unmarshal(data, &parsed) != nil || len(parsed) == 0 {
			return nil, false
		}
		s.parsed, s.mtime, s.size = parsed, info.ModTime(), info.Size()
	}
	age := time.Since(info.ModTime())
	return s.parsed, age >= 0 && age < openCodeCatalogTTL
}
