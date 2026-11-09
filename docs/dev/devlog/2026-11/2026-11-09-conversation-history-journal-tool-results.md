# 2026-11-09（历史回查读取现代 journal 的工具结果）

- 日期：2026-11-09
- 范围：`server/internal/services/conversation_history.go`、`conversation_history_journal.go` 及对应测试。
- 做了什么：在已授权且完成消息分页裁剪后，按消息固定的 queue、turn、generation、From、Through、Count 核验现代 journal，使用现有 JournalPage 读取 ACP，再沿原 conversationTools 合并工具调用的标题、状态和文本。不改变 get_history/read_reference 的参数或输出结构；整页工具过程共享 100000 个事件和 256 MiB 返回事件预算，缺失、损坏、越界均返回错误，不发布部分消息或前进游标。消息正文与旧 Events 的原有加载路径不在此预算中增加网络限流承诺。
- 为什么：现代回复使用 EventLog 指针保存执行过程，原历史工具只转换旧 Events 列，导致换 Agent 后无法按既有历史接口回查已执行工具结果。读取仅使用消息捕获的固定范围；当前 turn 后续追加和无 EventLog 的进行中占位消息不会扩大范围或阻断正常历史。
- 如何验证：真实 durable journal 跨多页合并、递归历史正文省略、外域授权零对象读取、原点及范围篡改、跨代次内容、缺失/损坏对象、分页 lookahead 不加载、请求共享配额和当前 turn 后续追加回归；定向 race、services vet、golangci-lint。完整模块门禁由主集成批次验证。
