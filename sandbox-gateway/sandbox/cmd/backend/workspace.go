package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"backend/internal/workspacebinding"
	"backend/internal/workspacejob"
	"backend/internal/workspacesnapshot"
)

var errWorkspaceStandby = errors.New("workspace awaits platform provider identity")
var workspaceRuntimeID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,128}$`)

// Startup alone chooses these roots and the retained external lineage. An HTTP
// request can select relative files, but cannot initialize or rebind this owner.
func newWorkspaceOperations(getenv func(string) string, authenticated bool) (*workspacesnapshot.Controller, error) {
	lineage := strings.TrimSpace(getenv("GRASP_EXECUTION_LINEAGE"))
	control := strings.TrimSpace(getenv("GRASP_WORKSPACE_CONTROL_ROOT"))
	restore := strings.TrimSpace(getenv("GRASP_WORKSPACE_RESTORE_ROOT"))
	if lineage == "" && control == "" && restore == "" {
		return nil, nil
	}
	resource := strings.TrimSpace(getenv("GRASP_SANDBOX_RESOURCE_ID"))
	workspace := strings.TrimSpace(getenv("WORKSPACE_DIR"))
	limits, err := workspaceRuntimeLimits(getenv("GRASP_WORKSPACE_FILE_LIMITS"))
	if err != nil {
		return nil, err
	}
	if authenticated && workspaceRuntimeID.MatchString(lineage) && resource == "" && control == "" && restore == "" && filepath.IsAbs(workspace) && getenv("GRASP_WORKSPACE_FILE_LIMITS") != "" {
		return nil, errWorkspaceStandby
	}
	if !authenticated || lineage == "" || resource == "" || !filepath.IsAbs(workspace) || !filepath.IsAbs(control) || !filepath.IsAbs(restore) {
		return nil, fmt.Errorf("workspace operations require authenticated startup, absolute roots and retained execution identity")
	}
	workspace, control, restore = filepath.Clean(workspace), filepath.Clean(control), filepath.Clean(restore)
	original, err := workspacebinding.StartupOriginalRoot(filepath.Join(control, "workspace-bindings"), restore, workspace, resource, lineage)
	if err != nil {
		return nil, err
	}
	workspace = original
	if pathsOverlap(control, workspace) || pathsOverlap(restore, workspace) || pathsOverlap(control, restore) {
		return nil, fmt.Errorf("workspace control and restore roots must be separate")
	}
	private := []string{}
	for _, root := range []string{control, restore, getenv("CONFIG_ROOT"), getenv("CODEX_HOME")} {
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(workspace, filepath.Clean(root))
		if err != nil {
			return nil, err
		}
		if rel == "." {
			return nil, fmt.Errorf("workspace cannot be a private runtime configuration root")
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			private = append(private, filepath.ToSlash(rel))
		}
	}
	for _, root := range []string{control, restore} {
		if err := os.MkdirAll(root, 0700); err != nil {
			return nil, err
		}
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			return nil, fmt.Errorf("workspace operation roots must be private real directories")
		}
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil || resolved != root {
			return nil, fmt.Errorf("workspace operation roots cannot traverse symbolic links")
		}
	}
	return workspacesnapshot.NewController(workspacesnapshot.ControllerConfig{
		Engine:      workspacesnapshot.Config{Root: workspace, PrivatePaths: private, Limits: limits},
		Jobs:        workspacejob.Config{Root: control, Resource: resource, Lineage: lineage},
		RestoreRoot: restore,
		Binding:     &workspacebinding.Config{ControlRoot: filepath.Join(control, "workspace-bindings"), OriginalRoot: workspace, RestoreRoot: restore, Resource: resource, Lineage: lineage},
	})
}

func workspaceRuntimeLimits(raw string) (workspacesnapshot.Limits, error) {
	if raw == "" {
		return workspacesnapshot.DefaultLimits(), nil
	}
	var limits workspacesnapshot.Limits
	if len(raw) > 16<<10 {
		return limits, fmt.Errorf("workspace limits are invalid")
	}
	d := json.NewDecoder(bytes.NewBufferString(raw))
	d.DisallowUnknownFields()
	if d.Decode(&limits) != nil {
		return limits, fmt.Errorf("workspace limits are invalid")
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return limits, fmt.Errorf("workspace limits are invalid")
	}
	for _, n := range []int64{limits.MaxFileBytes, limits.MaxBytes, limits.MaxGitBytes, limits.MaxArchiveBytes, limits.MaxManifestBytes, limits.MaxSpoolBytes} {
		if n < 1 || n > 1<<50 {
			return limits, fmt.Errorf("workspace limits are invalid")
		}
	}
	if limits.MaxFileBytes > limits.MaxBytes || limits.MaxArchiveBytes > limits.MaxSpoolBytes || limits.MaxEntries < 1 || limits.MaxEntries > 1000000 || limits.MaxScanEntries < limits.MaxEntries || limits.MaxScanEntries > 10000000 || limits.MaxRepositories < 1 || limits.MaxRepositories > 256 {
		return limits, fmt.Errorf("workspace limits are invalid")
	}
	return limits, nil
}

func pathsOverlap(a, b string) bool {
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
