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
# Never authenticate with developer-machine credentials during a smoke test.
unset GIT_REPOS PREVIEW_DIRECT GITHUB_TOKEN GH_TOKEN GITHUB_ENTERPRISE_TOKEN GH_ENTERPRISE_TOKEN GITLAB_TOKEN GLAB_TOKEN GITLAB_ACCESS_TOKEN
unset GITHUB_URL GITLAB_URL GIT_SSH_PRIVATE_KEY GIT_SSH_KNOWN_HOSTS GIT_SSH_COMMAND GIT_SSH SSH_AUTH_SOCK
unset GIT_CONFIG GIT_CONFIG_SYSTEM GIT_CONFIG_COUNT
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$H/.gitconfig"
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
( unset GRASP_NODE_ID MULTI OLD_SECRET
  . "$RUN/env"
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
# The assertion subshell must not inherit the sandbox's own GRASP_* exports.
( unset GRASP_NODE_ID MULTI OLD_SECRET
  . "$RUN/env"
  [ -z "${GRASP_NODE_ID:-}" ] || die "recorded key survived"
  [ -z "${MULTI:-}" ] || die "recorded key survived"
  [ "$CONFIG_ROOT" = "$H/.cursor" ] || die "CONFIG_ROOT=$CONFIG_ROOT"
)
[ ! -e "$H/.claude" ] || die "second wipe missed CONFIG_ROOT"
echo "OK: recorded keys dropped"

# A shared workflow node may select several accounts at once. Every selected
# secret is staged privately; routing remains deterministic and the next node
# can explicitly clear the complete selection.
rm -rf "$TMP/bundle"
mkdir -p "$TMP/bundle/ssh/git" "$TMP/bundle/ssh/repos" "$H/.config/gh" "$H/.config/glab-cli" "$H/.grasp/repo-credentials" "$H/.ssh"
printf 'legacy-cli\n' >"$H/.config/gh/hosts.yml"
printf 'legacy-cli\n' >"$H/.config/glab-cli/config.yml"
cat >"$TMP/bundle/ssh/git/manifest.json" <<'EOF'
[
 {"index":0,"id":"gh-default","kind":"github_https","scope":"","preferred":true},
 {"index":1,"id":"gh-repo","kind":"github_https","scope":"https://github.com/team/private.git","preferred":false},
 {"index":2,"id":"gl-host","kind":"gitlab_https","scope":"git.example","preferred":false},
 {"index":3,"id":"ssh-host","kind":"ssh","scope":"git.example","preferred":false}
]
EOF
printf 'catalog-host-token\n' >"$TMP/bundle/ssh/git/0.secret"
printf 'catalog-repo-token\n' >"$TMP/bundle/ssh/git/1.secret"
printf 'catalog-gl-token\n' >"$TMP/bundle/ssh/git/2.secret"
printf 'catalog-ssh-key\n' >"$TMP/bundle/ssh/git/3.secret"
printf 'git.example ssh-ed25519 FAKE\n' >"$TMP/bundle/ssh/git/3.known_hosts"
printf '0\tgithub_https\thttps://github.com/team/private.git\n' >"$TMP/bundle/ssh/repos/manifest"
printf 'explicit-repo-token\n' >"$TMP/bundle/ssh/repos/0.secret"
printf 'leftover-explicit\n' >"$H/.grasp/repo-credentials/0.token"
printf 'OLDKEY\n' >"$H/.ssh/grasp-repo-1"
{
  echo "AGENT_PROVIDER=$(b64 cursor)"
  echo "GITHUB_TOKEN=$(b64 must-not-survive-catalog)"
} >"$TMP/bundle/env"
tar -C "$TMP/bundle" -czf "$TMP/multi.tgz" .
HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" \
  GRASP_REPRIME_SERVICES="$TMP/services.sh" \
  bash "$REPRIME" <"$TMP/multi.tgz" >"$TMP/multi.log" 2>&1 || { cat "$TMP/multi.log"; die "multi-account reprime failed"; }
CATALOG="$H/.grasp/git-credentials"
[ "$(stat -c %a "$CATALOG")" = 700 ] || die "catalog directory not private"
for index in 0 1 2 3; do
  [ "$(stat -c %a "$CATALOG/$index.secret")" = 600 ] || die "selected secret not private"
done
password="$(printf 'url=https://github.com/team/private.git\n\n' | HOME="$H" GIT_TERMINAL_PROMPT=0 git credential fill | sed -n 's/^password=//p')"
[ "$password" = catalog-repo-token ] || die "explicit repository grant overrode the Agent catalog ($password)"
[ ! -e "$H/.grasp/repo-credentials" ] || die "old repository credential directory survived"
[ ! -e "$H/.ssh/grasp-repo-1" ] || die "old repository SSH alias survived"
! grep -q explicit-repo-token "$H/.gitconfig" "$RUN/env" 2>/dev/null || die "explicit repository token was applied"
password="$(printf 'url=https://github.com/team/public.git\n\n' | HOME="$H" GIT_TERMINAL_PROMPT=0 git credential fill | sed -n 's/^password=//p')"
[ "$password" = catalog-host-token ] || die "default host credential missing"
! grep -q 'catalog-.*-token\|must-not-survive-catalog' "$RUN/env" || die "Git secrets persisted in runtime env"
! grep -q legacy-cli "$H/.config/gh/hosts.yml" || die "previous GH CLI identity survived"
! grep -q legacy-cli "$H/.config/glab-cli/config.yml" || die "previous GitLab CLI identity survived"
echo "OK: multiple selected credentials route privately without token env"

rm -rf "$TMP/bundle"
mkdir -p "$TMP/bundle/ssh/git"
printf '[]\n' >"$TMP/bundle/ssh/git/manifest.json"
echo "AGENT_PROVIDER=$(b64 cursor)" >"$TMP/bundle/env"
tar -C "$TMP/bundle" -czf "$TMP/empty.tgz" .
HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" \
  GRASP_REPRIME_SERVICES="$TMP/services.sh" \
  bash "$REPRIME" <"$TMP/empty.tgz" >"$TMP/empty.log" 2>&1 || { cat "$TMP/empty.log"; die "explicit empty reprime failed"; }
[ -f "$CATALOG/manifest.json" ] || die "explicit empty catalog marker missing"
[ "$(cat "$CATALOG/manifest.json")" = '[]' ] || die "old catalog selection survived"
[ ! -e "$CATALOG/0.secret" ] && [ ! -e "$CATALOG/3.secret" ] || die "old selected token/key survived"
[ ! -e "$CATALOG/3.known_hosts" ] || die "old selected host trust survived"
[ ! -e "$H/.config/gh/hosts.yml" ] && [ ! -e "$H/.config/glab-cli/config.yml" ] || die "old CLI identities survived empty selection"
if printf 'url=https://github.com/team/private.git\n\n' | HOME="$H" GIT_TERMINAL_PROMPT=0 git credential fill >"$TMP/empty-fill" 2>/dev/null; then
  die "empty selection reused previous Git credentials"
fi
! grep -q 'catalog-.*-token' "$TMP/empty-fill" || die "empty selection leaked previous token"
echo "OK: explicit empty selection removes previous workflow node permissions"

# A user tool switch never initializes Git or repos, including a workspace
# left without its configured repo after an initial HTTPS clone failure.
mkdir -p "$H/workspace/plain" "$H/workspace/dirty" "$H/.ssh" "$H/.config/gh" "$H/.sandbox"
printf 'generated output\n' >"$H/workspace/plain/output.txt"
git -C "$H/workspace/dirty" init -q
printf 'original\n' >"$H/workspace/dirty/tracked.txt"
git -C "$H/workspace/dirty" add tracked.txt
git -C "$H/workspace/dirty" -c user.name=test -c user.email=test@example.com commit -qm initial
git -C "$H/workspace/dirty" worktree add -q --detach "$H/workspace/linked" HEAD
printf 'unfinished edit\n' >"$H/workspace/dirty/tracked.txt"
printf 'linked edit\n' >"$H/workspace/linked/tracked.txt"
printf 'existing SSH identity\n' >"$H/.ssh/id_rsa"
printf 'existing CLI login\n' >"$H/.config/gh/hosts.yml"
printf '[]\n' >"$H/.sandbox/repos.json"
repos='absent|https://unreachable.invalid/missing.git|,plain|https://unreachable.invalid/plain.git|,dirty|https://unreachable.invalid/dirty.git|,linked|https://unreachable.invalid/linked.git|'
printf 'declare -x GIT_REPOS="%s"\n' "$repos" >>"$RUN/env"
printf 'declare -x CURSOR_API_KEY="previous-tool-secret"\n' >>"$RUN/env"
printf 'CURSOR_API_KEY\n' >>"$RUN/reprime-keys"

workspace_snapshot() {
  (cd "$H" && find workspace .ssh .grasp .gitconfig .config .sandbox -type f -print0 | sort -z | xargs -0 sha256sum)
}
workspace_snapshot >"$TMP/workspace.before"
mkdir -p "$TMP/no-git"
cat >"$TMP/no-git/git" <<EOF
#!/bin/bash
echo "unexpected git invocation" >>"$TMP/git.log"
exit 99
EOF
chmod +x "$TMP/no-git/git"
cat >"$TMP/services.sh" <<EOF
#!/bin/bash
echo "\$*" >>"$TMP/services.log"
if [ "\$*" = 'restart backend' ] && [ -f "$TMP/fail-backend" ]; then
  rm -f "$TMP/fail-backend"
  exit 1
fi
EOF
for operation in inherited explicit failure rollback; do
  rm -rf "$TMP/bundle"
  mkdir -p "$TMP/bundle/config"
  provider=codex
  [ "$operation" != rollback ] || provider=cursor
  {
    echo "AGENT_PROVIDER=$(b64 "$provider")"
    echo "CONFIG_ROOT=$(b64 "$H/.$provider")"
    echo "CURRENT_TOOL_SECRET=$(b64 "next-$operation")"
    # First exercise inherited GIT_REPOS, then an explicit incoming clone list.
    [ "$operation" = inherited ] || echo "GIT_REPOS=$(b64 "$repos")"
  } >"$TMP/bundle/env"
  printf 'new tool config\n' >"$TMP/bundle/config/settings.json"
  tar -C "$TMP/bundle" -czf "$TMP/tool.tgz" .
  [ "$operation" != failure ] || touch "$TMP/fail-backend"
  rc=0
  HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" \
    GRASP_REPRIME_STARTUP="$TMP/no-startup.sh" GRASP_REPRIME_SERVICES="$TMP/services.sh" \
    PATH="$TMP/no-git:$PATH" bash "$REPRIME" --tool-only <"$TMP/tool.tgz" >"$TMP/tool.log" 2>&1 || rc=$?
  if [ "$operation" = failure ]; then
    [ "$rc" = 1 ] || die "tool failure exit=$rc"
  else
    [ "$rc" = 0 ] || { cat "$TMP/tool.log"; die "tool switch $operation exited $rc"; }
  fi
  workspace_snapshot >"$TMP/workspace.after"
  cmp -s "$TMP/workspace.before" "$TMP/workspace.after" || die "tool $operation changed workspace/Git/SSH state"
  [ ! -e "$H/workspace/absent" ] || die "tool switch created a missing repo"
  [ ! -e "$TMP/git.log" ] || die "tool switch invoked git"
  [ -f "$H/.$provider/settings.json" ] || die "tool config not installed"
  ! grep -q previous-tool-secret "$RUN/env" || die "previous tool secret survived"
done
echo "OK: tool switch, backend failure and rollback retain files and Git/SSH state without Git or startup"

# Shared workflow node handoffs still prepare a new repo and report failure.
rm -rf "$TMP/bundle"
mkdir -p "$TMP/bundle"
{
  echo "AGENT_PROVIDER=$(b64 cursor)"
  echo "GIT_REPOS=$(b64 "added|$H/workspace/dirty|")"
} >"$TMP/bundle/env"
tar -C "$TMP/bundle" -czf "$TMP/added.tgz" .
HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" \
  GRASP_REPRIME_SERVICES="$TMP/services.sh" bash "$REPRIME" <"$TMP/added.tgz" >"$TMP/added.log" 2>&1 || { cat "$TMP/added.log"; die "shared node did not clone its repo"; }
[ -d "$H/workspace/added/.git" ] || die "shared node's new repo missing"
echo "GIT_REPOS=$(b64 "unavailable|$TMP/not-a-repository|")" >"$TMP/bundle/env"
tar -C "$TMP/bundle" -czf "$TMP/unavailable.tgz" .
rc=0
HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" \
  GRASP_REPRIME_SERVICES="$TMP/services.sh" bash "$REPRIME" <"$TMP/unavailable.tgz" >"$TMP/unavailable.log" 2>&1 || rc=$?
[ "$rc" = 1 ] || die "shared node ignored clone failure (exit=$rc)"
grep -q 'clone unavailable failed' "$TMP/unavailable.log" || die "missing shared-node clone failure detail"
echo "OK: shared workflow still clones required repos and fails on unavailable repos"

# Bad bundles are refused before anything changes.
cp "$RUN/env" "$TMP/env.before"
if echo junk | bash "$REPRIME" >/dev/null 2>&1; then die "junk bundle accepted"; fi
rc=0; HOME="$H" GRASP_REPRIME_HOME="$H" GRASP_RUNTIME_RUN_DIR="$RUN" bash "$REPRIME" <"$TMP/bundle2.tgz" extra >/dev/null 2>&1 || rc=$?
[ "$rc" = 2 ] || die "extra arg exit=$rc"
cmp -s "$TMP/env.before" "$RUN/env" || die "env changed by refused bundle"
echo "OK: bad input refused"
