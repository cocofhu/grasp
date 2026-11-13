package runtime

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestSandboxResourcesText(t *testing.T) {
	if got := sandboxResourcesText(0); got != "" {
		t.Fatalf("no limit: got %q, want empty", got)
	}
	got := sandboxResourcesText(8192)
	for _, want := range []string{
		"## 沙箱资源",
		"8192 MiB",
		"串行执行",
		"--max-old-space-size` 不超过 3072",
		"--maxWorkers=2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPromptMemoryPrefersGatewayResolvedLimit(t *testing.T) {
	previous := sandbox.DefaultMemoryMB()
	t.Cleanup(func() { sandbox.SetDefaultMemoryMB(previous) })
	sandbox.SetDefaultMemoryMB(0)
	if got := promptMemoryMB(nil); got != 0 {
		t.Fatalf("no sandbox and no override: got %d", got)
	}
	if got := promptMemoryMB(&sandbox.Sandbox{MemoryMB: 3072}); got != 3072 {
		t.Fatalf("gateway-resolved limit ignored: got %d", got)
	}
	sandbox.SetDefaultMemoryMB(8192)
	if got := promptMemoryMB(&sandbox.Sandbox{}); got != 8192 {
		t.Fatalf("unreported limit must fall back to the override: got %d", got)
	}
	if got := promptMemoryMB(&sandbox.Sandbox{MemoryMB: 4096}); got != 4096 {
		t.Fatalf("actual sandbox limit must win: got %d", got)
	}
}
