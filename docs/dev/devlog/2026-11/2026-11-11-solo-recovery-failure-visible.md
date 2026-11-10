# 2026-11-11（Solo 环境恢复失败时在会话上显示失败）

- 日期：2026-11-11
- 范围：`server/internal/services/{sandbox,sandbox_solo,sandbox_solo_recovery,solo,solo_sandbox}.go`、`server/internal/services/sandbox_solo_recovery_test.go`、`CHANGELOG.md`
- 做了什么：恢复确实尝试过但失败时（启动／挂接失败，或运行时工作区核对失败），在同一事务里把会话从 starting／running 标为 error，并写入固定的用户文案；网关和运行时细节仍只记录在环境行和日志中。新增恢复失败回调，推送会话状态。启动时的只读观察可能只看到暂时的 pending，因此仍只记录环境行、不标记会话。后台恢复循环照常每 30 秒重试 error 会话，成功后改回 running；聊天区错误卡片的「重试」对保留环境的会话沿用原工作区恢复。
- 为什么：运行时工作区核对失败直接返回，未调用失败记录，会话一直停在 starting、error 为空，侧栏和聊天区只显示「正在恢复…」，用户无法分辨是在恢复中还是已经失败。
- 如何验证：扩展 `TestSoloRecoveryRetriesSameSandbox`，确认恢复失败后会话为 error 并带固定文案，修复前该断言失败；随后自动重试成功，会话回到 running 且 error 清空。启动观察的回归（pending／error 等状态）仍保持会话 running。
