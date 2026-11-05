#!/usr/bin/env bash
# Publish docs/public/ to the standalone GitHub Pages repository.
# Only call on default-branch success paths after a clean docs build.
#
# Required env:
#   PAGES_DEPLOY_KEY  — SSH private key (deploy key with write access on target repo)
# Optional env:
#   PAGES_REPO        — default cocofhu/approving-pages
#   PAGES_BRANCH      — default main
#   PAGES_SOURCE_DIR  — default docs/public (relative to repo root)
set -euo pipefail

PAGES_REPO="${PAGES_REPO:-cocofhu/approving-pages}"
PAGES_BRANCH="${PAGES_BRANCH:-main}"
PAGES_SOURCE_DIR="${PAGES_SOURCE_DIR:-docs/public}"

: "${PAGES_DEPLOY_KEY:?PAGES_DEPLOY_KEY is required}"

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
retry_network="${repo_root}/.github/scripts/retry-network.sh"
src="${repo_root}/${PAGES_SOURCE_DIR}"

if [[ ! -d "$src" ]]; then
  echo "missing build output: ${src}" >&2
  exit 1
fi
if [[ ! -f "$src/index.html" ]]; then
  echo "missing ${src}/index.html — run docs build first" >&2
  exit 1
fi

work="$(mktemp -d)"
ssh_dir="$(mktemp -d)"
trap 'rm -rf "$work" "$ssh_dir"' EXIT

# Deploy key auth: write key + accept github.com host key for this job only.
umask 077
printf '%s\n' "$PAGES_DEPLOY_KEY" >"${ssh_dir}/id_ed25519"
# GitHub secrets sometimes strip the trailing newline; OpenSSH requires it.
[[ "$(tail -c1 "${ssh_dir}/id_ed25519")" == $'\n' ]] || printf '\n' >>"${ssh_dir}/id_ed25519"
chmod 600 "${ssh_dir}/id_ed25519"
"$retry_network" ssh-keyscan -t ed25519,rsa github.com >"${ssh_dir}/known_hosts" 2>/dev/null
export GIT_SSH_COMMAND="ssh -i ${ssh_dir}/id_ed25519 -o BatchMode=yes -o ConnectTimeout=15 -o IdentitiesOnly=yes -o UserKnownHostsFile=${ssh_dir}/known_hosts -o StrictHostKeyChecking=yes"

clone_url="git@github.com:${PAGES_REPO}.git"

# An empty successful result means a missing branch. Authentication / transport
# errors must exhaust retries and fail, rather than creating a publish tree.
remote_refs="$("$retry_network" git ls-remote --heads "$clone_url" "refs/heads/${PAGES_BRANCH}")"
if [[ -n "$remote_refs" ]]; then
  "$retry_network" git clone --depth 1 --branch "$PAGES_BRANCH" "$clone_url" "$work"
else
  echo "branch ${PAGES_BRANCH} not found on ${PAGES_REPO}; creating orphan publish tree"
  "$retry_network" git clone --depth 1 "$clone_url" "$work"
  git -C "$work" checkout --orphan "$PAGES_BRANCH" 2>/dev/null || git -C "$work" checkout -B "$PAGES_BRANCH"
fi

# Keep existing custom-domain CNAME across publishes unless the build provides one.
preserved_cname=""
if [[ -f "$work/CNAME" && ! -f "$src/CNAME" ]]; then
  preserved_cname="$(cat "$work/CNAME")"
fi

# Replace tree contents but keep .git
find "$work" -mindepth 1 -maxdepth 1 ! -name '.git' -exec rm -rf {} +
cp -a "$src"/. "$work"/

if [[ -n "$preserved_cname" && ! -f "$work/CNAME" ]]; then
  printf '%s\n' "$preserved_cname" >"$work/CNAME"
fi

# Avoid Jekyll processing on GitHub Pages.
if [[ ! -f "$work/.nojekyll" ]]; then
  : >"$work/.nojekyll"
fi

cd "$work"
git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git add -A
if git diff --cached --quiet; then
  echo "pages unchanged: ${PAGES_REPO}@${PAGES_BRANCH}"
  exit 0
fi

sha="${GITHUB_SHA:-local}"
git commit -m "deploy: approving site from ${sha}"
"$retry_network" git push -u origin "HEAD:${PAGES_BRANCH}"
echo "published ${PAGES_REPO}@${PAGES_BRANCH}"
