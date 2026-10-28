#!/usr/bin/env bash
# Confirm a built Worktree image can run the local toolchains and does not
# contain the sandbox-only daemons. The image is not pushed.
set -euo pipefail

image="${1:?image ref}"

docker run --rm --entrypoint bash "$image" -lc '
set -euo pipefail
test "$(printenv GRASP_EXECUTION_MODE)" = "worktree"
test -x /app/grasp-server
test -d /ms-playwright
node -v | grep -q "^v24\.13\."
for cmd in cursor-agent claude codebuddy traecli opencode codex dsh \
           git gh glab go java python3 gcc \
           Xvfb x11vnc websockify socat chrome-devtools-mcp node; do
  command -v "$cmd" >/dev/null || { echo "missing $cmd" >&2; exit 1; }
done
for cmd in dockerd sshd code-server; do
  if command -v "$cmd" >/dev/null 2>&1; then
    echo "worktree image must not contain $cmd" >&2
    exit 1
  fi
done
'
