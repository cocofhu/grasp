package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/workspacejob"
	"backend/internal/workspacesnapshot"
)

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
	if !authenticated || lineage == "" || resource == "" || !filepath.IsAbs(workspace) || !filepath.IsAbs(control) || !filepath.IsAbs(restore) {
		return nil, fmt.Errorf("workspace operations require authenticated startup, absolute roots and retained execution identity")
	}
	workspace, control, restore = filepath.Clean(workspace), filepath.Clean(control), filepath.Clean(restore)
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
		Engine:      workspacesnapshot.Config{Root: workspace, PrivatePaths: private},
		Jobs:        workspacejob.Config{Root: control, Resource: resource, Lineage: lineage},
		RestoreRoot: restore,
	})
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
