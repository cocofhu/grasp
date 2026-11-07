# 2026-11-07 修复全来源 Token 计量、持久化与状态栏汇总

- 日期：2026-11-07
- 范围：`server/{cmd/server,internal/{models,tokenledger,sandbox,runtime,services}}`、`sandbox-gateway/sandbox/internal/{provider,service}`、`sandbox-gateway/scripts/{mock-agent-model.mjs,test-agent-switching.py}`、`web/src/{components,views,lib,locales}`、`CHANGELOG.md`
- 做了什么：
  - 工作区、状态栏与全局统计纳入全部账本来源；补全 Solo 筛选、图表与明细。状态栏累计与今日用量用同一 SQL 聚合快照，移除过期缓存；执行数加入 `starting` / `running` 的 Solo 会话。
  - 以真实 ACP operation ID 记录每次调用，用事务收据与原子写入保证重放幂等；先写入 `storage.blobs_root/token-ledger` 持久化日志，数据库失败后自动重放，支持重启恢复。保留缺失与临时测量，可升级为最终快照，已完成的收据不会重复累计。
  - 在调用边界捕获失败、取消确认、连接恢复与 Codex 登录重试的用量；沙箱事件保留 operation ID 和原始报告时间。未知用量与明确报告的零用量分别处理。
  - 校正 Codex 含缓存的输入桶以及恢复会话累计计数；终止快照替换流式临时快照，失败重试和 watchdog 续跑保留此前已知消耗。OpenCode 显式使用平台会话标题，避免原生 CLI 另发未暴露 usage 的标题模型请求。
  - DeepSeek Harness 等待本轮 `turn/end` 投影快照并确认本轮 usage 样本，避免异步写盘时把初始零值或中途快照误记为最终消耗。
  - 状态栏与统计页明确标记未完整报告、待入库及旧账本无法核验的情况。历史缺失和旧缓存重叠没有足够原始证据，保留原值并标记，不能编造回填；数据库和日志目录同时不可写时仅能在内存保留并报告错误。
  - Agent E2E 将每轮四个用量分量逐项对照真实 CLI 发出的本地模型请求，再核对平台状态栏及全局统计累计值；显式区分原生 CLI 与专有平台的协议 fixture。
- 为什么：issue #1124 最初表现为 Solo / Studio 账本已有消耗而状态栏显示「— Token」及活跃数遗漏。进一步核查发现缓存口径、恢复累计值、重试丢失和数据库故障会让仅修复汇总后的数字仍不可靠，因此同时修复采集、持久化与展示，并公开计量完整性。
- 如何验证：
  - 回归覆盖全来源、真实零用量、UTC+8 午夜、活跃状态变化、缓存分桶、恢复会话差量、重试及取消；通过数据库写入故障、并发重放、不确定提交、临时文件和进程重启验证不丢失、不重复及完整性标记。
  - 完整 Agent 矩阵通过：8 种工具、84 轮、全部 56 个有向切换；其中 Claude Code、CodeBuddy、OpenCode、Codex 和 DeepSeek Harness 的 54 轮使用真实 CLI，四桶用量逐项对照本地模型请求，最终状态栏与全局账本总量一致，付费模型调用为 0。Cursor / Trae / Kimi Code 为明确标注的专有平台 CLI-wire fixture。
  - Go 1.26.10：server 全量测试、runtime E2E、golangci-lint v2.12.0、vet、配置文档检查通过，server-core 覆盖率 90.5%；sandbox 全量测试、lint、vet、注入 / Git 凭证 / bootstrap / preview smoke 通过，sandbox-core 覆盖率 96.6%。新增账本、取消、重放与原生适配器路径通过 race 检查。
  - 前端 lint、vue-tsc、全量单测与构建通过，行覆盖率 91.82%；桌面、移动 Chromium / WebKit 共 361 项关键路径浏览器回归通过。模型 fixture 45 项、Python harness 9 项通过。
  - actionlint、error-level shellcheck、7 项 docs-publish transport 回归与品牌断言通过；docs 构建及 high/critical audit 检查通过。
  - 所有运行时逻辑由 server 运行时包发布；未修改沙箱镜像依赖或启动契约。CLI-wire fixture 只验证协议，不声称进行了专有平台的原生模型推理。
