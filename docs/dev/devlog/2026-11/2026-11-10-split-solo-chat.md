# useSoloChat 拆出模型设置与队列显示顺序

- 日期：2026-11-10
- 范围：`web/src/lib/solo/useSoloChat.ts`、新增 `soloModelSettings.ts`、`soloQueueOrder.ts`

## 做了什么

- 模型与推理强度的乐观切换（请求队列、待确认计数、已确认快照、失效与重置）移到 `createSoloModelSettings`。它与聊天的耦合只剩显式传入的 `sessionGeneration`、`patchSession`，以及 `mergeFetched` / `confirm` / `invalidate` / `reset` 四个入口。
- 队列拖拽的显示顺序覆盖层（`displayQueued`、`reorderQueued`）移到 `createSoloQueueOrder`，收到 `queue_state` 或重新加载时由聊天调用 `clear()`。
- 五处重复的“请求返回时会话是否仍是当前会话”判断统一为 `isCurrent(id, epoch)`。
- `useSoloChat.ts` 从 1340 行降到约 1220 行，对外返回值不变。

## 为什么

`useSoloChat` 内部大量闭包共享状态，一次性拆分风险高。这次只拆状态边界清晰、与帧处理无交叉写入的两块。本地发送确认、历史与过程分页、帧处理与重同步仍共享 `openSends`、`chatEpoch`、`seq` 等状态，留在原文件，后续需先把这些状态收敛为显式对象再拆。

## 如何验证

- 新增 `soloChatParts.test.ts` 直接覆盖两个模块：拖拽顺序与本地项位置、失败回退、待确认选择不被拉取结果覆盖、失败恢复已确认模型。
- 原有 Solo 相关测试（64 文件 / 976 用例）、`vue-tsc`、`eslint`、`libDomainMap` 均通过。
