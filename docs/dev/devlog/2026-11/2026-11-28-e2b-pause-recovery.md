# 2026-11-28（恢复暂停中的 E2B Solo 环境）

- 日期为 2026-11-28。
- 改动涉及 `sandbox-gateway/gateway` 的 E2B 驱动、`server/internal/services` 的 Solo 服务和 `web/src` 的沙箱信息面板。
- 网关识别 E2B 的 `pause_pending`。Solo 保留原沙箱绑定，并在准备消息期间等待快照可用。信息面板轮询状态，用原会话的唤醒入口继续恢复。
- 暂停请求超时后，E2B 节点可能继续完成快照。Grasp 需要保留会话和沙箱，直到 provider 能确认结果。
- 回归覆盖待决状态、后台核对后的原 ID 重连、Solo 清理保护和恢复按钮。
