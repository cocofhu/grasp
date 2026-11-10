# 本地对象存储 Compose 定义去重并接入 CI 回归

- **日期**：2026-11-10
- **范围**：`compose.objectstore.yaml`（新增）、`compose.release.yaml`、`docker-compose.yml`、`scripts/local-objectstore.sh`、`scripts/test-local-objectstore.py`、`.github/workflows/ci.yml`、`.github/workflows/e2e-restart.yml`。
- **做了什么**：
  - 内置 SeaweedFS 的镜像、用户、命令、端口与健康检查移到 `compose.objectstore.yaml`，两份 compose 通过 `extends` 引用，只各自保留 `local-storage` profile 与数据目录（`.localdata` / `.devdata`）。
  - 镜像摘要只写在共享文件里，`scripts/local-objectstore.sh` 从中读取默认值；新增回归确认两者一致且带 `@sha256:`。
  - `scripts/test-local-objectstore.py` 与 `scripts/init-execution.test.mjs` 此前未在任何 CI 中运行，现接入 `ci.yml` 的 `gate`。
  - `e2e-restart` 的触发路径加入共享文件。
- **为什么**：同一服务块在两份 compose 中逐字复制，镜像摘要另在脚本中出现一次，升级时需三处同步；原先用来保证两份一致的测试实际上没在 CI 中运行。
- **如何验证**：改动前后对两份 compose 执行 `docker compose --profile local-storage config --format json`，结果完全一致；`compose.release.yaml` 叠加 `server/e2e/compose.e2e.yaml` 解析通过；`test-local-objectstore.py`（14 项）、`init-execution.test.mjs`（13 项）、actionlint、shellcheck 通过。
