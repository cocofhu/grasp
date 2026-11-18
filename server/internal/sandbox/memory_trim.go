package sandbox

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// MemoryTrimResult reports one guest page-cache trim. Skipped is empty when
// the trim ran; otherwise it names why ("cache" below threshold, "unsupported").
type MemoryTrimResult struct {
	Skipped   string `json:"skipped"`
	CachedKiB int64  `json:"cachedKiB"`
	FreedKiB  int64  `json:"freedKiB"`
}

// TrimMemory drops the guest's clean page cache and compacts free memory so a
// VM guest's free page reporting can return whole blocks to the host. Only
// call it for VM guests: inside a container /proc/sys/vm affects the host.
// Writes are flushed first; process memory is untouched.
func (s *Sandbox) TrimMemory(ctx context.Context, minCacheMiB int) (MemoryTrimResult, error) {
	var res MemoryTrimResult
	cmd, err := newSafeCmd("sh", "-s", "--", strconv.Itoa(max(minCacheMiB, 0)))
	if err != nil {
		return res, err
	}
	out, err := s.creds().runInput(ctx, 2*time.Minute, cmd, strings.NewReader(memoryTrimScript))
	if err != nil {
		return res, err
	}
	err = json.Unmarshal(out, &res)
	return res, err
}

// Proactive compaction (the kernel default 20) keeps free memory reportable
// between trims. Low CPU/IO priority keeps the trim behind any user work.
const memoryTrimScript = `set -eu
if [ ! -w /proc/sys/vm/drop_caches ] || [ ! -w /proc/sys/vm/compact_memory ]; then
  echo '{"skipped":"unsupported"}'; exit 0
fi
if [ -w /proc/sys/vm/compaction_proactiveness ]; then echo 20 > /proc/sys/vm/compaction_proactiveness; fi
meminfo() { awk -v k="$1:" '$1 == k { print $2 }' /proc/meminfo; }
cached=$(meminfo Cached)
if [ "$cached" -lt $(( $1 * 1024 )) ]; then
  printf '{"skipped":"cache","cachedKiB":%s}\n' "$cached"; exit 0
fi
before=$(meminfo MemFree)
low=""
if command -v ionice >/dev/null 2>&1; then low="ionice -c3"; fi
$low nice -n 19 sh -c 'sync; echo 1 > /proc/sys/vm/drop_caches; echo 1 > /proc/sys/vm/compact_memory'
after=$(meminfo MemFree)
printf '{"cachedKiB":%s,"freedKiB":%s}\n' "$cached" $(( after - before ))
`
