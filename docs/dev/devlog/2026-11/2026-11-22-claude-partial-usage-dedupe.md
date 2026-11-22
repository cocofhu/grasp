# 2026-11-22（Claude Code 中断回合的用量按消息去重）

- 日期：2026-11-22
- 范围：`sandbox-gateway/sandbox/internal/provider/oneshot/`、`sandbox-gateway/sandbox/internal/provider/streamjson/`
- 做了什么：stream-json 解析 `assistant` 事件时带上 `message.id`（`ParseResult.UsageMessageID`）；回合累加器对同一条消息只保留一份用量，后到的样本替换先到的。回合拿到 `result` 总量时仍以总量为准。
- 为什么：Claude Code 把一条 API 消息按内容块拆成多条 `assistant` 事件，每条都带整条消息的用量。回合正常结束时用 `result.modelUsage`，不受影响；stop 或进程异常退出时没有 `result`，只能累加这些事件，输入和缓存读取会按内容块数重复计入。对照 t3code 按 `message.id` 计数的做法。
- 如何验证：用真实 `claude` 2.1 指向一个本地 Anthropic 兼容端点录制事件，确认"文字 + 工具调用"的消息产生两条 `message.id` 相同、用量相同的事件；修复前累加为输入 300 / 缓存读 15000，`result` 给出 200 / 10000。新增 `TestClaudePartialUsageCountsEachMessageOnce` 回放该录制（再加一条同 id、输出递增的样本），断言输入 200、缓存读 10000、输出取每条消息最后一个样本、`usagePartial=true`。另：这些事件里的输出 token 取自 `message_start`（通常为 1），中断回合的输出仍会偏少，已标记为部分用量。
