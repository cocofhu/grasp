# 2026-11-11（移除显式离线执行控制初始化步骤）

- 日期：2026-11-11
- 范围：`start.sh`、`compose.release.yaml`、`docker-compose.yml`、`.env.example`、`.github/workflows/ci.yml`、`release-smoke.sh`、`scripts/init-execution.*`、`server/cmd/server/{main,execution}.go`、`server/internal/{executionlineage,engine,chatsession,services}/`、`server/e2e/`、`sandbox-gateway/scripts/`、`README.md`、`README.zh-CN.md`、`docs/content/{,en/}help/configuration.md`、`CHANGELOG.md`。
- 做了什么：删除 `scripts/init-execution.*` 与 `./start.sh [dev] init-execution --offline` 命令；从两套 Compose、E2E overlay 和 `.env.example` 中移除 `GRASP_EXECUTION_CONTROL_*` 环境变量及 `/execution-control`、`/execution-maintenance` 挂载；服务端启动直接使用进程内本地 `executionlineage.NewLocalController(db)`，不再要求停机跑离线初始化或读取外部控制目录。
- 为什么：新环境或开发栈首次 `./start.sh dev -d` 未跑离线初始化时，服务端会静默进入只读历史模式，导致新建 Solo 会话事务失败并向前端返回 503「会话创建尚未确认，请使用原请求重试」。直接去掉外部离线初始化依赖，启动即可使用。
- 如何验证：`go test ./internal/executionlineage/... ./internal/chatsession/...`、`test_agent_switching_test.py`、`test-local-objectstore.py`、`shellcheck-error.sh` 通过；重启开发栈后确认服务直接进入可执行状态。
