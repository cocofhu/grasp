# 迭代热点结构重构 — 零影响协议与落点约定

> 本轮目标：对后续大量迭代一致的热点一次做透**纯结构**边界；**零功能影响**；近期不必再为同一热点结构重拆。

## 1. 手段白名单与黑名单（强制）

### 允许

- 搬文件 / 同包拆文件
- 抽取 composable / 纯函数到 `web/src/lib/{api,inbox,pm,agent,project,run}`
- 改 import 路径
- 测例路径随搬移调整
- 更新 `LIB_DOMAIN_MAP.json` 与本文件落点约定

### 禁止

- 改业务分支条件、默认值、文案、时序、字段语义、错误码
- 改 `api.xxx` 方法名 / 请求体语义 / 返回类型语义
- 改 Go `SandboxService` / `PmService` 导出方法签名或行为
- 夹带修 bug 或行为优化（发现只记账到 follow-up）
- 删减断言凑绿、降低覆盖率阈值

**红灯即停**：任一门禁失败 → 只修搬移导致的引用/导入问题，不得改业务语义后进入下一域。

## 2. 必改与不动清单

### 必改（本轮施工面）

| 面 | 路径 |
|----|------|
| HTTP 客户端 | `web/src/lib/api/api.ts` → 按域拆 client，保留聚合 `export const api` |
| Inbox / Gate | `GatesInboxView.vue`、`GateApproval.vue`、`ClarifyChat.vue` |
| Project / PM | `ProjectDetailView.vue`、`PmLeaderChat.vue`、`RequirementDraftsPanel.vue`、`PmChannelMultiPanel.vue` |
| Agent | `AgentStudioView.vue`、`AgentFilesPanel.vue`、`AgentCreateWizard.vue` |
| Run | `RunDetailView.vue`、`ArtifactPreview.vue`、`ExecutionStatsPanel.vue` |
| Server | `server/internal/services/sandbox.go`、`pm.go` 同包按职责拆文件 |

### 不动

- `engine/` / `gateway/` 大改
- 清单外上帝文件强拆（必要引用修复除外）
- 任何语义收紧
- 新功能、UI 改版、性能专项

## 3. 做透 DoD

每域完成须同时满足：

1. **编排下沉**：主要状态机 / 加载 / WS / 发送 / 选择逻辑落在对应 `lib/*` composable 或同包分文件
2. **落点明确**：后续同轴迭代优先进 lib / 同包分文件；SFC 以模板与接线为主
3. **金锁绿**：该域金锁（§5）通过；禁止删减断言
4. **可自证**：diff 可归类为搬移 / 抽取 / 改 import，无故意行为编辑

禁止半拉子：不得只抽无关工具函数却把编排留在上帝 SFC。

## 4. 冻结对外契约

| 面 | 冻结内容 |
|----|----------|
| 前端 | `api.xxx` / `authApi` / `apiState` / `blobContentUrl` / `isPaginated` 名称与语义 |
| Go | `SandboxService` / `PmService` 导出方法签名 |
| 协议 | HTTP / MCP / 产物字段语义 |

### 兼容逻辑

项目按全新项目处理：旧数据迁移、旧配置项、旧路由与字段别名、旧沙箱协议均已删除，不再保留兼容旁路。

## 5. 域 → 金锁映射

| 域 | 金锁（改前确认存在；改后同一批必须绿） |
|----|----------------------------------------|
| api | `web/src/lib/api/api.test.ts`；`npx vue-tsc --noEmit`；`npm run lint` |
| Inbox / Gate / Clarify | `web/src/lib/inbox/**/*.test.ts`；`GatesInboxView.*.test.ts`；相关组件测；关键路径 `npm run test:e2e:ci` |
| Project / PM / 草稿 | `web/src/lib/pm/**/*.test.ts`；`web/src/lib/project/**/*.test.ts`；`server` 下 `pm_*_test.go` / `pm_test.go` |
| Agent Studio | `web/src/lib/agent/**/*.test.ts`；`AgentStudioView.test.ts`；Agent 组件测 |
| Run / 产物 / 统计 | `web/src/lib/run/**/*.test.ts`；Run 相关组件测；必要 e2e |
| SandboxService | `sandbox_*_test.go`、`sandbox_service_test.go`；`cover-check-server.sh 90`；golangci / vet / configdoc |
| 总回归 | 触及树：web lint + vue-tsc + unit(lines≥85) + e2e:ci；server golangci + vet + configdoc + go test + cover≥90 |

**无锁规则**：无表征测试则先补测，或将该点标为不动，不得裸改。

## 6. 迭代落点约定（交接）

| 后续改动轴 | 落点 |
|------------|------|
| HTTP 客户端 | `web/src/lib/api/` 域 client（`clients/*Client.ts`）+ `api.ts` 仅聚合 |
| 收件箱 / 门禁 / 澄清 | `web/src/lib/inbox/`（必要时邻近 `lib/run`） |
| 项目 / 草稿 | `web/src/lib/project/` |
| PM 会话 / 通道 | `web/src/lib/pm/`；server `pm_*.go` |
| Agent Studio | `web/src/lib/agent/` |
| Run / 产物 / 统计 | `web/src/lib/run/` |
| 沙箱生命周期 | server `sandbox_*.go`（保留 `sandbox_agent` / `sandbox_pm`） |

**禁止**：把业务编排重新堆回上帝 SFC。

## 7. 施工顺序

```
协议/金锁映射 → api → Inbox/Gate → Project/PM(+pm.go) → Agent → Run → Sandbox → 跨域复跑+CI
```

每域：金锁确认 → 纯结构改 → 同一金锁复测 → 绿则下一域。


## 8. 本轮落点自证（实现痕迹）

| 域 | 结构移动证据 |
|----|--------------|
| api | `web/src/lib/api/{httpCore,apiTypes}.ts` + `clients/*Client.ts`（避开 E2E `**/api/{agents,runs,workflows}**` 路径碰撞）+ 聚合 `api.ts`；`api.test.ts` 绿 |
| Inbox/Gate/Clarify | `useGatesInbox` / `useGateApproval` / `useClarifyChat`；SFC 变壳 |
| Project/PM/草稿 | `useProjectDetail` / `useRequirementDrafts` / `usePmLeaderChat` / `usePmChannelMulti` |
| Agent | `useAgentStudio` / `useAgentFilesPanel` / `useAgentCreateWizard` |
| Run | `useRunDetail` / `useArtifactPreview` / `useExecutionStats` |
| Sandbox | `sandbox_{run,test_pool,view}.go`；保留 `sandbox_agent` / `sandbox_pm` |
| PM server | `pm_{memory,thread,message}.go`；签名不变 |
| 协议文档 | 本文件 §1–§7 |

金锁与 CI 阈值未降低。
