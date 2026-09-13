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

GITHUB_TOKEN="ghp_test_token"
GITLAB_TOKEN=""
GITHUB_URL=""
GITLAB_URL=""
rm -f "$TMP/root/.git-credentials" "$HOME/gh.last" "$HOME/gh.token"

setup_https_credentials "https://github.com/cocofhu/approving.git"
grep -q 'x-access-token:ghp_test_token@github.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token' "$HOME/gh.last"
grep -qx 'ghp_test_token' "$HOME/gh.token"
echo "OK: github.com HTTPS + gh auth login"

rm -f "$HOME/gh.last" "$HOME/gh.token" "$TMP/root/.git-credentials"
GITHUB_TOKEN="ghe_token"
GITHUB_URL="https://ghe.example.com"
setup_https_credentials "https://ghe.example.com/org/repo.git"
grep -q 'x-access-token:ghe_token@ghe.example.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname ghe.example.com --with-token' "$HOME/gh.last"
echo "OK: GITHUB_URL self-hosted + gh auth login"

rm -f "$HOME/gh.last" "$HOME/gh.token" "$TMP/root/.git-credentials"
GITHUB_TOKEN="bare_token"
GITHUB_URL=""
setup_bare_github_credentials
grep -q 'x-access-token:bare_token@github.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token' "$HOME/gh.last"
echo "OK: bare GITHUB_TOKEN defaults to github.com"

rm -f "$HOME/glab.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN=""
GITLAB_TOKEN="glpat_test"
GITLAB_URL="https://gitlab.com"
setup_https_credentials "https://gitlab.com/group/project.git"
grep -q 'oauth2:glpat_test@gitlab.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname gitlab.com --token glpat_test' "$HOME/glab.last"
echo "OK: gitlab.com HTTPS + glab auth login"

# gh auth failure must not roll back HTTPS credential injection.
export FAIL_GH=1
rm -f "$HOME/gh.last" "$TMP/root/.git-credentials"
GITHUB_TOKEN="still_ok"
GITLAB_TOKEN=""
GITHUB_URL=""
setup_https_credentials "https://github.com/cocofhu/approving.git"
grep -q 'x-access-token:still_ok@github.com' "$TMP/root/.git-credentials"
grep -q 'argv:auth login --hostname github.com --with-token' "$HOME/gh.last"
unset FAIL_GH
echo "OK: gh auth failure is non-fatal for HTTPS creds"

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
grep -q 'argv:auth login --hostname gitlab.com --token gl_dual' "$HOME/glab.last"
grep -q 'argv:auth login --hostname github.com --with-token' "$HOME/gh.last"
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
grep -q 'argv:auth login --hostname github.com --with-token' "$HOME/gh.last"
grep -q 'argv:auth login --hostname gitlab.com --token gl_dual' "$HOME/glab.last"
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
grep -q 'argv:auth login --hostname git.example.com --token gl_only' "$HOME/glab.last"
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
grep -q 'argv:auth login --hostname github.com --with-token' "$HOME/gh.last"
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
