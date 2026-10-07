package runtime

import (
	"strings"
	"testing"
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
