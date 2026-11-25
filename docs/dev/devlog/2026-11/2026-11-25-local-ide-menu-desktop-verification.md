# 2026-11-25（本地 IDE 菜单与桌面 IDE 实测）

- 日期：2026-11-25
- 范围：`web/src/components/sandbox/{SandboxIdeMenu,IdeLogo,SandboxInfoButton}.vue`、`web/src/lib/sandbox/sshConnect.ts`、`web/src/assets/ide-logos/`、`docs/content/{,en/}help/local-ide.md`
- 做了什么：
  - 信息面板的两个 IDE 按钮改为分体按钮 + 菜单：主按钮用上次选择的 IDE 打开，菜单列出 VS Code、Cursor、CodeBuddy、Trae（`CN` 打开国内版 `codebuddycn://` / `trae-cn://`）和 JetBrains Gateway，最后一项是「SSH 连接信息」（用户名、主机、端口、命令、可选别名、主机指纹）。品牌图标本地打包，来源见 `assets/ide-logos/README.md`，单色图标按文字颜色绘制。
  - 链接从依赖 `~/.ssh/config` 别名的 `sbx-<id>@grasp` 改为 `sbx-<id>@<host>:<port>`。
  - JetBrains Gateway 不提供一键链接：它的 SSH 链接要求 `deploy=false` 且带 `idePath`（`SshLinkConnectionDetailsKt.validateSshLinkParams`），只能连主机上已装好的 IDE，所以改为展示连接信息。
  - 修复菜单收起时仍吞掉 Esc、导致面板关不掉的问题；菜单改为在面板内展开，避免被滚动区域裁切。
- 为什么：用户希望和 MiniInfra 一样用菜单列出多种 IDE，并使用品牌图标；`user@host:port` 让一键打开不再需要额外配置。
- 如何验证：在云桌面（Xvfb）上实测，后端是 MiniInfra 上的 Docker 部署：VS Code 1.148 + Remote-SSH 0.128 用 `ssh-remote+sbx-1@192.168.2.184:2222` 连上并在集成终端执行命令；Cursor 3.32 + anysphere.remote-ssh 1.1.19 经 `ssh -D` 建立连接并启动 cursor-server（界面需登录 Cursor 账号）；JetBrains Gateway 2026.3（SSHJ）连接成功并把 IntelliJ 后端部署进沙箱，JetBrains Client 经转发端口连上后端，停在许可证页。在真实 Chrome 中从 Grasp 面板点击 VS Code、Cursor 链接，均拉起对应 IDE 并连上沙箱，面板显示 2 个连接。单测与 Playwright 用例覆盖菜单、CN 版、偏好记忆、JetBrains 详情和 Esc 行为。
