# 2026-11-18（E2B 沙箱崩溃后 Solo 会话显示失败，不再卡住）

- 日期：2026-11-18
- 范围：`sandbox-gateway/gateway/internal/driver/{driver.go,e2b/driver.go,e2b/state.go,e2b/tunnel.go}`、`sandbox-gateway/gateway/internal/service/service.go`、`server/internal/services/{sandbox.go,sandbox_solo.go,sandbox_solo_recovery.go,solo.go,solo_sandbox.go}`
- 起因：`grasp-e2b` 宿主机大页耗尽，两个沙箱的 Firecracker 进程以 `failed uffdio copy: cannot allocate memory` 退出。21:56:51 编排器记录 `sandbox crashed` 并移除路由，2 秒后 client-proxy 对该沙箱的请求开始返回 `sandbox not found`。E2B API 每 20 秒和编排器对账一次，但对账只清理节点上多出来的沙箱，不清理 Redis 里已经不在节点上的记录，所以 `GET /sandboxes/<id>` 一直报 `running`，直到租约到期。网关拿到的中继探测错误只是普通 `error`；服务端 30 秒巡检只处理 `exited`/`not_found`，会话一直显示运行中，用户看到的是「卡住」。
- 判定方法：
  - E2B API 报 `running`，而经 E2B 沙箱代理探测中继（`/_bridge/health`）时，代理返回它自己的错误 `{"sandboxId":"<该沙箱>","message":"The sandbox was not found","code":502}`；约 2 秒后再探测一次仍然如此，就判为崩溃。这是 E2B 编排器已移除路由的直接结果，不依赖续期。
  - 中继慢、返回其他错误或连不上（网络、隧道或中继自身的问题）仍然报 `error`，不判崩溃。代理对暂停/恢复中的沙箱返回 `still transitioning` 等其他错误，也不匹配。
  - 没有采用「续期连续失败」：续期只返回通用 500，和 API 临时故障分不开，而且要等 5–10 分钟。
- 做了什么：
  - 网关驱动新增状态 `crashed`。E2B 驱动在 `Status`/`Get`（含 `List`）里识别上述代理响应并复核，判定后锁存，此后直接报 `crashed`，直到沙箱被销毁。锁存只存在内存里，网关重启后会在下一次探测时重新判定。
  - 网关服务层观察到 `crashed` 时把记录从 `running` 改成 `error`（`sandbox crashed`）。
  - 服务端巡检 `ReleaseVanishedSoloSandboxes` 遇到 `crashed`：先通过新的 crashed hook 把会话标为 `error`，提示「沙箱已崩溃（虚拟机异常退出），请重新启动会话」，并取消进行中的回合；然后释放沙箱。有工作区备份的会话改为 `stopped`，提示可从备份恢复。前端错误卡片原有的「重新启动」按钮直接可用。
- 检测时延：服务端巡检每 30 秒查询一次状态，崩溃后通常 30 秒内会话变为失败。
- 如何验证：`driver/e2b/crash_test.go`（代理两次报不存在 → `crashed`；只报一次不判；中继 502 但不是代理的不存在错误不判；只匹配本沙箱 ID 和该错误文本）、`service_test.go` `TestObservedCrashRecordsError`、`sandbox_solo_recovery_test.go`（`crashed` 释放沙箱并给出崩溃提示）。在宿主机上用不存在的沙箱 ID 请求 client-proxy，确认返回上述 JSON。
