package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sandbox-gateway/internal/driver"
)

// CaptureSpec adopts a workload created before recreation inputs were stored.
// Bind mounts are preserved; anonymous volumes are intentionally recreated
// after Grasp has saved the workspace and are not copied as host paths.
func (d *Driver) CaptureSpec(ctx context.Context, spec driver.Spec) (driver.Spec, error) {
	raw, err := d.run(ctx, 10*time.Second, "inspect", d.containerName(spec.ID))
	if err != nil {
		return spec, err
	}
	var rows []struct {
		Config struct {
			Image string
			Env   []string
		}
		HostConfig struct{ Binds []string }
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil || len(rows) != 1 {
		return spec, fmt.Errorf("cannot read original container specification")
	}
	spec.Image, spec.Mounts = rows[0].Config.Image, rows[0].HostConfig.Binds
	spec.Config = nil // HostConfig.Binds includes the original config mount
	spec.Env = make(map[string]string)
	for _, entry := range rows[0].Config.Env {
		if key, value, ok := strings.Cut(entry, "="); ok {
			spec.Env[key] = value
		}
	}
	if spec.Image == "" {
		return spec, fmt.Errorf("original container image missing")
	}
	if path := spec.Env["WORKSPACE_DIR"]; path != "" {
		spec.WorkspaceDir = path
	}
	return spec, nil
}
