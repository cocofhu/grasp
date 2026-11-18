#!/bin/bash
# Keeps an E2B guest's file page cache near GRASP_MEMORY_CAP_MIB. The host
# backs every guest page the guest has ever touched, and cache counts the same
# as process memory there; free page reporting returns what the guest frees,
# so capping cache keeps the host's share close to the guest's real use.
# Free page reporting only returns whole 2 MiB blocks, so new free memory
# broken into smaller blocks is compacted too. Only for VM guests: in a privileged
# container these knobs act on the host kernel.
#
# Usage: memory-cap.sh [run|once]
#   run (default) trims every GRASP_MEMORY_CAP_INTERVAL seconds; once trims once.
# GRASP_MEMORY_CAP_MIB=0 disables it.
set -u

CAP_MIB="${GRASP_MEMORY_CAP_MIB:-512}"
INTERVAL="${GRASP_MEMORY_CAP_INTERVAL:-10}"
FRAG_MIB="${GRASP_MEMORY_CAP_FRAG_MIB:-512}"
COMPACT_EVERY="${GRASP_MEMORY_CAP_COMPACT_EVERY:-60}"
PROC="${GRASP_MEMORY_CAP_PROC:-/proc}"
RECLAIM="${GRASP_MEMORY_CAP_RECLAIM:-/sys/fs/cgroup/memory.reclaim}"
SLACK_MIB=64
last_compact=0
frag_floor=0

supported() {
  [ "${E2B_SANDBOX:-}" = true ] && [ "$CAP_MIB" -gt 0 ] && [ -w "$RECLAIM" ]
}

file_cache_mib() {
  awk '/^(Active|Inactive)\(file\):/ { s += $2 } END { print int(s / 1024) }' "$PROC/meminfo"
}

# Free memory in blocks below order 9 (2 MiB with 4 KiB pages), which free
# page reporting cannot return. buddyinfo columns start at order 0 in field 5.
fragmented_free_mib() {
  awk '{ for (i = 5; i <= NF && i < 14; i++) s += $i * 2 ^ (i - 5) * 4 } END { print int(s / 1024) }' "$PROC/buddyinfo"
}

trim_once() {
  local excess now
  excess=$(( $(file_cache_mib) - CAP_MIB ))
  if [ "$excess" -gt "$SLACK_MIB" ]; then
    # memory.reclaim takes the coldest pages first and, with no guest swap,
    # only page cache. It fails with EAGAIN when it frees less than asked
    # (pinned or mapped pages); the next pass retries.
    echo "${excess}M" >"$RECLAIM" 2>/dev/null || true
  fi
  now="$(date +%s)"
  [ $(( now - last_compact )) -ge "$COMPACT_EVERY" ] || return 0
  # Measured against what the last compaction left: blocks pinned by
  # unmovable pages stay fragmented, and recompacting them only burns CPU.
  if [ $(( $(fragmented_free_mib) - frag_floor )) -gt "$FRAG_MIB" ]; then
    echo 1 >"$PROC/sys/vm/compact_memory" 2>/dev/null || true
    last_compact="$now"
    frag_floor="$(fragmented_free_mib)"
  fi
}

main() {
  local mode="${1:-run}"
  case "$mode" in run|once) ;; *) echo "usage: memory-cap.sh [run|once]" >&2; exit 2 ;; esac
  if ! supported; then
    echo "memory-cap: not an E2B VM guest or disabled, skipped"
    exit 0
  fi
  if [ -w "$PROC/sys/vm/compaction_proactiveness" ]; then
    echo 20 >"$PROC/sys/vm/compaction_proactiveness"
  fi
  if [ "$mode" = once ]; then
    trim_once
    exit 0
  fi
  renice -n 19 -p $$ >/dev/null 2>&1 || true
  ionice -c3 -p $$ >/dev/null 2>&1 || true
  echo "memory-cap: keeping file cache near ${CAP_MIB} MiB every ${INTERVAL}s"
  while :; do
    trim_once
    sleep "$INTERVAL"
  done
}

main "$@"
