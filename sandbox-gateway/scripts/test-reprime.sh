#!/usr/bin/env bash
# Unit smoke for sandbox/scripts/reprime.sh (shared run sandbox re-prime):
# the previous node's env, config home and Git credentials are gone, the next
# node's are in place, and backend is restarted. Runs against a temp HOME with
# a stub services.sh; touches nothing outside it.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPRIME="$ROOT/sandbox/scripts/reprime.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

die() { echo "FAIL: $*" >&2; exit 1; }

H="$TMP/home"
RUN="$TMP/run"
mkdir -p "$H/.cursor/rules" "$H/.ssh" "$RUN" "$H/workspace"
echo old-rule >"$H/.cursor/rules/old.md"
echo old-key >"$H/.ssh/id_rsa"
echo "https://x-access-token:OLD@github.com" >"$H/.git-credentials"
cat >"$H/.gitconfig" <<'EOF'
[credential]
	helper = store
[user]
	email = old@example.com
	name = old
EOF
cat >"$RUN/env" <<EOF
declare -x AGENT_PROVIDER="cursor"
declare -x CONFIG_ROOT="$H/.cursor"
declare -x OLD_SECRET="s3cret"
declare -x BASE_VAR="keep-me"
declare -x WORKSPACE_DIR="$H/workspace"
EOF

cat >"$TMP/services.sh" <<EOF
#!/bin/bash
echo "\$*" >>"$TMP/services.log"
EOF
chmod +x "$TMP/services.sh"

b64() { printf '%s' "$1" | base64 | tr -d '\n'; }
mkdir -p "$TMP/bundle/config/rules"
echo new-rule >"$TMP/bundle/config/rules/new.md"
{
  echo "AGENT_PROVIDER=$(b64 claude_code)"
  echo "CONFIG_ROOT=$(b64 "$H/.claude")"
  echo "GRASP_NODE_ID=$(b64 node-b)"
  echo "MULTI=$(b64 $'line1\nline 2 "quoted" $HOME')"
  echo "GIT_USER_EMAIL=$(b64 new@example.com)"
  echo "GIT_USER_NAME=$(b64 new)"
} >"$TMP/bundle/env"
printf 'OLD_SECRET\n' >"$TMP/bundle/unset"
tar -C "$TMP/bundle" -czf "$TMP/bundle.tgz" .

HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" \
  GRASP_REPRIME_SERVICES="$TMP/services.sh" \
  bash "$REPRIME" <"$TMP/bundle.tgz" >"$TMP/out.log" 2>&1 || { cat "$TMP/out.log"; die "reprime exited non-zero"; }

[ ! -e "$H/.cursor" ] || die "old CONFIG_ROOT not wiped"
[ -f "$H/.claude/rules/new.md" ] || die "new config home missing"
[ ! -e "$H/.ssh/id_rsa" ] || die "old ssh key left behind"
[ ! -e "$H/.git-credentials" ] || die "old git credentials left behind"
grep -q 'new@example.com' "$H/.gitconfig" || die "git identity not updated"
! grep -q 'old@example.com' "$H/.gitconfig" || die "old git identity left behind"
echo "OK: previous node wiped, next node config applied"

# shellcheck disable=SC1091
( . "$RUN/env"
  [ "$AGENT_PROVIDER" = claude_code ] || die "AGENT_PROVIDER=$AGENT_PROVIDER"
  [ "$CONFIG_ROOT" = "$H/.claude" ] || die "CONFIG_ROOT=$CONFIG_ROOT"
  [ "$GRASP_NODE_ID" = node-b ] || die "GRASP_NODE_ID=$GRASP_NODE_ID"
  [ "$MULTI" = $'line1\nline 2 "quoted" $HOME' ] || die "MULTI not round-tripped"
  [ "$BASE_VAR" = keep-me ] || die "base env lost"
  [ -z "${OLD_SECRET:-}" ] || die "previous node env survived"
)
grep -qx GRASP_NODE_ID "$RUN/reprime-keys" || die "keys file missing GRASP_NODE_ID"
echo "OK: env file replaced"

grep -qx 'restart backend' "$TMP/services.log" || die "backend not restarted"
grep -qx 'stop preview-inject' "$TMP/services.log" || die "preview-inject not stopped"
echo "OK: services restarted"

# The recorded keys are dropped by the next re-prime even without an unset file.
rm -rf "$TMP/bundle" && mkdir -p "$TMP/bundle"
echo "AGENT_PROVIDER=$(b64 cursor)" >"$TMP/bundle/env"
tar -C "$TMP/bundle" -czf "$TMP/bundle2.tgz" .
HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" \
  GRASP_REPRIME_SERVICES="$TMP/services.sh" \
  bash "$REPRIME" <"$TMP/bundle2.tgz" >"$TMP/out2.log" 2>&1 || { cat "$TMP/out2.log"; die "second reprime failed"; }
# shellcheck disable=SC1091
( . "$RUN/env"
  [ -z "${GRASP_NODE_ID:-}" ] || die "recorded key survived"
  [ -z "${MULTI:-}" ] || die "recorded key survived"
  [ "$CONFIG_ROOT" = "$H/.cursor" ] || die "CONFIG_ROOT=$CONFIG_ROOT"
)
[ ! -e "$H/.claude" ] || die "second wipe missed CONFIG_ROOT"
echo "OK: recorded keys dropped"

# Bad bundles are refused before anything changes.
cp "$RUN/env" "$TMP/env.before"
if echo junk | bash "$REPRIME" >/dev/null 2>&1; then die "junk bundle accepted"; fi
rc=0; HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" bash "$REPRIME" <"$TMP/bundle2.tgz" extra >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || die "extra arg exit=$rc"
cmp -s "$TMP/env.before" "$RUN/env" || die "env changed by refused bundle"
echo "OK: bad input refused"
