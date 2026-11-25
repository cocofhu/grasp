# 2026-11-25（Docker/K8s 沙箱不再空闲回收）

- 日期：2026-11-25
- 范围：`server/internal/services/`（`sandbox_studio.go`、`sandbox_recovery.go`、`sandbox_view.go`、`sandbox_activity.go`、`sandbox.go`、`solo.go`）、`server/internal/handlers/run.go`、`web/src/components/shell/AppSidebarNav.vue`、locales、生命周期与配置文档
- 做了什么：sweeper 按网关 driver 解析恢复类型并按沙箱缓存，非 `snapshot`（Docker/K8s）跳过自动回收；`reclaimLocked` 自动路径对非快照环境直接拒绝。回收倒计时只在已知为 E2B 时给出。Solo 会话视图与运行列表新增 `idleReclaimExempt`，侧栏据此隐藏「永不自动回收」。手动停止、关闭会话、取消运行与销毁逻辑不变。
- 为什么：Docker/K8s 的空闲回收是打包工作区后删除容器（K8s 含 PVC），开发服务器和后台进程被终止，恢复需要冷启动和解包；E2B 原生快照保留内存，代价小。
- 如何验证：新增 `TestContainerSandboxesAreNeverIdleReclaimed`（docker/kubernetes × test/solo/run：到期不回收、无倒计时、自动 reclaim 被拒、手动停止仍保存释放）；原依赖 Docker 自动回收的用例改为 E2B，删除只在 Docker 自动打包窗口存在的 `TestKeepAliveDuringSaveWins`；前端新增隐藏菜单项用例。server `go test ./...`、`go vet`、`gofmt`；web `AppSidebarNav.test.ts`、`vue-tsc --noEmit` 通过。
