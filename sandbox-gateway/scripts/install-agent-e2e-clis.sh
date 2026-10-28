#!/usr/bin/env bash
# Install only into the caller's temporary prefix; never use global npm/home.
set -euo pipefail
prefix="${1:?usage: install-agent-e2e-clis.sh TEMP_PREFIX}"
script_dir="$(cd "$(dirname "$0")" && pwd)"
mapfile -t packages < <(node --input-type=module - "$script_dir/agent-e2e-matrix.json" <<'JS'
import fs from 'node:fs'
const tools = JSON.parse(fs.readFileSync(process.argv[2], 'utf8')).tools
for (const tool of tools.filter((entry) => entry.mode === 'native-cli')) {
  if (!tool.package) throw new Error(`missing native CLI installation: ${tool.id}`)
  console.log(tool.package)
}
JS
)
test "${#packages[@]}" -gt 0
npm install --prefix "$prefix" --no-audit --no-fund --package-lock=false --save=false "${packages[@]}"
node --input-type=module - "$script_dir/agent-e2e-matrix.json" "$prefix" <<'JS'
import fs from 'node:fs'
import path from 'node:path'
import {execFileSync} from 'node:child_process'
const tools = JSON.parse(fs.readFileSync(process.argv[2], 'utf8')).tools
for (const tool of tools.filter((entry) => entry.mode === 'native-cli')) {
  const bin = path.resolve(process.argv[3], 'node_modules/.bin', tool.bin)
  const version = execFileSync(bin, ['--version'], {encoding: 'utf8', timeout: 30000}).trim().split('\n')[0]
  console.log(`${tool.id}: ${version}`)
}
JS
if [[ -n "${GITHUB_PATH:-}" ]]; then
  printf '%s/node_modules/.bin\n' "$prefix" >> "$GITHUB_PATH"
fi
