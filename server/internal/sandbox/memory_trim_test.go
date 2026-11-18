package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrimMemorySendsScriptAndParsesResult(t *testing.T) {
	t.Cleanup(SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		body, _ := io.ReadAll(stdin)
		if command != "'sh' '-s' '--' '1024'" || !strings.Contains(string(body), "drop_caches") {
			t.Fatalf("command=%q", command)
		}
		return []byte(`{"cachedKiB":4096000,"freedKiB":3072000}`), nil
	}))
	res, err := (&Sandbox{}).TrimMemory(context.Background(), 1024)
	if err != nil || res.Skipped != "" || res.FreedKiB != 3072000 || res.CachedKiB != 4096000 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	t.Cleanup(SetExecHook(func(context.Context, string, int, string, io.Reader) ([]byte, error) {
		return nil, errors.New("SSH failed")
	}))
	if _, err := (&Sandbox{}).TrimMemory(context.Background(), -5); err == nil {
		t.Fatal("transport failure must be reported")
	}
}

// The script runs against a fake /proc so its decisions and writes are real.
func TestMemoryTrimScript(t *testing.T) {
	run := func(t *testing.T, root string, minCacheMiB string) (MemoryTrimResult, error) {
		t.Helper()
		script := strings.ReplaceAll(memoryTrimScript, "/proc/", root+"/proc/")
		cmd := exec.Command("sh", "-s", "--", minCacheMiB)
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return MemoryTrimResult{}, errors.New(string(out))
		}
		var res MemoryTrimResult
		return res, json.Unmarshal(out, &res)
	}
	fakeProc := func(t *testing.T, cachedKiB string, writable bool) string {
		t.Helper()
		root := t.TempDir()
		vm := filepath.Join(root, "proc", "sys", "vm")
		if err := os.MkdirAll(vm, 0o755); err != nil {
			t.Fatal(err)
		}
		meminfo := "MemTotal: 16000000 kB\nMemFree: 1000 kB\nCached: " + cachedKiB + " kB\n"
		if err := os.WriteFile(filepath.Join(root, "proc", "meminfo"), []byte(meminfo), 0o644); err != nil {
			t.Fatal(err)
		}
		if writable {
			for _, f := range []string{"drop_caches", "compact_memory", "compaction_proactiveness"} {
				if err := os.WriteFile(filepath.Join(vm, f), []byte("0\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		return root
	}
	read := func(root, name string) string {
		b, _ := os.ReadFile(filepath.Join(root, "proc", "sys", "vm", name))
		return strings.TrimSpace(string(b))
	}

	t.Run("trims a large cache", func(t *testing.T) {
		root := fakeProc(t, "4194304", true)
		res, err := run(t, root, "1024")
		if err != nil || res.Skipped != "" || res.CachedKiB != 4194304 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if read(root, "drop_caches") != "1" || read(root, "compact_memory") != "1" || read(root, "compaction_proactiveness") != "20" {
			t.Fatal("trim must drop caches, compact memory and enable proactive compaction")
		}
	})
	t.Run("skips a small cache but enables proactive compaction", func(t *testing.T) {
		root := fakeProc(t, "524288", true)
		res, err := run(t, root, "1024")
		if err != nil || res.Skipped != "cache" || res.CachedKiB != 524288 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if read(root, "drop_caches") != "0" || read(root, "compaction_proactiveness") != "20" {
			t.Fatal("a skipped trim must not drop caches")
		}
	})
	t.Run("reports an unsupported guest", func(t *testing.T) {
		res, err := run(t, fakeProc(t, "4194304", false), "1024")
		if err != nil || res.Skipped != "unsupported" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
}
