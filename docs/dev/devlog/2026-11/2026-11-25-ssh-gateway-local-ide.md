# 2026-11-25（内置 SSH 网关与沙箱信息面板）

- 日期：2026-11-25
- 范围：`server/internal/sshgateway/`、`server/internal/services/{sandbox_external_sessions,ssh_keys,sandbox_view,sandbox_recovery,sandbox_memory_trim,sandbox_studio,solo}.go`、`server/internal/sandbox/ssh.go`、`server/internal/models/ssh_key.go`、`server/internal/handlers/ssh_gateway.go`、`server/internal/config/`、`server/cmd/server/ssh_gateway.go`、`sandbox-gateway/gateway/internal/driver/e2b/relay.py`、`web/src/components/sandbox/SandboxInfoButton.vue`、`web/src/views/SSHKeysView.vue`、`web/src/lib/sandbox/`、compose / K8s 示例、`docs/content/{,en/}help/local-ide.md`
- 做了什么：
  - 新增 `sshgateway` 包：对外开放一个 SSH 端口，用户名 `sbx-<id>` 选择沙箱，用 `user_ssh_keys` 表中的公钥认证，再经 `SandboxService.DialSSH`（复用网页终端的凭据和唤醒逻辑）以 root 连到沙箱 sshd，逐通道转发 session（shell/exec/sftp）、`direct-tcpip`、`direct-streamlocal@openssh.com`；拒绝 agent/X11/远程转发。限制每来源认证失败次数、全局和单沙箱连接数；删除公钥立即断开，每分钟复核公钥与账号（覆盖配置文件删账号）。无法连接时在 stderr 输出原因并以退出码 1 结束，IDE 日志可见。
  - `SandboxService` 新增外部连接计数：有 IDE 连接时 E2B 不自动回收、不做内存压缩、不给回收倒计时；最后一个连接断开后从断开时刻重新计时。`TouchActivity` 只用于显示，原方案用它防回收是错误的。
  - `SoloSandboxInfo` 新增 `recoveryKind`、`modelActiveAt`、`destroyAt`、`externalSessions`。
  - 配置 `ssh_gateway.*`（默认关闭）、接口 `GET /api/ssh-gateway`、`/api/me/ssh-keys` 增删查。
  - 前端：Solo 顶栏新增沙箱信息按钮（状态点 + 弹出卡片：状态与主操作、生命周期、本地 IDE、基本信息，窄屏为底部抽屉）；账号菜单新增「SSH 公钥」页。
  - `relay.py` 的 WebSocket 解掩码改为整数异或，2 MB 帧从约 156 ms 降到约 7 ms。
- 为什么：issue #1367，用户希望用本地 VS Code / Cursor 打开沙箱；E2B 没有 IP、Docker/K8s 端点多为内网，统一由 Grasp 服务端中转。
- 如何验证：`go test ./internal/sshgateway/ -race`（含真实 OpenSSH `ssh`/`sftp` 客户端的 exec、退出码、`-W` 转发、sftp 上传、认证失败）；`services` 端到端测试经真实网关 + `SSHKeyService` + `SandboxService` 连到模拟 sshd，并验证连接计数与删公钥断开；回收、内存压缩、倒计时与保存中途连接的单测；配置、接口、启动接线单测；`relay.py` 解掩码与逐字节实现逐长度比对；web 组件与纯函数测试、lint、vue-tsc、全量 vitest、docs 构建。
