#!/usr/bin/env bash
# Platform-only SSH bootstrap. The JSON tuple is read from stdin by the fixed
# runtime binary, never interpolated into shell code or accepted through HTTP.
set -euo pipefail
task_run_dir="/run/grasp-runtime"
if [ -r "$task_run_dir/env" ]; then
  # SSH does not inherit the authenticated standby's environment.
  # shellcheck disable=SC1090
  . "$task_run_dir/env"
fi
export GRASP_RUNTIME_RUN_DIR="$task_run_dir"
unset GRASP_RUNTIME_BIN_DIR GRASP_RUNTIME_SHARE_DIR
task_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "$task_script_dir/../bin/backend" --configure-workspace
