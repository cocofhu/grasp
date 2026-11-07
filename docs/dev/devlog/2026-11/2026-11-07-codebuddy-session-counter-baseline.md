# 2026-11-07 以原生会话累计量核对 CodeBuddy 每轮消耗

- 日期：2026-11-07
- 范围：`sandbox-gateway/sandbox/internal/provider/{oneshot,streamjson}`、`sandbox-gateway/scripts/test-agent-switching.py`、`CHANGELOG.md`
- 做了什么：原生累计计数与模型明细分别解析；oneshot 按原生会话 ID 保存已入账基线，用累计差量补齐当轮明细。没有末尾快照的失败/中断轮次也推进已知基线，避免后续恢复重复计入。Agent E2E 保存仅含原生用量字段的 CodeBuddy 诊断文件，完整转发真实 CLI 输出，不保存提示词。
- 为什么：仅保留已收到的 assistant 样本仍未覆盖 CI 中 t32 的漏报；原生 CLI 的流式样本与进程模型明细都可能缺一次调用。真实续话确认 flat 计数按会话累计，不能直接作为每轮消耗。需结合累计差量与此前已入账的部分消耗对账。
- 如何验证：进程级回归覆盖七次调用只出现六份样本/明细、恢复历史隔离、失败轮次与下一轮差量、明确零消耗；旧源码 overlay 下失败，新实现及 streamjson/oneshot 竞态检查通过。运行 sandbox lint、vet、全量测试与 90% 覆盖门禁，模型/协议 fixture 和 Python 合约测试；再运行完整原生 Agent 矩阵及 CodeBuddy 连续续话检查。
