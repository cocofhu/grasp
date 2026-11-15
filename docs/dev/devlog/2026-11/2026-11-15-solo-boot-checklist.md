# 2026-11-15（Solo 沙箱启动进度与工作流统一，按真实状态推进）

- 日期：2026-11-15
- 范围：`web/src/components/run/BootChecklist.vue`（新）、`web/src/components/run/ClarifyBootLoader.vue`、`web/src/lib/run/bootRecord.ts`、`web/src/components/solo/SoloStartupCard.vue`、`web/src/lib/solo/soloBoot.ts`（新）、`web/src/locales/*/pages.json` 及对应测试
- 做了什么：
  - 把工作流节点启动时的折叠工具行（`ClarifyBootLoader` inline starting）抽成 `BootChecklist`，工作流照旧使用，`clarify-boot-*` 测试 ID 不变；`fmtBootSec` 移到 `bootRecord.ts` 共用。
  - `SoloStartupCard` 改用同一个 `BootChecklist`，步骤、标题、当前阶段文案与工作流一致（启动沙箱 / 准备工作区 / 连接 Agent），耗时从沙箱行 `createdAt` 按服务端时钟计。
  - 步骤只跟真实状态走（`soloBoot.ts`）：沙箱 `pulling` / `creating` 且容器未起 → 启动沙箱；容器 `containerStatus` running 但沙箱仍 `creating`（startup.sh 克隆/恢复工作区并启动 ACP bridge，后端不再细分）→ 准备工作区；沙箱 `running`（ACP 连接后写入）→ 连接 Agent；会话 `running` 后卡片消失。同一沙箱内不回退，换沙箱重新开始。去掉原来每 3.5 秒自动前进的计时器。
- 为什么：Solo 原卡片样式与工作流启动动画不同，且创建容器之后的步骤靠计时器推进，与实际进度无关。
- 如何验证：新增 `SoloStartupCard.test.ts`（阶段推导、计时器不推进、轮询丢失容器状态不回退、换沙箱重置、无时间戳不显示耗时）。`npx vitest run src/components/solo src/components/run src/views/GatesInboxView.starting.test.ts src/locales` 通过（`collapsedReplySpacing` 浏览器用例在整组负载下超时，单独运行通过），`eslint`、`vue-tsc --noEmit` 通过。
