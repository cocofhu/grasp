#!/usr/bin/env bash
# Unit smoke for startup.sh Git HTTPS credential + gh/glab auth helpers.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STARTUP="$ROOT/sandbox/scripts/startup.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# Extract helpers from repo_scheme through configure_git_credentials (inclusive).
START=$(grep -n '^repo_scheme()' "$STARTUP" | head -1 | cut -d: -f1)
END=$(awk -v s="$START" 'NR>s && /^repo_name_from_url\(\)/ {print NR-1; exit}' "$STARTUP")
sed -n "${START},${END}p" "$STARTUP" >"$TMP/git-auth.sh"

# HOME-scoped mocks so we never touch the real ~/.config/gh or git creds.
export HOME="$TMP/home"
mkdir -p "$HOME/bin" "$HOME"
export GIT_CONFIG_GLOBAL="$TMP/gitconfig"
touch "$GIT_CONFIG_GLOBAL"
# Prefer mocks over any host-installed gh/glab.
export PATH="$HOME/bin:$PATH"

# Fake gh/glab: record argv + stdin token; succeed unless FAIL_*=1.
cat >"$HOME/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${HOME:?}"
mkdir -p "$HOME"
printf 'argv:%s\n' "$*" >"$HOME/gh.last"
cat >"$HOME/gh.token"
if [ "${FAIL_GH:-0}" = "1" ]; then
  echo "mock gh: forced failure" >&2
  exit 1
fi
exit 0
EOF
chmod +x "$HOME/bin/gh"

cat >"$HOME/bin/glab" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
: "${HOME:?}"
mkdir -p "$HOME"
printf 'argv:%s\n' "$*" >"$HOME/glab.last"
if [ "${FAIL_GLAB:-0}" = "1" ]; then
  echo "mock glab: forced failure" >&2
  exit 1
fi
exit 0
EOF
chmod +x "$HOME/bin/glab"

# Helpers hardcode /root/.git-credentials — rewrite to TMP for the test sandbox.
sed -i 's|/root|'"$TMP/root"'|g' "$TMP/git-auth.sh"
mkdir -p "$TMP/root"

# shellcheck disable=SC1091
source "$TMP/git-auth.sh"

# gh 成功路径必须带明文持久化；glab 必须写配置文件且不得走密钥环。
assert_glab_file_store() {
  local host="$1"
  local token="$2"
  grep -q "argv:auth login --hostname ${host} --token ${token} --api-protocol https --git-protocol https" "$HOME/glab.last" \
    || { echo "FAIL: glab auth login for ${host} must keep file-store flags" >&2; exit 1; }
  if grep -q -- '--use-keyring' "$HOME/glab.last"; then
    echo "FAIL: glab auth login must not pass --use-keyring" >&2
    exit 1
  fi
}

GITHUB_TOKEN="ghp_test_token"
GITLAB_TOKEN=""
GITHUB_URL=""
GITLAB_URL=""
rm -f "$TMP/root/.git-credentials" "$HOME/gh.last" "$HOME/gh.token"

setup_https_credentials "https://github.com/cocofhu/approving.git"
grep -q 'x-access-token:ghp_test_token@github.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token --insecure-storage' "$HOME/gh.last"
grep -qx 'ghp_test_token' "$HOME/gh.token"
echo "OK: github.com HTTPS + gh auth login"

rm -f "$HOME/gh.last" "$HOME/gh.token" "$TMP/root/.git-credentials"
GITHUB_TOKEN="ghe_token"
GITHUB_URL="https://ghe.example.com"
setup_https_credentials "https://ghe.example.com/org/repo.git"
grep -q 'x-access-token:ghe_token@ghe.example.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname ghe.example.com --with-token --insecure-storage' "$HOME/gh.last"
echo "OK: GITHUB_URL self-hosted + gh auth login"

rm -f "$HOME/gh.last" "$HOME/gh.token" "$TMP/root/.git-credentials"
GITHUB_TOKEN="bare_token"
GITHUB_URL=""
setup_bare_github_credentials
grep -q 'x-access-token:bare_token@github.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token --insecure-storage' "$HOME/gh.last"
echo "OK: bare GITHUB_TOKEN defaults to github.com"

rm -f "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN=""
GITLAB_TOKEN="glpat_test"
GITLAB_URL="https://gitlab.com"
setup_https_credentials "https://gitlab.com/group/project.git"
grep -q 'oauth2:glpat_test@gitlab.com' "$TMP/root/.git-credentials"
assert_glab_file_store gitlab.com glpat_test
echo "OK: gitlab.com HTTPS + glab auth login"

# gh auth failure must not roll back HTTPS credential injection, and the log must say gh is not logged in.
export FAIL_GH=1
rm -f "$HOME/gh.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN="still_ok"
GITLAB_TOKEN=""
GITHUB_URL=""
gh_fail_out="$TMP/gh-fail.out"
gh_fail_err="$TMP/gh-fail.err"
setup_https_credentials "https://github.com/cocofhu/approving.git" >"$gh_fail_out" 2>"$gh_fail_err"
grep -q 'x-access-token:still_ok@github.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token --insecure-storage' "$HOME/gh.last"
grep -q 'gh: 自动登录失败' "$gh_fail_err"
if grep -q 'still_ok' "$gh_fail_out" "$gh_fail_err"; then
  echo "FAIL: gh failure path must not print the token" >&2
  exit 1
fi
unset FAIL_GH
echo "OK: gh auth failure is non-fatal for HTTPS creds"

# glab auth failure must not roll back git credentials or abort startup.
export FAIL_GLAB=1
rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN=""
GITLAB_TOKEN="glpat_fail"
GITHUB_URL=""
GITLAB_URL="https://gitlab.com"
GIT_REPOS="proj|https://gitlab.com/group/project.git|main"
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
glab_fail_out="$TMP/glab-fail.out"
glab_fail_err="$TMP/glab-fail.err"
configure_git_credentials >"$glab_fail_out" 2>"$glab_fail_err"
grep -q 'oauth2:glpat_fail@gitlab.com' "$TMP/root/.git-credentials"
assert_glab_file_store gitlab.com glpat_fail
grep -q 'glab: 自动登录失败' "$glab_fail_err"
if grep -q 'glpat_fail' "$glab_fail_out" "$glab_fail_err"; then
  echo "FAIL: glab failure path must not print the token" >&2
  exit 1
fi
if [ -f "$HOME/gh.last" ]; then
  echo "FAIL: GitLab-only FAIL_GLAB should not invoke gh" >&2
  exit 1
fi
unset FAIL_GLAB
echo "OK: FAIL_GLAB=1 keeps git credentials and does not abort"

# Dual tokens: cloning GitLab still configures GitHub.
rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN="gh_dual"
GITLAB_TOKEN="gl_dual"
GITHUB_URL=""
GITLAB_URL="https://gitlab.com"
GIT_REPOS="proj|https://gitlab.com/group/project.git|main"
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
configure_git_credentials
grep -q 'oauth2:gl_dual@gitlab.com' "$TMP/root/.git-credentials"
grep -q 'x-access-token:gh_dual@github.com' "$TMP/root/.git-credentials"
assert_glab_file_store gitlab.com gl_dual
grep -q 'argv:auth login --hostname github.com --with-token --insecure-storage' "$HOME/gh.last"
echo "OK: dual tokens + GitLab clone configures both platforms"

# Dual tokens: cloning GitHub still configures GitLab.
rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN="gh_dual"
GITLAB_TOKEN="gl_dual"
GITHUB_URL=""
GITLAB_URL=""
GIT_REPOS="app|https://github.com/acme/app.git|main"
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
configure_git_credentials
grep -q 'x-access-token:gh_dual@github.com' "$TMP/root/.git-credentials"
grep -q 'oauth2:gl_dual@gitlab.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token --insecure-storage' "$HOME/gh.last"
assert_glab_file_store gitlab.com gl_dual
echo "OK: dual tokens + GitHub clone configures both platforms"

# Mis-derived GITLAB_URL=https://github.com must not write GitLab token onto github.com.
rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN="gh_dual"
GITLAB_TOKEN="gl_dual"
GITHUB_URL=""
GITLAB_URL="https://github.com"
GIT_REPOS="app|https://github.com/acme/app.git|main"
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
configure_git_credentials
grep -q 'x-access-token:gh_dual@github.com' "$TMP/root/.git-credentials"
grep -q 'oauth2:gl_dual@gitlab.com' "$TMP/root/.git-credentials"
if grep -q 'oauth2:.*@github.com' "$TMP/root/.git-credentials"; then
  echo "FAIL: GitLab token must not be written onto github.com" >&2
  exit 1
fi
echo "OK: mis-derived GITLAB_URL=https://github.com falls back to gitlab.com"

# Single-sided tokens must not abort under set -e (function last-line && pitfall).
rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN=""
GITLAB_TOKEN="gl_only"
GITHUB_URL=""
GITLAB_URL="https://git.example.com"
GIT_REPOS="api|https://git.example.com/team/api.git|main"
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
configure_git_credentials
grep -q 'oauth2:gl_only@git.example.com' "$TMP/root/.git-credentials"
assert_glab_file_store git.example.com gl_only
if [ -f "$HOME/gh.last" ]; then
  echo "FAIL: GitLab-only should not invoke gh" >&2
  exit 1
fi
echo "OK: GitLab-only token + GitLab clone does not abort"

rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN="gh_only"
GITLAB_TOKEN=""
GITHUB_URL=""
GITLAB_URL=""
GIT_REPOS="app|https://github.com/acme/app.git|main"
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
configure_git_credentials
grep -q 'x-access-token:gh_only@github.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token --insecure-storage' "$HOME/gh.last"
if [ -f "$HOME/glab.last" ]; then
  echo "FAIL: GitHub-only should not invoke glab" >&2
  exit 1
fi
echo "OK: GitHub-only token + GitHub clone does not abort"

rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN=""
GITLAB_TOKEN=""
GITHUB_URL=""
GITLAB_URL=""
GIT_REPOS="pub|https://example.com/pub.git|"
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
configure_git_credentials
echo "OK: empty tokens + GIT_REPOS does not abort"

rm -f "$HOME/gh.last" "$HOME/gh.token" "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN=""
GITLAB_TOKEN=""
GIT_REPOS=""
GIT_CLONE_URL=""
_GIT_CRED_RESET=0
configure_git_credentials
echo "OK: empty tokens without repos does not abort"

echo "OK: git auth unit smoke passed"
