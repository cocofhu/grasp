# 2026-11-18（「电脑」页签首帧前显示 loading）

- 日期：2026-11-18
- 范围：`web/src/components/run/NovncPreviewPanel.vue`、`web/src/components/run/ReactArtifactStage.vue`、`web/src/lib/shared/previewFps.ts`
- 做了什么：
  - `createPreviewFpsCounter` 新增 `painted`：noVNC 第一次 `flip` 后为 true，`reset()` 清零；挂不上 `flip`（私有 API 变动）时直接视为已绘制，避免 loading 卡死。
  - `NovncPreviewPanel` 的 `computer` 视图新增 `computerLoading` 遮罩（spinner +「桌面启动中…」），覆盖连接中以及 `ready` 已到但首帧未绘制的阶段；替换原来不贴切的「正在检测浏览器组件…」。
  - 舞台「电脑」页签在还没有沙箱 id 时，与「浏览器」页签一样显示加载中 / 无沙箱提示，不再整块空白。
- 为什么：沙箱控制通道的 `ready` 会把状态置为 `live` 并撤掉连接遮罩，但此时 noVNC 往往还没画出第一帧，「电脑」页签整块空白，看起来像挂了。
- 如何验证：
  - 本地 `web/` 未安装依赖，未跑 `vitest` / `vue-tsc`，需在 CI 中确认。
  - 用静态 mock 页对比改动前（空白）、加载中、首帧到达三种状态截图检查。
