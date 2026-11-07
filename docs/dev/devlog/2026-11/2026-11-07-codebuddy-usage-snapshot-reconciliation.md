# 2026-11-07 保留 CodeBuddy 末尾统计快照尚未纳入的用量

- 日期：2026-11-07
- 范围：`sandbox-gateway/sandbox/internal/provider/streamjson/{streamjson.go,usage_totals_test.go}`、`CHANGELOG.md`
- 做了什么：CodeBuddy 的 assistant/flat 输入先扣除缓存，维持四个桶互斥；逐个原生消息 ID 保留当前进程已观察的用量，末尾模型明细不足时补入弱模型桶并标记 partial。只使用本轮流式样本，不将可能含恢复前历史的 flat 总量当成本轮消耗下限。
- 为什么：PR #1131 的 CI 原生 Agent 矩阵中，CodeBuddy 的 t32 发出 7 次模型请求，末尾快照只有 6 次用量，原解析器直接用快照覆盖已知消耗。检查原生 CLI 输出和源码也确认 assistant/flat 与 modelUsage 的缓存输入口径不同，模型明细来自独立更新的 cost service。
- 如何验证：新增回归覆盖 7 次样本与 6 次末尾统计、正常明细、消息 ID 重放、恢复会话的历史 flat 总量隔离、无模型明细及中断样本；以旧源码 overlay 验证回归修复前失败，修复后通过。运行 streamjson/oneshot 竞态测试、sandbox 贡献门禁和完整 Agent 切换矩阵；原生 CLI 与专有平台协议 fixture 继续分别报告。
