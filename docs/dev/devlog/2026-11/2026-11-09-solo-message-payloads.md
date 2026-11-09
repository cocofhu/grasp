# 2026-11-09 Solo 消息正文与历史续聊事务边界

- 日期：2026-11-09
- 范围：`server/internal/services/solo_message_store*`、`solo.go`、`solo_queue*`、`solo_history.go`、`solo_reference.go`、`conversation_{chain,history}.go`、`artifact_delivery.go`、`solo_workflows.go`、`workflow_history*`、`workflow_message_payload_test.go`、`CHANGELOG.md`。
- 做了什么：新增原表原列的无正文 serializer SQL record，强准备证明绑定存储实例，消息读取先释放 cursor 再显式加载。主回复、失败/恢复通知、system 消息、历史续聊和 outbox 消息交付统一准备/提交边界；最终状态及执行权仍在短事务决定，附件 placeholder 只更新文件元数据。历史源在一致 SQL 快照中捕获原始引用，锁外加载和组装，再在创建事务中使用 Run SHARE current read 核验状态、event/control version 与 attempt。消息上传和提交共用保留更早期限的上下文。
- 为什么：只处理队列和 Journal 还不能消除聊天事务中的对象 I/O；历史组装、附件 Save 和结果通知也会经旧序列化器读取或重写正文。冻结版本、附件元数据与最终结果的权属必须保留。
- 如何验证：包含已提交 Journal/P4 与本批消息的独立源码快照通过完整 server 测试、vet、golangci-lint、配置一致性、运行时定向及核心覆盖率 90.1%。Solo/history/reference/chain/artifact/workflow 定向 race 重复 2 次通过；新增真实最终回复上传期间续租、SQL source 捕获、重复 outbox 交付等故障回归通过。最后 Run SHARE 锁修复单独以 race 重复 2 次验证实际事务、精确元数据列、锁声明与提交零 GET/PUT，并审计锁顺序不存在已知反向环。此项只拆分 outbox 的消息交付，Run 终态来源冻结和生产者的对象 I/O 留给独立 manifest 闭环，未宣称全部完成。
