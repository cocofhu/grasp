package execution

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestImageAndCIContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	read := func(rel string) string {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	slim := read("server/Dockerfile")
	if !strings.Contains(slim, "GRASP_EXECUTION_MODE=sandbox") || strings.Contains(slim, "install-agent.sh") {
		t.Fatal("slim image must declare sandbox mode and stay free of agent CLIs")
	}
	wt := read("server/Dockerfile.worktree")
	for _, want := range []string{
		"install-agent.sh",
		"GRASP_EXECUTION_MODE=worktree",
		`ENTRYPOINT ["/app/grasp-server"]`,
		"NODE_VERSION=24.13.0",
		"PLAYWRIGHT_VERSION=1.61.1",
		"xvfb",
		"chrome-devtools-mcp",
		"openjdk-11-jdk",
		"GO_VERSION=1.26.10",
	} {
		if !strings.Contains(wt, want) {
			t.Fatalf("worktree image missing %s", want)
		}
	}
	for _, banned := range []string{"docker-ce", "openssh-server", "code-server"} {
		if strings.Contains(wt, banned) {
			t.Fatalf("worktree image mentions %s", banned)
		}
	}
	sbx := read("sandbox-gateway/sandbox/Dockerfile")
	for _, want := range []string{"docker-ce", "openssh-server", "code-server"} {
		if !strings.Contains(sbx, want) {
			t.Fatalf("sandbox image lost %s", want)
		}
	}
	ci := read(".github/workflows/ci.yml")
	if !strings.Contains(ci, "needs: [gate, changes, go-hygiene, go-race, server-image, worktree-image]") {
		t.Fatal("ci-required does not wait for worktree-image")
	}
	if !strings.Contains(ci, `contains(needs.*.result, 'failure')`) {
		t.Fatal("ci-required no longer fails when a needed job fails")
	}
	if !strings.Contains(ci, "sandbox-gateway/sandbox/scripts/install-agent.sh") {
		t.Fatal("worktree path filter does not include install-agent.sh")
	}
	filterStart := strings.Index(ci, "            worktree_image:")
	filterEnd := strings.Index(ci, "  go-hygiene:")
	if filterStart < 0 || filterEnd < filterStart || !strings.Contains(ci[filterStart:filterEnd], "sandbox-gateway/sandbox/**") {
		t.Fatal("worktree path filter does not cover the sandbox backend")
	}
	section := ci[strings.Index(ci, "\n  worktree-image:"):strings.Index(ci, "\n  ci-required:")]
	for _, want := range []string{"timeout-minutes: 180", "push: false", "scope=grasp-worktree-image", "check-worktree-image.sh"} {
		if !strings.Contains(section, want) {
			t.Fatalf("worktree-image job missing %s", want)
		}
	}
	if strings.Contains(section, "push: true") {
		t.Fatal("worktree-image job pushes")
	}
	pub := read(".github/workflows/publish-image.yml")
	if !strings.Contains(pub, "images: ghcr.io/cocofhu/grasp\n") || !strings.Contains(pub, "images: ghcr.io/cocofhu/grasp-worktree\n") {
		t.Fatal("publish-image tags drifted")
	}
	if strings.Contains(read(".github/workflows/publish-sandbox.yml"), "grasp-worktree") {
		t.Fatal("publish-sandbox was changed to include the worktree image")
	}
	if strings.Contains(read(".github/workflows/release-smoke.yml"), "grasp-worktree") {
		t.Fatal("release-smoke picked up the worktree image")
	}
	check := read("scripts/check-worktree-image.sh")
	for _, want := range []string{"cursor-agent", "claude", "codebuddy", "traecli", "opencode", "codex", "dsh", "dockerd", "sshd", "code-server"} {
		if !strings.Contains(check, want) {
			t.Fatalf("check script missing %s", want)
		}
	}
}
