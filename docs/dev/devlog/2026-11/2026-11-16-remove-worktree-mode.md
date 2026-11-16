# 2026-11-16（移除 worktree 镜像和本地执行模式）

- 日期：2026-11-16
- 范围：`server/internal/execution`（删除）、`server/internal/sandbox`、`server/internal/services`、`server/internal/runtime`、`server/internal/handlers/settings.go`、`server/internal/config`、`server/Dockerfile.worktree`（删除）、`sandbox-gateway/sandbox/internal/provider/codex`、`web/src`（设置页、路由守卫、`useBrandSettings` 与各 `worktreeMode` 分支）、`.github/workflows/{ci,publish-image,ci-agent-e2e}.yml`、`sandbox-gateway/scripts`（Agent 切换 E2E）
- 做了什么：
  - 按 #1018 及后续 #1020、#1022、#1030 反向移除 worktree 模式：删除 `execution` 包、`worktree_*` 与 `local_terminal.go`，`Sandbox` 不再有本地执行分支，文件读写、命令、终端、恢复、空闲回收、归档和导出只走网关沙箱。
  - 删除 `GRASP_EXECUTION_MODE` / `GRASP_WORKTREE_ROOT` / `GRASP_WORKTREE_BACKEND`、`GRASP_LAUNCH_KIND` 注入、设置接口的 `execution_mode`、设置页「执行方式」和 worktree 路由守卫；`server.mcp_advertise` 默认值回到 `http://host.docker.internal:<port>`。
  - Codex 恢复为只按 `autoPermission` 加 `--dangerously-bypass-approvals-and-sandbox`；删除 `grasp-worktree` 镜像的构建、发布和镜像契约检查。
  - Agent 切换 E2E 原先依赖 worktree 模式在宿主上直接运行 CLI，改为真实网关（docker 驱动）+ 沙箱：`agent-e2e-sandbox.Dockerfile` 在本次构建的产品沙箱镜像上把 Cursor、Trae、Kimi 换成已标注的平台线协议替身，其余 CLI 必须已在镜像中，缺失即构建失败。删除宿主安装脚本 `install-agent-e2e-clis.sh`、矩阵里的 `package` 字段，以及 CI 的 user namespace 调整步骤；收尾时通过网关删除残留沙箱。
- 为什么：worktree 模式不再需要，保留会让每个沙箱路径都带一条本地分支和对应测试。
- 如何验证：
  - server：`golangci-lint`、`go vet`、`gen-configdoc -check` 通过，`cover-check-server.sh 90` 为 90.7%，`internal/{sandbox,services,runtime,handlers,config}` 测试通过。
  - sandbox：`golangci-lint`、`go vet` 通过，`cover-check-sandbox.sh 90` 为 96.6%；gateway `go vet` 通过。
  - web：`eslint`、`npm run build`（含 `vue-tsc`）通过，`npm test -- --coverage` 562 个文件、6052 个用例通过。
  - E2E：`test_agent_switching_test.py` 10 个用例、mock/fixture `node --test` 通过；本机以 `universal-sandbox:3.3.0`（IMAGE_LEVEL 2，与源码一致）为底构建替身镜像，完整矩阵 8 个工具 84 个用例通过，结束后无残留容器。本机 Docker 为 vfs 且不支持 cgroup 限额，仅本地验证时在临时网关副本里放宽 `docker run` 超时并去掉 `--cpus/--memory`，仓库代码未改。
  - `actionlint` 检查三个改动的 workflow 通过。
