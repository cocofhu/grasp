package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sandbox-gateway/internal/driver"
)

// Run with GRASP_DOCKER_RECOVERY_IMAGE set to a local Linux image containing
// bash, curl, coreutils and find (for example the CI cli-tools image). This uses
// a real stopped container and writable layer, without a vendor model or key.
func TestDockerResidentResumeIntegration(t *testing.T) {
	image := os.Getenv("GRASP_DOCKER_RECOVERY_IMAGE")
	if image == "" {
		t.Skip("set GRASP_DOCKER_RECOVERY_IMAGE to run real Docker recovery")
	}
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	bootstrapPath, err := filepath.Abs("../../../../sandbox/scripts/grasp-bootstrap.sh")
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := os.ReadFile(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	cache := filepath.Join(tmp, "bundle")
	if err := os.MkdirAll(filepath.Join(cache, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "scripts/startup.sh"), []byte("#!/bin/bash\nexec sleep infinity\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Build a valid cached runtime using the production checksum function.
	cmd := exec.Command("bash", "-c", `source <(sed -n '/^runtime_digest() {/,/^}/p' "$1"); runtime_digest "$2"`, "digest", bootstrapPath, cache)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("runtime digest: %v %s", err, out)
	}
	version := strings.TrimSpace(string(out))
	manifest := fmt.Sprintf("version=%s\narch=%s\nmin_image=2\n", version, runtime.GOARCH)
	if err := os.WriteFile(filepath.Join(cache, "MANIFEST"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeRoot := filepath.Join(tmp, "runtime")
	if err := os.Mkdir(runtimeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(cache, filepath.Join(runtimeRoot, version)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(version, filepath.Join(runtimeRoot, "current")); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(tmp, "legacy.sh")
	if err := os.WriteFile(legacy, []byte("#!/bin/bash\nset -e\ncurl -fsSL \"$GRASP_RUNTIME_URL\" >/dev/null\nexec /opt/grasp-runtime/current/scripts/startup.sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	level := filepath.Join(tmp, "image-level")
	if err := os.WriteFile(level, []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(tmp, "workspace")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "uncommitted-output.mp4"), []byte("irreplaceable user output"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("resident-recovery-%d", time.Now().UnixNano())
	name := "sbx-" + id
	docker("create", "--name", name, "--entrypoint", "/grasp-bootstrap.sh", "-e", "GRASP_RUNTIME_URL=http://127.0.0.1:1/expired", "-e", "GRASP_IMAGE_LEVEL_FILE=/image-level", image, "fetch")
	defer func() { _ = exec.Command("docker", "rm", "-f", "-v", name).Run() }()
	docker("cp", runtimeRoot, name+":/opt/grasp-runtime")
	docker("cp", workspace, name+":/root/workspace")
	docker("cp", legacy, name+":/grasp-bootstrap.sh")
	docker("cp", level, name+":/image-level")
	originalID := docker("inspect", "--format", "{{.Id}}", name)
	docker("start", name)
	docker("wait", name)
	d := New(Options{})
	if status, err := d.Status(context.Background(), id); err != nil || status != driver.StatusStopped {
		t.Fatalf("legacy container did not stop: %s %v", status, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := d.PrepareStart(context.Background(), id, driver.StartOptions{BootstrapScript: string(bootstrap), RuntimeURL: "http://127.0.0.1:1/still-expired"}); err != nil {
			t.Fatal(err)
		}
		if err := d.Start(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if mode := docker("exec", name, "stat", "-c", "%a", "/grasp-bootstrap.sh"); mode != "700" {
			t.Fatalf("bootstrap download credentials are not root-only: mode=%s", mode)
		}
		if got := docker("exec", name, "cat", "/root/workspace/uncommitted-output.mp4"); got != "irreplaceable user output" {
			t.Fatalf("workspace data changed: %q", got)
		}
		if got := docker("inspect", "--format", "{{.Id}}", name); got != originalID {
			t.Fatal("resume replaced the container")
		}
		if err := d.Stop(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
}
