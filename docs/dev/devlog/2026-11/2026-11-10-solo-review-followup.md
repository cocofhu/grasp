# 修复非安全来源下的请求 ID 生成与工作流补充要求轮询

- **日期**：2026-11-10
- **范围**：`web/src/lib/shared/randomUuid*`、`web/src/components/solo/SoloWorkflowInstructions*`、`web/src/components/solo/SoloWorkflowImport.vue`、`web/src/components/solo/SoloEnvironmentRelease.vue`、`web/src/components/workflow/RunLaunchModal.vue`、`web/src/components/agent/AgentChatTester.vue`、`web/src/lib/inbox/clarifyDraftStash.ts`、`web/src/lib/inbox/embedPageControl.ts`、`web/LIB_DOMAIN_MAP.json`、`CHANGELOG.md`。
- **做了什么**：
  - 新增共享 `randomUuid()`：安全来源用 `crypto.randomUUID`，否则用 `getRandomValues` 生成合法 v4 UUID。工作流启动弹窗、补充要求发送、代码导入、环境释放直接调用 `crypto.randomUUID()` 的地方改用它；澄清聊天、Agent 测试聊天、页面控制中的三份自写回退合并为这一份。
  - 工作流补充要求：轮询进行中点击“更早记录”不再被丢弃，轮询结束后补发；标签页隐藏时暂停 1.5s 轮询。
  - 代码导入：合并四处 `applying/unknown` 判断；不受支持原因改为按文案键是否存在显示，删除与文案重复的硬编码原因列表。
- **为什么**：自部署常通过 `http://<内网 IP>` 访问，浏览器在非安全来源不提供 `crypto.randomUUID`。`RunLaunchModal` 在 setup 阶段调用，弹窗直接无法打开；其余入口点击即抛错。服务端按 UUID 格式校验这些请求 ID，所以回退必须生成合法 v4 UUID，而原有的页面控制回退只生成 32 位十六进制串。
- **review 结论（未改）**：
  - #1149 的 `objectstore` 服务块在 `docker-compose.yml` 与 `compose.release.yaml` 中重复，镜像摘要另在 `scripts/local-objectstore.sh` 出现一次；发行版 compose 可能被单独下载，暂不拆成共享文件，升级镜像时需三处同步。
  - #1150 代码导入的确认、对账流程有明确的不确定状态与同一 nonce 重试，未发现问题；组件直接用 `req` 拼 URL，与 `lib/api/clients` 的惯例不一致，可在后续拆分时迁移。
- **如何验证**：新增 `randomUuid` 单测（含移除 `crypto.randomUUID` 的回退路径）及补充要求“轮询中加载更早记录”“隐藏时不轮询”回归。`vue-tsc --noEmit`、`eslint .`、全量 vitest（6429）通过。
