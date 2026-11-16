# 2026-11-16（桌面镜像新增「电脑」页签：画面卡片 + 控制胶囊；页签放大）

- 日期：2026-11-16
- 范围：`web/src/components/run/NovncPreviewPanel.vue`、`web/src/components/run/ReactArtifactStage.vue`、`web/src/components/run/ResourceCardIllustration.vue`、`web/src/lib/run/reactArtifactPreview.ts`、`web/src/views/SandboxConsoleView.vue`、中英文 `pages.json`，及相关测试
- 做了什么：
  - `NovncPreviewPanel` 新增 `computer` 属性。在桌面镜像（`useDesktopControl` 报 `mode: desktop`）上，打开该属性后不显示地址栏和原来的状态条：远程画面作为圆角卡片居中（noVNC 背景设为透明，圆角与描边加在 canvas 上），下方居中的控制胶囊显示控制者、Agent 运行 / 空闲、桌面启动中，以及取点标注、全屏和「接管 / 交还」。接管时画面描边变为强调色，接管说明放在胶囊的 title 和读屏文本里。画面底色和胶囊在舞台内使用 `--ra-*` 变量，在其它页面回退到 `--c-*` 主题色。
  - 舞台和沙箱控制台在桌面镜像下保留原来的「浏览器」页签（工具栏和行为不变），另外新增「电脑」页签和资源卡片（显示器图标、插画），使用上面的卡片 + 胶囊视图。浏览器镜像不出现「电脑」页签。公开分享 token 不查询桌面状态。应用预览保持原有工具栏。
  - 资源卡片插画：「浏览器」从地球图标改为网页线框 + 虚线取点框 + 指针（对应「可取点标注」）；「应用预览」不再用半透明的通用地球图标，改为同一套窗口插画：带绿色运行点的标题栏和柱状图面板，与 IDE / 终端 / 电脑 / 日志一致。
  - 「电脑」页签只在确认是桌面镜像时出现。服务端明确返回非桌面时：控制台 `?tab=computer` 回到「浏览器」；舞台去掉恢复出来的「电脑」页签，并卸载面板，不在后台保留 VNC 连接。查询失败（沙箱暂停、运行时过旧）时保持原状。
  - 舞台页签与沙箱控制台页签放大：13px 字号、15px 图标、更高的点击区域。
- 为什么：原来的状态条只是几段纯文字并排，观感简陋。参照 dot 的电脑页，「电脑」以画面为主体，控制权集中在一个胶囊里；「浏览器」保留给需要地址栏的场景。
- 如何验证：
  - web：`eslint`、`vue-tsc --noEmit` 通过；`vitest run src/components/run src/views src/components/solo src/lib/run` 全部通过（226 个文件，2718 个用例）。
  - 本地起真实 server（SQLite 写入模拟沙箱与 Solo 会话）+ web dev，桌面状态与 VNC 经仓库外的开发代理接到本机 x11vnc。在 Solo 舞台的「浏览器」「电脑」页签中按浅色 / 深色截图检查。
  - 用 e2e Vite 临时演示页（不入库；用一个画出假桌面的 noVNC 替身）在 Agent 运行中 / 空闲 / 启动中 / 他人控制 / 接管后几种状态下截图检查。
