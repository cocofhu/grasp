#!/usr/bin/env bash
# Unit smoke for SANDBOX_INJECT (extract inject_one + loop from startup.sh).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STARTUP="$ROOT/sandbox/scripts/startup.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"; kill ${HPID:-} 2>/dev/null || true' EXIT

# Initial startup and shared-sandbox reprime must inject into the same dsh home.
for source_script in "$STARTUP" "$ROOT/sandbox/scripts/reprime.sh"; do
  sed -n '/^AGENT_PROVIDER=/,/^export AGENT_PROVIDER CONFIG_ROOT$/p' "$source_script" >"$TMP/provider-root.sh"
  mapped="$(AGENT_PROVIDER=deepseek_harness HOME_DIR=/root bash -c '
    unset CONFIG_ROOT
    source "$1"
    printf "%s" "$CONFIG_ROOT"
  ' _ "$TMP/provider-root.sh")"
  [ "$mapped" = /root/.dsh ] || { echo "FAIL: dsh config root in $source_script: $mapped" >&2; exit 1; }
  mapped="$(AGENT_PROVIDER=deepseek_harness HOME_DIR=/root CONFIG_ROOT="$TMP/custom-dsh" bash -c '
    source "$1"
    printf "%s" "$CONFIG_ROOT"
  ' _ "$TMP/provider-root.sh")"
  [ "$mapped" = "$TMP/custom-dsh" ] || { echo "FAIL: dsh config root override in $source_script" >&2; exit 1; }
done
echo "OK: DeepSeek Harness startup/reprime config roots"

# Lines covering inject_one() through the SANDBOX_INJECT for-loop (inclusive).
sed -n '/^inject_one() {/,/^fi$/p' "$STARTUP" | awk '
  BEGIN {keep=1}
  /^if \[ -d \/root\/\.sandbox\/init\.d \]/ {exit}
  {print}
' >"$TMP/inject.sh"

# Keep only through the SANDBOX_INJECT block's closing `fi` (first `fi` after inject_one).
# The sed range above may include too many `fi`; truncate after second top-level if-block.
awk '
  /^inject_one\(\)/ {print; next}
  {print}
  /^if \[ -n "\$SANDBOX_INJECT" \]/ {in_inject=1}
  in_inject && /^fi$/ {print; exit}
' "$TMP/inject.sh" >"$TMP/inject2.sh" || true

# Prefer the reliable line-number extract from known markers.
START=$(grep -n '^inject_one()' "$STARTUP" | head -1 | cut -d: -f1)
END=$(awk -v s="$START" 'NR>s && /^if \[ -d \/root\/\.sandbox\/init\.d \]/ {print NR-1; exit}' "$STARTUP")
sed -n "${START},${END}p" "$STARTUP" >"$TMP/inject.sh"

mkdir -p "$TMP/seed/rules"
echo '{"ok":true}' >"$TMP/seed/mcp.json"
echo rule >"$TMP/seed/rules/r.md"
tar -C "$TMP/seed" -czf "$TMP/bundle.tgz" mcp.json rules

run_inject() {
  local inject="$1" dest="$2"
  CONFIG_ROOT="$dest" SANDBOX_INJECT="$inject" bash -c '
    set -e
    mkdir -p "$CONFIG_ROOT"
    # shellcheck disable=SC1091
    source "'"$TMP/inject.sh"'"
  '
}

# archive
OUT="$TMP/out-archive"
mkdir -p "$OUT"
run_inject "$TMP/bundle.tgz|$OUT" "$OUT"
test -f "$OUT/mcp.json" && test -f "$OUT/rules/r.md"
grep -q '"ok":true' "$OUT/mcp.json"
echo "OK: archive inject"

# URL
(
  cd "$TMP"
  python3 -m http.server 18999 --bind 127.0.0.1
) >/tmp/inject-unit-http.log 2>&1 &
HPID=$!
sleep 0.4
OUT2="$TMP/out-url"
mkdir -p "$OUT2"
run_inject "http://127.0.0.1:18999/bundle.tgz|$OUT2" "$OUT2"
test -f "$OUT2/mcp.json" && test -f "$OUT2/rules/r.md"
echo "OK: URL inject"

echo "OK: SANDBOX_INJECT unit smoke passed"
