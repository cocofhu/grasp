# 2026-11-11（修复 Solo 停止后无法恢复环境，侧栏补充停止／恢复进度）

- 日期：2026-11-11
- 范围：`sandbox-gateway/sandbox/internal/workspacebinding/manager*`、`server/internal/sandbox/{runtime_ensure,workspace_runtime_config}*`、`server/internal/services/sandbox_solo_recovery.go`、`web/src/components/shell/AppSidebarNav*`、`web/src/locales/{zh-CN,en}/pages.json`、`web/e2e/solo-environment-stop.spec.ts`（替换 `solo-close.spec.ts`）、`web/package.json`、`CHANGELOG.md`
- 做了什么：工作区绑定在进程启动时，对路径和 inode 均未变化、仅设备号变化的保留目录更新记录的设备号；同一进程内仍严格校验设备号与 inode，目录被替换或 inode 变化继续拒绝。Solo 恢复在只读核对失败时，若运行中环境的运行时版本落后，就沿用现有运行时更新流程安装新包并在空闲时重启 backend，然后只再核对一次；运行时已是最新则不推送、不重启。侧栏 Solo 菜单移除实际执行关闭会话的「取消运行」和对应确认弹窗；停止／恢复请求期间及服务端仍在收敛时，行内显示「正在停止…／正在恢复…」与转圈，菜单项显示进度并禁用。
- 为什么：Kubernetes 沙箱 PVC 默认使用 iSCSI，每次停止再启动都会挂到新的块设备，设备号改变而文件系统与 inode 不变。`head.json` 记录的旧设备号使 `/api/workspace/binding/head` 永久返回 409 `workspace_binding_changed`，`workspace-configure.sh` 随之以状态 1 退出，环境停在「启动中」，每 30 秒重试仍失败。恢复循环使用不更新运行时的 Attach，已卡住的运行中环境即使部署修复也拿不到新运行时。侧栏点击停止后菜单立即关闭且没有任何进度，服务端尚未确认时也只显示禁用的「重试停止」。
- 如何验证：线上沙箱日志确认能力接口 200、head 接口持续 409，与代码中设备号比对分支一致。新增回归在修复前复现“重新挂载后 head 返回 workspace binding changed”，修复后通过，并确认 inode 变化仍被拒绝；服务端回归确认落后运行时只推送和重启一次后恢复成功，最新运行时不被重启。前端单元测试覆盖菜单项、本地与服务端收敛两类进度；浏览器用例覆盖菜单无取消项、慢停止期间的进度、禁用与不重复请求。
