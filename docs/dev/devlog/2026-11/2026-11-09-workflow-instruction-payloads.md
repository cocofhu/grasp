# 2026-11-09 工作流补充指令显式正文边界

- 日期：2026-11-09
- 范围：`server/internal/engine/workflow_instructions.go`、`workflow_instructions_payload.go`、`workflow_instructions_payload_test.go`、`CHANGELOG.md`。
- 做了什么：受理在最外层事务前准备 Text，锁内只写 raw ref 并核验强证明。重复请求以原编码和摘要或 legacy inline 原字节确认，不要求对象存储在线。派发先捕获引用，锁外有界加载，再在 mailbox 事务中核对 Run、visit、attempt、generation、队首 ID/序号和原引用；空队列 seal 同样在锁内二次确认。历史分页仅加载实际返回的 50 条，原模型标签和原收据状态保持兼容。
- 为什么：mailbox、Run 与 StateRun 的隐式正文读取会占住 SQL 锁；读取期间停止、换代或其他执行者抢先派发后，旧预取值不能继续派发，也不能误封闭刚收到新指令的队列。
- 如何验证：独立源码快照完整 server 测试、vet、golangci-lint、配置一致性及核心覆盖率 90.1% 通过，根目录门槛通过。engine/runtime 指令回归在 race 重复 2 次下通过，覆盖阻塞上传后关闭、SQL 失败孤立对象、GET 期间停止/换代/竞争队首、缺正文/错编码、唯一 SQLite 连接释放、正常单次 GET、竞争后再核验、空预取后的新受理、旧 nonce 离线确认及未知操作不重派。本批不包含 Run 终态生成 outbox 时的来源物化，后者单独实现。
