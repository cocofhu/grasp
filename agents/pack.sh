#!/usr/bin/env bash
# pack.sh — 将 agents/<AgentName>/ 打成可经 Agent Studio ImportZIP 导入的 ZIP
#
# 用法:
#   ./pack.sh                # 打包全部 Agent（同 all）
#   ./pack.sh all            # 打包全部 Agent
#   ./pack.sh ClarifyAgent   # 仅打包指定 Agent
#
# 产出: agents/dist/<AgentName>.zip
#   - ZIP 根级含 agent.json（schemaVersion:1）
#   - workspace/ 下文件以相对路径扁平写入 ZIP（无 workspace/ 前缀）
# 本脚本不读取、不嵌入任何密钥或凭据。
#
# 注意: 当前平台 ImportZIP 不持久化 ZIP 内 acpBackend 字段，导入后默认 cursor；
# 源码 agent.json 中的 acpBackend 供可读性与直拷磁盘布局，勿假定 ZIP 能携带非 cursor 后端。

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DIST="${ROOT}/dist"
TARGET="${1:-all}"

ALL_AGENTS=(
  PMAgent
  ClarifyAgent
  ResearchAgent
  ProposalAgent
  PlanAgent
  ImplementAgent
  TestAgent
  ReviewAgent
  VisualAgent
  PreviewAgent
  PreflightAgent
)

die() { echo "error: $*" >&2; exit 1; }

pack_one() {
  local name="$1"
  local dir="${ROOT}/${name}"

  [[ -d "$dir" ]] || die "目录不存在: ${dir}"
  [[ -f "${dir}/agent.json" ]] || die "缺少 agent.json: ${dir}/agent.json"
  [[ -d "${dir}/workspace" ]] || die "缺少 workspace/: ${dir}/workspace"

  mkdir -p "$DIST"
  python3 - "$dir" "$DIST/${name}.zip" "$name" <<'PY'
import json, sys, zipfile
from pathlib import Path

agent_dir = Path(sys.argv[1])
out = Path(sys.argv[2])
expect_name = sys.argv[3]
agent_json = agent_dir / "agent.json"
workspace = agent_dir / "workspace"

data = json.loads(agent_json.read_text(encoding="utf-8"))
assert data.get("name") == expect_name, f"name={data.get('name')!r} want {expect_name!r}"
assert data.get("schemaVersion") == 1, f"schemaVersion={data.get('schemaVersion')!r}"
assert "prompts" not in data or data.get("prompts") in (None, {}), "prompts 不得覆盖"
mcp = data.get("mcp") or []
assert len(mcp) == 1 and mcp[0].get("name") == "artifact-store", "MCP 须仅为 artifact-store"
assert mcp[0].get("url") == "${GRASP_ARTIFACT_URL}", "缺少 GRASP_ARTIFACT_URL 模板"
auth = (mcp[0].get("headers") or {}).get("Authorization", "")
assert auth == "Bearer ${GRASP_ARTIFACT_TOKEN}", "缺少 GRASP_ARTIFACT_TOKEN 模板"
raw = agent_json.read_text(encoding="utf-8").lower()
for bad in ("sk-", "glpat-", "ghp_", "-----begin"):
    assert bad not in raw, f"疑似密钥片段: {bad}"

# 不读取任何密钥文件；仅打包 agent.json + workspace 相对路径
meta = agent_json.read_bytes()
if out.exists():
    out.unlink()
with zipfile.ZipFile(out, "w") as zf:
    # Store agent.json uncompressed（对齐平台 ExportZIP，便于浏览器 peek）
    zf.writestr(zipfile.ZipInfo("agent.json"), meta, compress_type=zipfile.ZIP_STORED)
    for path in sorted(workspace.rglob("*")):
        if not path.is_file():
            continue
        rel = path.relative_to(workspace).as_posix()
        if not rel or ".." in rel.split("/"):
            raise SystemExit(f"非法路径: {rel}")
        zf.write(path, arcname=rel, compress_type=zipfile.ZIP_DEFLATED)

print(f"packed {expect_name} -> {out}")
PY
}

if [[ "$TARGET" == "all" ]]; then
  names=("${ALL_AGENTS[@]}")
else
  names=("$TARGET")
fi

for n in "${names[@]}"; do
  pack_one "$n"
done
