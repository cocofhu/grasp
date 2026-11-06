package docker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"sandbox-gateway/internal/driver"
)

// PrepareStart updates the bootstrap in the existing, stopped container. This
// also migrates old images whose entrypoint always fetches an expired URL.
func (d *Driver) PrepareStart(ctx context.Context, id string, opts driver.StartOptions) error {
	st, err := d.Status(ctx, id)
	if err != nil {
		return err
	}
	if st != driver.StatusStopped {
		return fmt.Errorf("bootstrap repair requires a stopped container (status=%s)", st)
	}
	if !strings.HasPrefix(opts.BootstrapScript, "#!/bin/bash\n") || opts.RuntimeURL == "" {
		return fmt.Errorf("invalid resume bootstrap")
	}
	file, err := os.CreateTemp("", "sandbox-resume-*.sh")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	// Docker cannot change a stopped container's environment. Keep the fresh
	// inputs inside its root-only bootstrap, never in docker's command arguments.
	content := "#!/bin/bash\nexport GRASP_RUNTIME_URL=" + quote(opts.RuntimeURL) +
		"\nexport SANDBOX_INJECT_HEADERS=" + quote(opts.RuntimeHeaders) +
		"\nexport SANDBOX_INJECT=''\n" + strings.TrimPrefix(opts.BootstrapScript, "#!/bin/bash\n")
	_, writeErr := file.WriteString(content)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(file.Name(), 0o700); err != nil {
		return err
	}
	_, err = d.run(ctx, 30*time.Second, "cp", file.Name(), d.containerName(id)+":/grasp-bootstrap.sh")
	return err
}
