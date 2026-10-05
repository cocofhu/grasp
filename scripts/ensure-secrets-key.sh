#!/usr/bin/env bash
# Ensure GRASP_SECRETS_KEY (base64 32-byte AES key) is set in the given .env
# file, generating one on first run. The key must never change once written:
# a new key makes every saved credential unreadable.
#
# Usage: scripts/ensure-secrets-key.sh <env-file>   (prints the key)
#        source scripts/ensure-secrets-key.sh; ensure_secrets_key <env-file>
set -euo pipefail

generate_secrets_key() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32
  else
    head -c 32 /dev/urandom | base64 | tr -d '\n'
    echo
  fi
}

ensure_secrets_key() {
  local env_file="$1"
  local current=""
  if [[ -f "$env_file" ]]; then
    current="$(sed -n 's/^GRASP_SECRETS_KEY=//p' "$env_file" | tail -n 1 | tr -d '"'"'"' \r')"
  fi
  if [[ -n "$current" ]]; then
    echo "$current"
    return 0
  fi
  local key
  key="$(generate_secrets_key)"
  if [[ -f "$env_file" ]] && grep -q '^GRASP_SECRETS_KEY=' "$env_file"; then
    local tmp
    tmp="$(mktemp)"
    awk -v k="$key" '/^GRASP_SECRETS_KEY=/{print "GRASP_SECRETS_KEY=" k; next} {print}' "$env_file" >"$tmp"
    cat "$tmp" >"$env_file"
    rm -f "$tmp"
  else
    printf '\nGRASP_SECRETS_KEY=%s\n' "$key" >>"$env_file"
  fi
  echo "$key"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  if [[ $# -ne 1 ]]; then
    echo "usage: $0 <env-file>" >&2
    exit 2
  fi
  ensure_secrets_key "$1"
fi
