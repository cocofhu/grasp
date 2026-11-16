# 2026-11-17（修复云桌面 PR 的 CI 测试夹具）

- 日期：2026-11-17
- 范围：`sandbox-gateway/scripts/test-desktop-session.sh`、`server/internal/services/solo_stop_test.go`
- 做了什么：离线桌面测试用 Python 设置进程名来模拟 Openbox，不再重命名 `sleep`；失败时输出桌面启动日志。停止/恢复测试等待本次停止完成的系统消息后再继续，避免只观察到中间的 `stopped` 状态便修改 GORM 回调表。
- 为什么：Ubuntu 26.04 的 `sleep` 来自多调用入口的 coreutils，重命名后不能作为 Openbox 启动；另外 `Reclaim` 会先更新会话状态，`finishStop` 随后还有最终数据库更新，测试注册故障注入回调时会与之发生数据竞争。两处都是测试夹具的问题，生产桌面和停止/恢复行为不变。
- 如何验证：在 Ubuntu 26.04 容器中复现重命名 `sleep` 的失败，并通过完整的离线桌面测试；停止、冷恢复相关服务测试在 Go 1.26.11 下通过 20 轮 `-race` 回归。执行仓库静态门禁、沙箱模块 lint/vet/覆盖率及运行时 smoke；服务端完整测试和覆盖率、文档构建及依赖审计见本次 PR 检查结果。
