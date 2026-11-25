# 2026-11-25（SSH 网关评审修复）

- 日期：2026-11-25
- 范围：`server/internal/sshgateway/{server,proxy,hostkey}.go`、`server/internal/services/{sandbox_view,sandbox_activity,sandbox_recovery,sandbox_memory_trim}.go`、`server/internal/sandbox/ssh.go`、`server/cmd/server/main.go`、`compose.{release,e2b}.yaml`、`web/src/components/sandbox/SandboxInfoButton.vue`、帮助文档
- 做了什么：
  - 按账号授权：Solo 沙箱只允许会话所有者，管理员可进任意沙箱，工作流等无所有者的沙箱只对管理员开放；不存在和无权访问返回同样提示。注册连接后、拨号前复查公钥与授权；定时复查也检查授权，会话删除或转移后断开。
  - 未认证握手单独限量（全局 64、每来源 8），全局连接名额改为认证成功后占用。
  - 拒绝时不再主动断开，等客户端关闭（上限 30 秒），OpenSSH 能拿到退出码 1 而不是 255。
  - 唤醒、拨号和计入外部连接放在恢复锁内完成，避免 E2B 空闲保存在两者之间暂停刚连上的沙箱。
  - IDE 断开时间只用于自动回收倒计时（`reclaimIdleSince`），手动停止仍只比较模型活动，归档期间关闭 IDE 不再导致停止失败。
  - 上游 SSH 握手受拨号 ctx 的截止时间和取消约束。
  - 主机私钥写入临时文件并 `fsync` 后用硬链接原子发布，多副本首次启动不会读到半截私钥；不支持硬链接的卷退回到 rename。
  - 两个方向的通道请求按顺序转发，关闭客户端通道前等待进行中的回复：快速结束的命令偶尔让客户端在 exec 回复前看到通道关闭（race 下可复现）。
  - Compose 透传全部 `GRASP_SSH_GATEWAY_*`，未设置时为空值，不再覆盖 YAML。
  - 信息面板打开期间每 10 秒刷新会话，连接数和倒计时随 IDE 连接变化。
- 为什么：PR #1369 评审意见（1 个 high、多个 P1/P2）。
- 如何验证：每条修复都有对应单测（授权、握手中撤销公钥、复查断开、未认证握手限量、拒绝后客户端先断开、并发首次生成主机密钥与无硬链接回退、握手超时与取消、手动停止、恢复锁、空环境变量不覆盖 YAML、面板定时刷新）；网关包 `-race -count=20` 全部通过；`docker compose config` 核对 Compose 变量；服务端 lint / vet / 配置文档 / 全量测试 / 覆盖率门禁，前端 lint / vue-tsc / 相关单测 / Playwright。
