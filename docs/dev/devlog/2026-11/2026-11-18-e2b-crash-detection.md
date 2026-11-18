# 2026-11-18（E2B 沙箱崩溃后 Solo 会话显示失败，不再卡住）

- 日期：2026-11-18
- 范围：`sandbox-gateway/gateway/internal/driver/{driver.go,e2b/driver.go,e2b/state.go}`、`sandbox-gateway/gateway/internal/service/service.go`、`server/internal/services/{sandbox.go,sandbox_solo.go,sandbox_solo_recovery.go,solo.go,solo_sandbox.go}`
- 起因：`grasp-e2b` 宿主机大页耗尽，两个沙箱的 Firecracker 进程以 `failed uffdio copy: cannot allocate memory` 退出。E2B API 的 Redis 记录没清掉，`GET /sandboxes/<id>` 仍报 `running`，只有续期 `POST /sandboxes/<id>/timeout` 每轮返回 500（`sandbox routing lease absent`）。网关只记 warn 日志；服务端 30 秒巡检只处理 `exited`/`not_found`，会话一直显示运行中，用户看到的是「卡住」。
- 做了什么：
  - 网关驱动新增状态 `crashed`。E2B 驱动按续期轮次（`RenewInterval`）记录连续续期失败次数；连续失败 2 次并且沙箱内中继 `/_bridge/health` 也探测不通时，判定崩溃，`Status`/`Get`/`List` 报 `crashed`。续期再次成功会清除判定。判定只存在内存里，网关重启后再过两轮续期重新得出。
  - 只看续期失败或只看中继不通都可能是 API/网络的临时故障，两个信号同时成立才判崩溃。E2B API 的 `SandboxState` 只有 `running`/`paused`，没有可直接读取的崩溃状态；E2B 最终删掉记录后返回 404，走原有 `not_found` 处理。
  - 网关服务层观察到 `crashed` 时把记录从 `running` 改成 `error`（`sandbox crashed`）。
  - 服务端巡检 `ReleaseVanishedSoloSandboxes` 遇到 `crashed`：先通过新的 crashed hook 把会话标为 `error`，提示「沙箱已崩溃（虚拟机异常退出），请重新启动会话」，并取消进行中的回合；然后释放沙箱。有工作区备份的会话改为 `stopped`，提示可从备份恢复。前端错误卡片原有的「重新启动」按钮直接可用。
- 检测时延：最后一次成功续期后的第二轮续期失败时判定（当前部署每 5 分钟一轮，约 5–10 分钟），之后 30 秒内会话变为失败。
- 如何验证：`driver/e2b/crash_test.go`（两次续期失败且中继不通 → `crashed`，一次失败不判，中继正常时不判，续期恢复后清除）、`service_test.go` `TestObservedCrashRecordsError`、`sandbox_solo_recovery_test.go`（`crashed` 释放沙箱并给出崩溃提示）。
