#!/usr/bin/env bash
# Sourced by start.sh. An empty S3 endpoint explicitly selects the bundled,
# private SeaweedFS service; an external endpoint never falls back to it.

export GRASP_LOCAL_OBJECTSTORE_IMAGE="${GRASP_LOCAL_OBJECTSTORE_IMAGE:-chrislusf/seaweedfs:4.53@sha256:ccad23e18639c8b98cee17e7100b95830a06077b56d8e5b006d6dcf8ec5e94a0}"

local_objectstore_requested() {
  [[ "${GRASP_STORAGE_DRIVER:-}" == s3 && -z "${GRASP_S3_ENDPOINT:-}" ]]
}

local_objectstore_profile() {
  case ",${COMPOSE_PROFILES:-}," in
    *,local-storage,*) ;;
    *) export COMPOSE_PROFILES="${COMPOSE_PROFILES:+${COMPOSE_PROFILES},}local-storage" ;;
  esac
}

local_objectstore_owner() {
  # GNU/Linux and BSD/macOS stat use different flags.
  stat -c "$1" "$2" 2>/dev/null || stat -f "$3" "$2"
}

prepare_local_objectstore() {
  GRASP_LOCAL_OBJECTSTORE_ENABLED=0
  local_objectstore_requested || return 0

  # Partial cloud credentials are an error, never permission to provision a
  # different storage location. Local credentials are generated and kept here.
  if [[ -n "${GRASP_S3_ACCESS_KEY_ID:-}${GRASP_S3_SECRET_ACCESS_KEY:-}${GRASP_S3_SESSION_TOKEN:-}" ]]; then
    echo "error: S3 credentials require GRASP_S3_ENDPOINT; leave both empty for bundled storage" >&2
    return 1
  fi
  local bucket="${GRASP_S3_BUCKET:-grasp}"
  local port="${GRASP_LOCAL_OBJECTSTORE_PORT:-8333}"
  if [[ ! "$bucket" =~ ^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$ ]]; then
    echo "error: local S3 bucket must contain 3-63 lowercase letters, digits or hyphens" >&2
    return 1
  fi
  if [[ ! "$port" =~ ^[0-9]{1,5}$ ]] || (( 10#$port < 1 || 10#$port > 65535 )); then
    echo "error: GRASP_LOCAL_OBJECTSTORE_PORT must be between 1 and 65535" >&2
    return 1
  fi

  local dir="$1" saved="$1/client.env" temporary key value
  if [[ -L "$dir" || -L "$dir/data" ]]; then
    echo "error: local object-storage directories must not be symlinks" >&2
    return 1
  fi
  mkdir -p "$dir/data"
  chmod 700 "$dir"
  if [[ -L "$saved" || -L "$dir/iam.json" ]]; then
    echo "error: local object-storage credential files must not be symlinks" >&2
    return 1
  fi
  if [[ ! -e "$saved" ]]; then
    if (shopt -s nullglob dotglob; existing_objects=("$dir/data"/*); (( ${#existing_objects[@]} > 0 ))) && [[ ! -f "$saved" ]]; then
      echo "error: existing local object data has no client identity; restore its credentials before starting" >&2
      return 1
    fi
    temporary="$(mktemp "$dir/.client.XXXXXX")"
    chmod 600 "$temporary"
    {
      printf 'LOCAL_BUCKET=%s\n' "$bucket"
      printf 'LOCAL_ACCESS_KEY=%s\n' "$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
      printf 'LOCAL_SECRET_KEY=%s\n' "$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
    } > "$temporary"
    # Atomic first writer wins, so concurrent starts use the same identity.
    if ! ln "$temporary" "$saved" 2>/dev/null && [[ ! -f "$saved" ]]; then
      rm -f "$temporary"
      echo "error: could not persist local object-storage credentials" >&2
      return 1
    fi
    rm -f "$temporary"
  fi
  chmod 600 "$saved"
  local stored_bucket="" access="" secret="" count=0
  while IFS='=' read -r key value; do
    case "$key" in
      LOCAL_BUCKET) stored_bucket="$value" ;;
      LOCAL_ACCESS_KEY) access="$value" ;;
      LOCAL_SECRET_KEY) secret="$value" ;;
      *) echo "error: invalid local object-storage credential file" >&2; return 1 ;;
    esac
    count=$((count + 1))
  done < "$saved"
  if [[ "$count" != 3 || "$stored_bucket" != "$bucket" || ! "$access" =~ ^[a-f0-9]{32}$ || ! "$secret" =~ ^[a-f0-9]{64}$ ]]; then
    echo "error: local storage identity or bucket changed; restore its credentials or migrate storage explicitly" >&2
    return 1
  fi

  local host_uid host_gid storage_uid storage_gid data_uid data_gid
  host_uid="$(id -u)"
  host_gid="$(id -g)"
  data_uid="$(local_objectstore_owner %u "$dir/data" %u)"
  data_gid="$(local_objectstore_owner %g "$dir/data" %g)"
  storage_uid="${GRASP_LOCAL_OBJECTSTORE_UID:-$host_uid}"
  storage_gid="${GRASP_LOCAL_OBJECTSTORE_GID:-$host_gid}"
  if [[ "$host_uid" == 0 ]]; then
    # Reuse a retained volume's non-root owner. A root launcher defaults a new
    # store (or the former root-owned layout) to an unprivileged identity.
    storage_uid="${GRASP_LOCAL_OBJECTSTORE_UID:-${data_uid}}"
    storage_gid="${GRASP_LOCAL_OBJECTSTORE_GID:-${data_gid}}"
    if [[ "$storage_uid" == 0 && -z "${GRASP_LOCAL_OBJECTSTORE_UID:-}" ]]; then storage_uid=10001; fi
    if [[ "$storage_gid" == 0 && -z "${GRASP_LOCAL_OBJECTSTORE_GID:-}" ]]; then storage_gid=10001; fi
  fi
  if [[ ! "$storage_uid" =~ ^[0-9]{1,10}$ || ! "$storage_gid" =~ ^[0-9]{1,10}$ ]] ||
     (( 10#$storage_uid < 1 || 10#$storage_uid > 2147483647 || 10#$storage_gid > 2147483647 )); then
    echo "error: local object storage requires a non-root numeric UID and a numeric GID" >&2
    return 1
  fi
  storage_uid="$((10#$storage_uid))"
  storage_gid="$((10#$storage_gid))"
  if [[ "$host_uid" == 0 ]]; then
    if [[ "$data_uid:$data_gid" != "$storage_uid:$storage_gid" ]]; then
      # Migrate only the private data mount; -h never follows an entry's
      # symlink to a file outside it. Client credentials remain host-private.
      chown -R -h "$storage_uid:$storage_gid" "$dir/data"
    fi
  elif [[ "$host_uid:$host_gid" != "$storage_uid:$storage_gid" || "$data_uid:$data_gid" != "$storage_uid:$storage_gid" ]]; then
    echo "error: restore local object-storage mount ownership to the launcher UID/GID before starting" >&2
    return 1
  fi
  temporary="$(mktemp "$dir/.iam.XXXXXX")"
  chmod 600 "$temporary"
  # Values have been validated above. Only this bucket is granted, and the
  # server pre-creates it without handing an admin identity to Grasp.
  printf '{"identities":[{"name":"grasp-local-app","credentials":[{"accessKey":"%s","secretKey":"%s"}],"actions":["Read:%s","Write:%s","List:%s"]}]}\n' \
    "$access" "$secret" "$bucket" "$bucket" "$bucket" > "$temporary"
  if [[ "$host_uid" == 0 ]]; then
    chown "$storage_uid:$storage_gid" "$temporary"
  fi
  mv -f "$temporary" "$dir/iam.json"

  export GRASP_LOCAL_OBJECTSTORE_DIR="$dir"
  export GRASP_LOCAL_OBJECTSTORE_PORT="$((10#$port))"
  export GRASP_LOCAL_OBJECTSTORE_UID="$storage_uid" GRASP_LOCAL_OBJECTSTORE_GID="$storage_gid"
  export GRASP_S3_ENDPOINT="http://127.0.0.1:$((10#$port))"
  export GRASP_S3_BUCKET="$bucket" GRASP_S3_ACCESS_KEY_ID="$access" GRASP_S3_SECRET_ACCESS_KEY="$secret"
  export GRASP_S3_REGION="${GRASP_S3_REGION:-us-east-1}" GRASP_S3_PREFIX="${GRASP_S3_PREFIX:-application}"
  export GRASP_S3_PATH_STYLE=true
  GRASP_LOCAL_OBJECTSTORE_ENABLED=1
  local_objectstore_profile
}

start_local_objectstore() {
  [[ "${GRASP_LOCAL_OBJECTSTORE_ENABLED:-0}" == 1 ]] || return 0
  local compose_file="$1" container status storage_attempt
  "${COMPOSE[@]}" -f "$compose_file" up -d objectstore
  container="$("${COMPOSE[@]}" -f "$compose_file" ps -q objectstore)"
  if [[ -z "$container" ]]; then
    echo "error: local object storage did not start" >&2
    return 1
  fi
  for ((storage_attempt=0; storage_attempt<90; storage_attempt++)); do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$container")"
    if [[ "$status" == healthy ]]; then
      echo "local object storage ready: ${GRASP_S3_ENDPOINT} (private bucket ${GRASP_S3_BUCKET})"
      return 0
    fi
    if [[ "$status" == unhealthy ]]; then
      break
    fi
    sleep 1
  done
  echo "error: local object storage is not ready; inspect ./start.sh logs objectstore" >&2
  return 1
}
