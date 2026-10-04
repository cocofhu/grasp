package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// KeepalivePort detaches the in-sandbox listener on port into an independent
// session (setsid) with pidfile + log redirection. Returns the keepalive pid.
//
// Why not a nohup watch loop: ACP session teardown may killpg the agent process
// group. A watcher that only polls kill -0 does not change the listener's
// session/PGID, so the preview still dies with the agent. setsid (or an
// already-detached session leader) is required for true decoupling.
func (s *PreviewService) KeepalivePort(ctx context.Context, sandboxName string, port int) (int, error) {
	if s.mgr == nil || sandboxName == "" || port <= 0 {
		return 0, nil
	}
	script := fmt.Sprintf(`set -eu
port=%d
pidfile="/tmp/grasp-preview-${port}.pid"
logfile="/tmp/grasp-preview-${port}.log"

find_pid() {
  ss -tlnp 2>/dev/null | grep -E ":${port}[[:space:]]" | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | head -1
}

pid="$(find_pid || true)"
if [ -z "${pid}" ]; then
  echo "keepalive: no listener on port ${port}" >&2
  exit 1
fi

sid="$(ps -o sid= -p "${pid}" 2>/dev/null | tr -d ' ' || true)"
if [ -n "${sid}" ] && [ "${sid}" = "${pid}" ]; then
  echo "${pid}" > "${pidfile}"
  echo "OK pid=${pid} detached=1"
  exit 0
fi

cwd="$(readlink -f "/proc/${pid}/cwd" 2>/dev/null || echo /root/workspace)"
cmdfile="/tmp/grasp-preview-${port}.cmd"
tr '\0' '\n' < "/proc/${pid}/cmdline" > "${cmdfile}" || true
if [ ! -s "${cmdfile}" ]; then
  echo "keepalive: cannot read cmdline for pid ${pid}" >&2
  exit 1
fi

# Stop the old listener, then relaunch under setsid in a new session.
kill "${pid}" 2>/dev/null || true
for _ in $(seq 1 40); do
  if ! kill -0 "${pid}" 2>/dev/null; then
    break
  fi
  sleep 0.1
done
for _ in $(seq 1 40); do
  if [ -z "$(find_pid || true)" ]; then
    break
  fi
  sleep 0.1
done

(
  cd "${cwd}"
  # Rebuild argv from NUL-split cmdline lines; first line is the executable.
  set --
  while IFS= read -r arg || [ -n "${arg}" ]; do
    set -- "$@" "${arg}"
  done < "${cmdfile}"
  exec setsid "$@"
) >>"${logfile}" 2>&1 < /dev/null &
newpid=$!
echo "${newpid}" > "${pidfile}"

ok=0
for _ in $(seq 1 50); do
  cur="$(find_pid || true)"
  if [ -n "${cur}" ]; then
    echo "${cur}" > "${pidfile}"
    echo "OK pid=${cur} detached=1"
    ok=1
    break
  fi
  sleep 0.1
done
if [ "${ok}" -ne 1 ]; then
  echo "keepalive: relaunch under setsid did not bind port ${port}" >&2
  exit 1
fi
`, port)
	out, err := s.mgr.ExecScript(ctx, sandboxName, 20*time.Second, "bash", script)
	if err != nil {
		if msg := keepaliveFailure(out); msg != "" {
			return 0, fmt.Errorf("%s (%w)", msg, err)
		}
		return 0, err
	}
	return parseKeepalivePID(out), nil
}

// keepaliveFailure returns the script's own "keepalive: …" reason; ExecScript
// folds stderr into out, which is otherwise lost behind "exit status 1".
func keepaliveFailure(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); strings.HasPrefix(line, "keepalive: ") {
			return line
		}
	}
	return lastNonEmptyLine(out)
}

func lastNonEmptyLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			if len(line) > 200 {
				line = line[:200]
			}
			return line
		}
	}
	return ""
}

// ListenAddrs lists the local addresses (ss "Local Address:Port") holding a
// TCP listener on port inside the sandbox, e.g. "127.0.0.1:3000".
func (s *PreviewService) ListenAddrs(ctx context.Context, sandboxName string, port int) ([]string, error) {
	if s.mgr == nil || sandboxName == "" || port <= 0 {
		return nil, fmt.Errorf("sandbox not available")
	}
	script := fmt.Sprintf(`ss -tlnH 2>/dev/null | awk '{print $4}' | grep -E ':%d$' || true`, port)
	out, err := s.mgr.ExecScript(ctx, sandboxName, 5*time.Second, "bash", script)
	if err != nil {
		return nil, err
	}
	return parseListenAddrs(out, port), nil
}

func parseListenAddrs(out string, port int) []string {
	suffix := ":" + strconv.Itoa(port)
	seen := map[string]bool{}
	var addrs []string
	for _, line := range strings.Split(out, "\n") {
		a := strings.TrimSpace(line)
		if a == "" || !strings.HasSuffix(a, suffix) || seen[a] {
			continue
		}
		seen[a] = true
		addrs = append(addrs, a)
	}
	return addrs
}

// parseKeepalivePID extracts "OK pid=<n>" from keepalive script stdout.
func parseKeepalivePID(out string) int {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "OK pid=") {
			continue
		}
		rest := strings.TrimPrefix(line, "OK pid=")
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			rest = rest[:i]
		}
		n, err := strconv.Atoi(rest)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}
