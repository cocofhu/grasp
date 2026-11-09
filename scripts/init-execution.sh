#!/usr/bin/env bash
# Explicit fresh-install initialization. Never starts/stops application writers.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

mode="${1:-}"
compose_file="${2:-}"
if [[ $# != 3 || "$3" != --offline || ( "$mode" != release && "$mode" != dev ) ]]; then
  echo 'usage: scripts/init-execution.sh <release|dev> <compose-file> --offline' >&2
  exit 2
fi
if docker compose version >/dev/null 2>&1; then
  compose=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  compose=(docker-compose)
else
  echo 'error: Docker Compose is required' >&2
  exit 1
fi
service=grasp
[[ "$mode" == release ]] || service=server
containers="$("${compose[@]}" -f "$compose_file" ps --all --quiet "$service")"
while IFS= read -r container; do
  [[ -n "$container" ]] || continue
  if [[ ! "$container" =~ ^[a-f0-9]{12,64}$ ]]; then
    echo 'error: could not prove application writers are stopped' >&2
    exit 1
  fi
  state="$(docker inspect --format '{{.State.Running}} {{.State.Status}}' "$container")"
  case "$state" in
    'false exited'|'false created'|'false dead') ;;
    *) echo 'error: application writer is still running; stop it explicitly before initialization' >&2; exit 1 ;;
  esac
done <<<"$containers"

if [[ "$mode" == dev ]]; then
  if [[ ! -f server/config.yaml ]]; then
    cp server/config.example.yaml server/config.yaml
    echo 'created server/config.yaml from config.example.yaml'
  fi
  "${compose[@]}" -f "$compose_file" build "$service"
fi
args=(--offline)
[[ "$mode" == release ]] || args+=(--source)
"${compose[@]}" -f "$compose_file" run --rm --no-deps --entrypoint node \
  -v "$PWD/scripts/init-execution.mjs:/opt/grasp-init-execution.mjs:ro" \
  "$service" /opt/grasp-init-execution.mjs "${args[@]}"
