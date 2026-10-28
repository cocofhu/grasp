package deepseek

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/provider"
)

func writeUsageCache(t *testing.T, home, sessionID string, seq int64, totals usageTotals) {
	t.Helper()
	dir := filepath.Join(home, usageCacheDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"version": 7,
		"record": map[string]any{"rows": map[string]any{
			"tokenUsage": map[string]any{
				"ver": 2,
				"seq": seq,
				"val": map[string]any{"totals": totals},
			},
			// 真实缓存混有字符串/null 的投影行，不能让它们破坏 tokenUsage 解析。
			"title":       map[string]any{"ver": 1, "seq": seq, "val": "fixture title"},
			"workingDir":  map[string]any{"ver": 1, "seq": seq, "val": "/workspace"},
			"sandboxMode": map[string]any{"ver": 1, "seq": seq, "val": "workspace-write"},
			"todos":       map[string]any{"ver": 2, "seq": seq, "val": nil},
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".json.tmp"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, sessionID+".json.tmp"), filepath.Join(dir, sessionID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestUsageTrackerReportsPerTurnDelta(t *testing.T) {
	home := t.TempDir()
	tracker := newUsageTracker(home, "session-1", "vendor/model")
	if !tracker.enabled() {
		t.Fatal("tracker should be enabled")
	}

	tracker.begin() // 首轮前缓存尚未落盘：基线按 0 处理
	writeUsageCache(t, home, "session-1", 10, usageTotals{100, 20, 30, 5})
	want := map[string]provider.TokenUsage{"vendor/model": {
		InputTokens: 100, OutputTokens: 20, CacheReadTokens: 30, CacheWriteTokens: 5,
	}}
	if got := tracker.promptDone(); len(got) != 1 || got["vendor/model"] != want["vendor/model"] {
		t.Fatalf("first turn usage = %+v", got)
	}
	if got := tracker.turnUsage(); got["vendor/model"] != want["vendor/model"] {
		t.Fatalf("turnUsage = %+v", got)
	}

	tracker.begin()
	writeUsageCache(t, home, "session-1", 20, usageTotals{260, 45, 90, 9})
	if got := tracker.promptDone(); got["vendor/model"] != (provider.TokenUsage{
		InputTokens: 160, OutputTokens: 25, CacheReadTokens: 60, CacheWriteTokens: 4,
	}) {
		t.Fatalf("second turn usage = %+v", got)
	}
}

func TestUsageTrackerWaitsForAsyncCacheWrite(t *testing.T) {
	home := t.TempDir()
	writeUsageCache(t, home, "session-2", 10, usageTotals{50, 10, 5, 0})
	tracker := newUsageTracker(home, "session-2", "m")
	tracker.begin()

	go func() {
		time.Sleep(30 * time.Millisecond)
		writeUsageCache(t, home, "session-2", 20, usageTotals{80, 15, 15, 0})
	}()
	if got := tracker.promptDone(); got["m"] != (provider.TokenUsage{InputTokens: 30, OutputTokens: 5, CacheReadTokens: 10}) {
		t.Fatalf("async usage not picked up: %+v", got)
	}
}

func TestUsageTrackerNoChangeAndReset(t *testing.T) {
	home := t.TempDir()
	writeUsageCache(t, home, "session-3", 10, usageTotals{100, 20, 30, 5})
	tracker := newUsageTracker(home, "session-3", "m")

	tracker.begin()
	if got := tracker.promptDone(); got != nil {
		t.Fatalf("unchanged cache must not report usage: %+v", got)
	}

	tracker.begin()
	writeUsageCache(t, home, "session-3", 5, usageTotals{10, 2, 3, 0}) // 缓存重建/回退
	if got := tracker.promptDone(); got != nil {
		t.Fatalf("reset cache must not report negative usage: %+v", got)
	}
	tracker.begin() // 基线跟随回退后的值，下一轮正常
	writeUsageCache(t, home, "session-3", 6, usageTotals{25, 4, 6, 0})
	if got := tracker.promptDone(); got["m"] != (provider.TokenUsage{InputTokens: 15, OutputTokens: 2, CacheReadTokens: 3}) {
		t.Fatalf("usage after reset = %+v", got)
	}
}

func TestUsageTrackerMalformedAndUnsafeInputs(t *testing.T) {
	for _, tc := range []struct{ name, sessionID, body string }{
		{"missing row", "s", `{"record":{"rows":{}}}`},
		{"null val", "s", `{"record":{"rows":{"tokenUsage":{"ver":2,"seq":1,"val":null}}}}`},
		{"wrong version", "s", `{"record":{"rows":{"tokenUsage":{"ver":3,"seq":1,"val":{"totals":{"uncachedInputTokens":1}}}}}}`},
		{"negative", "s", `{"record":{"rows":{"tokenUsage":{"ver":2,"seq":1,"val":{"totals":{"uncachedInputTokens":-1}}}}}}`},
		{"invalid json", "s", `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, usageCacheDir)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "s.json"), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			tracker := newUsageTracker(home, tc.sessionID, "m")
			tracker.begin()
			if got := tracker.promptDone(); got != nil {
				t.Fatalf("malformed cache reported usage: %+v", got)
			}
		})
	}

	for _, sessionID := range []string{"", " ", "../escape", `a\b`} {
		if tracker := newUsageTracker(t.TempDir(), sessionID, "m"); tracker.enabled() {
			t.Fatalf("unsafe session id %q enabled the tracker", sessionID)
		}
	}
	if tracker := newUsageTracker("", "s", "m"); tracker.enabled() {
		t.Fatal("empty home enabled the tracker")
	}
}

func TestUsageTrackerMissingBaselineAfterFirstTurnSkips(t *testing.T) {
	home := t.TempDir()
	tracker := newUsageTracker(home, "session-5", "m")
	tracker.begin()
	writeUsageCache(t, home, "session-5", 1, usageTotals{100, 10, 0, 0})
	if got := tracker.promptDone(); got["m"].InputTokens != 100 {
		t.Fatalf("first turn = %+v", got)
	}
	path := filepath.Join(home, usageCacheDir, "session-5.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	tracker.begin() // 第二轮基线读不到：不能把整会话累计误当本轮增量
	writeUsageCache(t, home, "session-5", 2, usageTotals{300, 30, 0, 0})
	if got := tracker.promptDone(); got != nil {
		t.Fatalf("missing baseline must skip usage: %+v", got)
	}
	tracker.begin() // 基线已恢复为 300，下一轮正常差分
	writeUsageCache(t, home, "session-5", 3, usageTotals{320, 35, 0, 0})
	if got := tracker.promptDone(); got["m"].InputTokens != 20 || got["m"].OutputTokens != 5 {
		t.Fatalf("usage after baseline recovery = %+v", got)
	}
}

func TestUsageTrackerEmptyModelFallsBack(t *testing.T) {
	home := t.TempDir()
	tracker := newUsageTracker(home, "session-4", "")
	tracker.begin()
	writeUsageCache(t, home, "session-4", 1, usageTotals{1, 2, 3, 4})
	if got := tracker.promptDone(); got["default"] != (provider.TokenUsage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4}) {
		t.Fatalf("default bucket missing: %+v", got)
	}
}
