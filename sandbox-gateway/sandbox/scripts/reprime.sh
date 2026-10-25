#!/bin/bash
# Re-prime a running sandbox for the next node of a shared-sandbox run: swap the
# agent env, config home and Git/SSH credentials of the previous node for the
# next node's, then restart backend (and preview-inject) so they pick it up.
# Grasp calls it over SSH with a tar.gz on stdin; only used when a workflow runs
# in sandbox mode "shared" (off by default). startup.sh is not involved.
#
# Usage: reprime.sh < bundle.tgz
# Bundle:
#   env      required. NAME=base64(value) lines: the Grasp spec env of the next node.
#   unset    optional. One env name per line: Grasp spec env of the previous node.
#   config/  optional. Config home of the next node, extracted into its CONFIG_ROOT.
#   ssh/     optional. SSH staging bundle (id_rsa, known_hosts, repos/...), the
#            same layout startup.sh reads from /tmp/grasp-ssh-inject.
# Exit: 0 ok · 1 failed · 2 usage / bad bundle
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUN_DIR="${GRASP_RUNTIME_RUN_DIR:-/run/grasp-runtime}"
ENV_FILE="$RUN_DIR/env"
KEYS_FILE="$RUN_DIR/reprime-keys"
STARTUP="${GRASP_REPRIME_STARTUP:-$SCRIPT_DIR/startup.sh}"
SERVICES="${GRASP_REPRIME_SERVICES:-$SCRIPT_DIR/services.sh}"
HOME_DIR="${GRASP_REPRIME_HOME:-/root}"

[ $# -eq 0 ] || { echo "usage: reprime.sh < bundle.tgz" >&2; exit 2; }

umask 077
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
if ! tar -xzf - -C "$STAGE"; then
  echo "reprime: bad bundle" >&2
  exit 2
fi
if [ ! -f "$STAGE/env" ]; then
  echo "reprime: bundle has no env" >&2
  exit 2
fi

valid_name() {
  case "$1" in
    ''|[0-9]*|*[!A-Za-z0-9_]*) return 1 ;;
  esac
  return 0
}

# --- previous node -----------------------------------------------------------
if [ -r "$ENV_FILE" ]; then
  # shellcheck disable=SC1090
  . "$ENV_FILE"
fi
OLD_CONFIG_ROOT="${CONFIG_ROOT:-}"

# Drop the previous node's spec env: names the server sent plus the ones the
# last re-prime recorded (they survive a control-plane restart).
for f in "$STAGE/unset" "$KEYS_FILE"; do
  [ -r "$f" ] || continue
  while IFS= read -r name || [ -n "$name" ]; do
    valid_name "$name" && unset "$name"
  done <"$f"
done

# Wipe what Grasp wrote for the previous agent. Paths are fixed so a hostile
# CONFIG_ROOT cannot point the wipe outside the agent config homes.
case "$OLD_CONFIG_ROOT" in
  "$HOME_DIR"/.?*) rm -rf "$OLD_CONFIG_ROOT" ;;
esac
rm -rf "$HOME_DIR/.git-credentials" "$HOME_DIR/.grasp/repo-credentials" "$HOME_DIR/.grasp/git-credentials" \
  "$HOME_DIR/.ssh/id_rsa" "$HOME_DIR/.ssh/known_hosts" "$HOME_DIR/.ssh/grasp_repos_config" \
  "$HOME_DIR/.config/gh/hosts.yml" "$HOME_DIR/.config/glab-cli/config.yml"
rm -f "$HOME_DIR"/.ssh/grasp-repo-*
if [ -f "$HOME_DIR/.gitconfig" ]; then
  git config --global --remove-section credential 2>/dev/null || true
  git config --global --unset-all core.sshCommand 2>/dev/null || true
  git config --global --get-regexp '^credential\..*\.helper$' 2>/dev/null | awk '{print $1}' | sort -u |
    while IFS= read -r k; do git config --global --unset-all "$k" 2>/dev/null || true; done
  git config --global --get-regexp '^url\..*grasp-repo-.*\.insteadof$' 2>/dev/null | awk '{print $1}' | sort -u |
    while IFS= read -r k; do git config --global --unset-all "$k" 2>/dev/null || true; done
  git config --global --unset-all user.email 2>/dev/null || true
  git config --global --unset-all user.name 2>/dev/null || true
fi

# --- next node ---------------------------------------------------------------
: >"$STAGE/keys"
while IFS= read -r line || [ -n "$line" ]; do
  [ -n "$line" ] || continue
  name="${line%%=*}"
  if ! valid_name "$name"; then
    echo "reprime: skipping invalid env name" >&2
    continue
  fi
  if ! value="$(printf '%s' "${line#*=}" | base64 -d 2>/dev/null)"; then
    echo "reprime: bad value for ${name}" >&2
    exit 2
  fi
  export "$name=$value"
  printf '%s\n' "$name" >>"$STAGE/keys"
done <"$STAGE/env"

# Same provider → CONFIG_ROOT mapping as startup.sh.
AGENT_PROVIDER=${AGENT_PROVIDER:-cursor}
case "$AGENT_PROVIDER" in
  cursor|cursor_acp)           CONFIG_ROOT=${CONFIG_ROOT:-$HOME_DIR/.cursor} ;;
  claude_code|claude_code_acp) CONFIG_ROOT=${CONFIG_ROOT:-$HOME_DIR/.claude} ;;
  codebuddy|codebuddy_acp)     CONFIG_ROOT=${CONFIG_ROOT:-$HOME_DIR/.codebuddy} ;;
  opencode)                    CONFIG_ROOT=${CONFIG_ROOT:-$HOME_DIR/.config/opencode} ;;
  deepseek_harness)             CONFIG_ROOT=${CONFIG_ROOT:-$HOME_DIR/.dsh} ;;
  *)                           CONFIG_ROOT=${CONFIG_ROOT:-$HOME_DIR/.$AGENT_PROVIDER} ;;
esac
export AGENT_PROVIDER CONFIG_ROOT
WORKSPACE_DIR=${WORKSPACE_DIR:-$HOME_DIR/workspace}
export WORKSPACE_DIR

if [ -d "$STAGE/config" ]; then
  mkdir -p "$CONFIG_ROOT"
  cp -a "$STAGE/config/." "$CONFIG_ROOT/"
fi

# The Git/SSH credential helpers are startup.sh's own: pull their definitions
# out of it rather than keeping a second copy in sync.
if [ ! -r "$STARTUP" ]; then
  echo "reprime: ${STARTUP} not found" >&2
  exit 1
fi
FUNCS="repo_scheme repo_host url_host prepare_git_credentials_file glab_auth_login gh_auth_login
setup_https_credentials setup_ssh_credentials setup_bare_gitlab_credentials setup_bare_github_credentials
setup_repo_credentials configure_git_credentials repo_url_key repo_has_own_credential
setup_repo_https_credential setup_repo_ssh_credential configure_repo_credentials configure_git_catalog repo_name_from_url
apply_ssh_file_inject"
# shellcheck disable=SC2086
defs="$(awk -v want="$(echo $FUNCS)" '
  BEGIN { n = split(want, w, " "); for (i = 1; i <= n; i++) keep[w[i]] = 1 }
  !inside && match($0, /^[a-z_]+\(\) \{$/) {
    name = substr($0, 1, index($0, "(") - 1)
    if (name in keep) { inside = 1; found[name] = 1 }
  }
  inside { print }
  inside && /^}$/ { inside = 0 }
  END { for (k in keep) if (!(k in found)) { print "missing " k > "/dev/stderr"; exit 1 } }
' "$STARTUP")" || { echo "reprime: credential helpers missing from startup.sh" >&2; exit 1; }
# startup's image home is fixed; the same definitions also support the isolated
# re-prime home used by smoke tests and callers overriding GRASP_REPRIME_HOME.
defs="${defs//\/root/\$HOME_DIR}"
eval "$defs"
# startup.sh's helpers read optional env without defaults.
set +u
REPO_CRED_DIR="$HOME_DIR/.grasp/repo-credentials"
REPO_SSH_CONFIG="$HOME_DIR/.ssh/grasp_repos_config"
_REPO_CRED_URLS=""
_GIT_CATALOG_CONFIGURED=0
GIT_CREDENTIAL_HOME="$HOME_DIR"
GIT_REPOS="${GIT_REPOS:-}"
GITLAB_TOKEN="${GITLAB_TOKEN:-}"
GITHUB_TOKEN="${GITHUB_TOKEN:-}"
GITLAB_URL="${GITLAB_URL:-}"
GITHUB_URL="${GITHUB_URL:-}"
GIT_SSH_PRIVATE_KEY="${GIT_SSH_PRIVATE_KEY:-}"
GIT_SSH_KNOWN_HOSTS="${GIT_SSH_KNOWN_HOSTS:-}"

if [ -d "$STAGE/ssh" ]; then
  if [ ! -f "$STAGE/ssh/git/manifest.json" ]; then
    apply_ssh_file_inject "$STAGE/ssh"
  fi
  configure_git_catalog "$STAGE/ssh/git" || { echo "reprime: git catalog failed" >&2; exit 1; }
  configure_repo_credentials "$STAGE/ssh/repos"
fi
configure_git_credentials || { echo "reprime: git credentials failed" >&2; exit 1; }
git config --global user.email "${GIT_USER_EMAIL:-sandbox@localhost}"
git config --global user.name "${GIT_USER_NAME:-sandbox}"

# Clone repos the previous nodes did not have. Existing ones are left alone:
# the shared workspace is the point of this mode.
if [ -n "$GIT_REPOS" ]; then
  IFS=',' read -ra _entries <<<"$GIT_REPOS"
  for _entry in "${_entries[@]}"; do
    [ -n "$_entry" ] || continue
    IFS='|' read -r _name _url _branch <<<"$_entry"
    [ -n "$_url" ] || continue
    [ -n "$_name" ] || _name=$(repo_name_from_url "$_url")
    case "$_name" in ''|*/*|*\\*|.|..) continue ;; esac
    _dest="$WORKSPACE_DIR/$_name"
    [ -d "$_dest/.git" ] && continue
    echo "reprime: cloning ${_name}"
    rm -rf "$_dest"
    mkdir -p "$_dest"
    if ! git clone "$_url" "$_dest"; then
      rm -rf "$_dest"
      echo "reprime: clone ${_name} failed" >&2
      exit 1
    fi
    if [ -n "$_branch" ]; then
      git -C "$_dest" fetch origin "$_branch" 2>/dev/null || true
      git -C "$_dest" checkout "$_branch" 2>/dev/null || true
    fi
  done
fi

# --- persist + restart -------------------------------------------------------
mkdir -p "$RUN_DIR"
chmod 700 "$RUN_DIR"
(
  for _v in $(compgen -e); do
    case "$_v" in PWD|OLDPWD|SHLVL|_) continue ;; esac
    declare -p "$_v"
  done >"$ENV_FILE.tmp"
) && mv -f "$ENV_FILE.tmp" "$ENV_FILE" || { echo "reprime: write env failed" >&2; exit 1; }
cp -f "$STAGE/keys" "$KEYS_FILE"

"$SERVICES" restart backend || exit 1
if [ "${PREVIEW_DIRECT:-}" = "1" ] || [ "${PREVIEW_DIRECT:-}" = "true" ]; then
  "$SERVICES" restart preview-inject || echo "reprime: preview-inject restart failed" >&2
else
  "$SERVICES" stop preview-inject >/dev/null 2>&1 || true
fi
echo "reprime: ok (AGENT_PROVIDER=${AGENT_PROVIDER})"
