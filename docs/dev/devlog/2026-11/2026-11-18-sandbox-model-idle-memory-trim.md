# 2026-11-18（沙箱只按模型回合空闲回收，E2B 空闲内存归还主机）

- 日期：2026-11-18
- 范围：`server/internal/services/sandbox_{activity,recovery,studio,run,view,memory_trim}.go`、`server/internal/services/solo.go`、`server/internal/sandbox/{idle_activity,memory_trim}.go`、`server/internal/config/`、`server/internal/handlers/{solo,run,dto}.go`、`server/internal/router/router.go`、`server/internal/models/`、`web/src/components/shell/AppSidebarNav.vue`、`sandbox-gateway/sandbox/internal/{handler/runtime.go,service/chats.go,provider/{acpx,deepseek,oneshot}}`、相关 API 客户端、文案与文档
- 做了什么：
  - 实现 [#1285](https://github.com/cocofhu/grasp/issues/1285)。`sandboxes.model_active_at` 记录最近一次模型回合运行、结束或环境就绪的时间，空闲回收和清缓存都从它计时。回合结束（Studio/Solo 对话、运行节点交还或等待人工）、恢复和就绪时写入；空闲扫描遇到执行中的回合也会刷新它。`TouchActivity` 只更新展示用的 `last_active_at`，不再续期。旧行没有 `model_active_at` 时回退到 `last_active_at`，升级不会立刻回收现有环境。
  - 进入沙箱的最终检查改为只读 `/api/runtime/activity`（回合和排队），删除进程扫描。沙箱运行时随之删除只为进程扫描提供的 `resident_pids` 字段和各 provider 的 `ResidentAgentPIDs`；旧服务端读取时按缺省空列表处理，兼容。
  - `solo_sessions.keep_alive` 和 `runs.keep_alive` 保存「永不自动回收」，在加锁的回收路径和保存完成、释放之前各检查一次。新增接口 `PUT /api/solo/sessions/:id/keep-alive` 和 `PUT /api/runs/:id/keep-alive`，关闭时重新计时；运行列表返回 `keepAlive`。默认空闲时长改为 360 分钟。
  - 新增 E2B 空闲清缓存：空闲扫描在模型回合结束 10 分钟后（`sandbox.memory_trim_*` 可配）通过 SSH 执行写盘、`drop_caches=1`、`compact_memory`，并把 `compaction_proactiveness` 设为 20。只对网关驱动为 `e2b` 的沙箱执行，每个沙箱同时最多一个后台任务，间隔 30 分钟，缓存低于 1 GiB 时跳过。
- 为什么：192.168.2.18 的 E2B 主机大页池固定 54 GiB。五台沙箱程序实际使用约 8.5 GiB，主机却被占了 37.5 GiB：其中沙箱页面缓存约 19 GiB，碎成小于 2 MiB 小块、无法经空闲页上报归还的空闲内存约 10 GiB。结果 16 GiB 新沙箱因启动时需要 18 GiB 余量而创建超时。另一方面，原空闲判定把桌面、Chromium 等常驻进程和 IDE/预览访问都当成活动，环境长期不回收。`compaction_proactiveness` 在模板里是 0，单清缓存并不能让内存回到主机。
- 如何验证：
  - 在线上沙箱 `i9u93k5…` 上手动执行同样三步，耗时 3 秒：缓存从 5.3 GiB 降到 1.3 GiB，不小于 2 MiB 的整块空闲从 16 MiB 增至 1.85 GiB；20 秒后主机侧 Hugetlb 占用从 7.91 GiB 降到 4.60 GiB。
  - 新增服务层测试，覆盖：访问不续期、沙箱内回合与控制面回合续期并持久化、会话和运行「永不自动回收」、手动停止不受影响、保存期间开启保留后放弃释放、清缓存的延迟、间隔、容器跳过和关闭策略。
  - 清缓存脚本在伪造的 `/proc` 下实际执行。另有接口测试、配置默认值与环境变量覆盖测试，以及侧栏菜单切换测试。
