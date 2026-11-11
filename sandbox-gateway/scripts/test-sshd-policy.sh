#!/usr/bin/env bash
# Real OpenSSH config parsing; runs locally and inside the runtime E2E image.
set -euo pipefail
startup="${1:-$(dirname "$0")/../sandbox/scripts/startup.sh}"
sshd_bin="$(command -v sshd || true)"
[ -n "$sshd_bin" ] || { echo 'FAIL: openssh-server is required' >&2; exit 1; }
work="$(mktemp -d)"
sshd_pid=""
cleanup() {
  if [ -n "$sshd_pid" ]; then
    kill "$sshd_pid" 2>/dev/null || true
    wait "$sshd_pid" 2>/dev/null || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

# Exercise the shipped function against temporary files, never host ssh config.
sed -n '/^configure_sshd_password_policy() {/,/^}/p' "$startup" >"$work/policy.sh"
# shellcheck disable=SC1091
source "$work/policy.sh"
mkdir "$work/includes"
printf 'PermitEmptyPasswords yes\nPermitRootLogin yes\nPasswordAuthentication yes\n' >"$work/includes/00-e2b.conf"
cat >"$work/sshd_config" <<EOF
Include $work/includes/*.conf
UsePAM yes
PubkeyAuthentication yes
UseDNS no
EOF
chmod 0640 "$work/sshd_config"
cp "$work/sshd_config" "$work/original"
"$sshd_bin" -T -f "$work/sshd_config" >"$work/effective"
grep -qx 'permitemptypasswords yes' "$work/effective"
configure_sshd_password_policy "$work/sshd_config"
"$sshd_bin" -T -f "$work/sshd_config" >"$work/effective"
for expected in 'permitemptypasswords no' 'passwordauthentication yes' 'pubkeyauthentication yes' 'permitrootlogin yes' 'usepam yes' 'usedns no'; do
  grep -qx "$expected" "$work/effective" || { echo "FAIL: $expected" >&2; exit 1; }
done
tail -n +2 "$work/sshd_config" | cmp -s - "$work/original"
[ "$(stat -c %a "$work/sshd_config")" = 640 ]
cp "$work/sshd_config" "$work/once"
configure_sshd_password_policy "$work/sshd_config"
cmp "$work/once" "$work/sshd_config"
[ "$(find "$work" -name '*.grasp.*' | wc -l)" = 0 ]
echo 'OK: OpenSSH rejects empty-password probes, retains key/password/PAM auth and applies policy idempotently'

sed -n '/^persist_session_path() {/,/^}/p' "$startup" >"$work/session-path.sh"
# shellcheck disable=SC1091
source "$work/session-path.sh"
printf 'LANG=C.UTF-8\nPATH="/usr/bin:/bin"\n' >"$work/environment"
PATH="/root/.local/bin:$PATH" persist_session_path "$work/environment"
cp "$work/environment" "$work/environment.once"
PATH="/root/.local/bin:$PATH" persist_session_path "$work/environment"
cmp "$work/environment.once" "$work/environment"
grep -qx 'LANG=C.UTF-8' "$work/environment"
[ "$(grep -c '^PATH=' "$work/environment")" = 1 ]
grep -q '^PATH="/root/.local/bin:' "$work/environment"
[ "$(stat -c %a "$work/environment")" = 644 ]
[ "$(find "$work" -name '*.grasp.*' | wc -l)" = 0 ]
echo 'OK: SSH session PATH is persisted to /etc/environment idempotently'

# The runtime E2E image has a known test-only root password set by startup.
# A separate loopback daemon tests real auth without changing its main sshd or
# authorized_keys. Local config-only runs never change a host account.
if [ "${2:-}" = --auth ]; then
  : "${ROOT_PASSWORD:?--auth requires the disposable sandbox test password}"
  [ "$(id -u)" = 0 ] || { echo 'FAIL: --auth runs inside the disposable image as root' >&2; exit 1; }
  ssh-keygen -q -t ed25519 -N '' -f "$work/key"
  ssh-keygen -q -t ed25519 -N '' -f "$work/host-key"
  port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
  cat >>"$work/sshd_config" <<EOF
Port $port
ListenAddress 127.0.0.1
HostKey $work/host-key
PidFile $work/sshd.pid
AuthorizedKeysFile $work/key.pub
# Only this temporary daemon: the test key is under the temporary /tmp tree.
StrictModes no
EOF
  "$sshd_bin" -D -e -f "$work/sshd_config" >"$work/sshd.log" 2>&1 &
  sshd_pid=$!
  for _ in $(seq 1 50); do
    [ -f "$work/sshd.pid" ] && break
    sleep 0.1
  done
  ssh_args=(-F /dev/null -p "$port" -o ConnectTimeout=5 -o StrictHostKeyChecking=accept-new
    -o "UserKnownHostsFile=$work/known_hosts" -o LogLevel=ERROR)
  [ "$(ssh "${ssh_args[@]}" -o BatchMode=yes -o IdentitiesOnly=yes -i "$work/key" root@127.0.0.1 printf auth-ok)" = auth-ok ]
  cat >"$work/askpass" <<'EOF'
#!/bin/sh
printf '%s\n' "$ROOT_PASSWORD"
EOF
  chmod 0700 "$work/askpass"
  export SSH_ASKPASS="$work/askpass" SSH_ASKPASS_REQUIRE=force DISPLAY=grasp-ssh-test
  [ "$(ssh "${ssh_args[@]}" -o PreferredAuthentications=password -o PubkeyAuthentication=no -o NumberOfPasswordPrompts=1 root@127.0.0.1 printf auth-ok </dev/null)" = auth-ok ]
  if ssh "${ssh_args[@]}" -o BatchMode=yes -o IdentitiesOnly=yes -i "$work/host-key" root@127.0.0.1 true 2>/dev/null; then
    echo 'FAIL: unknown public key was accepted' >&2
    exit 1
  fi
  echo 'OK: real OpenSSH public-key login and password fallback succeed; unknown key is rejected'
  # /proc/$$/environ is what sshd handed the session, before any rc file ran.
  session_path="$(ssh "${ssh_args[@]}" -o BatchMode=yes -o IdentitiesOnly=yes -i "$work/key" root@127.0.0.1 \
    'tr "\0" "\n" </proc/$$/environ | sed -n "s/^PATH=//p"')"
  case ":$session_path:" in
    *:/root/.local/bin:*) ;;
    *) echo "FAIL: SSH session PATH lacks /root/.local/bin: $session_path" >&2; exit 1 ;;
  esac
  echo 'OK: SSH sessions start with /root/.local/bin on PATH'
fi
