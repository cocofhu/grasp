package deepseek

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"backend/internal/provider"
)

// DSH 的 ACP 响应目前只带 stopReason，不携带每轮 token 用量；但 DSH 会把
// tokenUsage 投影（整会话累计的四桶用量）持久化到会话投影缓存里。这里读取
// 固定版本 dsh 的缓存文件，按每轮前后差分得到本轮用量，再交给 prompt_done。
const (
	usageCacheDir           = "storages/session_projcache/sessions"
	usageCacheRetryDelay    = 20 * time.Millisecond
	usageCacheRetryAttempts = 5
)

// usageTotals 与 DSH tokenUsage 投影的累计字段一一对应。
type usageTotals struct {
	UncachedInputTokens int64 `json:"uncachedInputTokens"`
	OutputTokens        int64 `json:"outputTokens"`
	CacheReadTokens     int64 `json:"cacheReadTokens"`
	CacheWriteTokens    int64 `json:"cacheWriteTokens"`
}

func (u usageTotals) zero() bool { return u == usageTotals{} }

type usageSnapshot struct {
	known  bool
	seq    int64
	totals usageTotals
}

// usageTracker 按会话维护投影缓存的读取基线与本轮结果。
type usageTracker struct {
	path  string
	model string

	mu     sync.Mutex
	before usageSnapshot
	turn   map[string]provider.TokenUsage
	begins int
	warned bool
}

// newUsageTracker 返回一个可用 tracker；home/sessionID 不合法时返回禁用实例，
// 调用方据此保持“不上报用量”的旧行为。
func newUsageTracker(home, sessionID, model string) *usageTracker {
	home = strings.TrimSpace(home)
	sessionID = strings.TrimSpace(sessionID)
	if home == "" || sessionID == "" || filepath.Base(sessionID) != sessionID ||
		strings.ContainsAny(sessionID, `/\`) {
		return &usageTracker{}
	}
	return &usageTracker{
		path:  filepath.Join(home, usageCacheDir, sessionID+".json"),
		model: strings.TrimSpace(model),
	}
}

func (t *usageTracker) enabled() bool { return t != nil && t.path != "" }

// begin 在每轮 prompt 之前记录累计基线；缓存缺失时保持 unknown，
// 仅首轮在 promptDone 里按 0 处理（会话由本进程新建）。
func (t *usageTracker) begin() {
	if !t.enabled() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.turn = nil
	t.begins++
	t.before = t.read()
}

// promptDone 在 session/prompt 返回后计算本轮增量，并缓存给 TurnResult.Usage。
func (t *usageTracker) promptDone() map[string]provider.TokenUsage {
	if !t.enabled() {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	after := t.readAfterPrompt()
	if !after.known {
		return nil
	}
	before := t.before
	if !before.known {
		// 仅首轮可以把缺失基线当作 0：session/new 由本进程创建。后续轮次读不到
		// 基线时，整会话累计量会被误当成本轮增量，宁可不报。
		if t.begins > 1 {
			t.before = after
			return nil
		}
		before = usageSnapshot{known: true}
	}
	delta, ok := subtractUsage(after.totals, before.totals)
	t.before = after
	if !ok || delta.zero() {
		return nil
	}
	key := t.model
	if key == "" {
		key = "default"
	}
	t.turn = map[string]provider.TokenUsage{key: {
		InputTokens:      delta.UncachedInputTokens,
		OutputTokens:     delta.OutputTokens,
		CacheReadTokens:  delta.CacheReadTokens,
		CacheWriteTokens: delta.CacheWriteTokens,
	}}
	return t.turn
}

// turnUsage 返回本轮已经算出的用量（拷贝），供 provider.TurnResult 使用。
func (t *usageTracker) turnUsage() map[string]provider.TokenUsage {
	if !t.enabled() {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.turn) == 0 {
		return nil
	}
	out := make(map[string]provider.TokenUsage, len(t.turn))
	for k, v := range t.turn {
		out[k] = v
	}
	return out
}

// readAfterPrompt 读取本轮结束后的投影。turn/end 触发的缓存写入是异步的，
// 因此短暂重试等待累计序号推进；没有模型调用的回合会等满上限后按无用量处理。
func (t *usageTracker) readAfterPrompt() usageSnapshot {
	for attempt := 0; ; attempt++ {
		snap := t.read()
		if snap.known && (snap.seq != t.before.seq || snap.totals != t.before.totals) {
			return snap
		}
		if attempt >= usageCacheRetryAttempts {
			return snap
		}
		time.Sleep(usageCacheRetryDelay)
	}
}

func (t *usageTracker) read() usageSnapshot {
	snap, err := readUsageSnapshot(t.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) && !t.warned {
		t.warned = true
		log.Printf("dsh: 读取 token 用量缓存失败（%s），本轮不上报用量: %v", t.path, err)
	}
	return snap
}

type usageCacheFile struct {
	Record struct {
		// 其他投影（title/sandboxMode/workingDirectory 等）的 val 可能是字符串或 null，
		// 不能按 tokenUsage 的行结构整体反序列化；只取目标行再单独解析。
		Rows map[string]json.RawMessage `json:"rows"`
	} `json:"record"`
}

type usageCacheRow struct {
	Ver int   `json:"ver"`
	Seq int64 `json:"seq"`
	Val *struct {
		Totals usageTotals `json:"totals"`
	} `json:"val"`
}

// DSH tokenUsage 投影的 stateVersion；字段语义变化时整块拒读，避免把新格式误读成旧四桶。
const usageCacheStateVersion = 2

func readUsageSnapshot(path string) (usageSnapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return usageSnapshot{}, err
	}
	var file usageCacheFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return usageSnapshot{}, err
	}
	rawRow, ok := file.Record.Rows["tokenUsage"]
	if !ok {
		return usageSnapshot{}, nil
	}
	var row usageCacheRow
	if err := json.Unmarshal(rawRow, &row); err != nil {
		return usageSnapshot{}, err
	}
	if row.Val == nil || row.Ver != usageCacheStateVersion {
		return usageSnapshot{}, nil
	}
	totals := row.Val.Totals
	if totals.UncachedInputTokens < 0 || totals.OutputTokens < 0 ||
		totals.CacheReadTokens < 0 || totals.CacheWriteTokens < 0 {
		return usageSnapshot{}, nil
	}
	return usageSnapshot{known: true, seq: row.Seq, totals: totals}, nil
}

// subtractUsage 对四桶做差分；任一分量为负说明缓存被重建或回退，本轮放弃上报。
func subtractUsage(after, before usageTotals) (usageTotals, bool) {
	out := usageTotals{
		UncachedInputTokens: after.UncachedInputTokens - before.UncachedInputTokens,
		OutputTokens:        after.OutputTokens - before.OutputTokens,
		CacheReadTokens:     after.CacheReadTokens - before.CacheReadTokens,
		CacheWriteTokens:    after.CacheWriteTokens - before.CacheWriteTokens,
	}
	if out.UncachedInputTokens < 0 || out.OutputTokens < 0 ||
		out.CacheReadTokens < 0 || out.CacheWriteTokens < 0 {
		return usageTotals{}, false
	}
	return out, true
}
