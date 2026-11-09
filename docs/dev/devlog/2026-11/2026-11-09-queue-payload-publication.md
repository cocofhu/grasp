# 2026-11-09 队列正文显式发布与事务边界

- 日期：2026-11-09
- 范围：`server/internal/filestore/prepared*`、`server/internal/chatsession/durable_store*`、`server/internal/chatsession/durable_payload_test.go`、`server/internal/services/solo.go`、`solo_creation*`、`solo_queue.go`、`CHANGELOG.md`。
- 做了什么：增加绑定存储实例的不可伪造正文准备凭据，队列受理事务只写已发布的原列引用。创建首条消息在最外层事务前完成正文保存；领取、历史读取和恢复先释放 SQL 连接再加载正文，派发前重新核验租约、取消和关闭。旧队列导入先准备，再锁内核对原始来源未改变，保留旧序列化标签和引用格式供旧读取器、迁移及备份扫描使用。
- 为什么：对象存储延迟不能占据数据库锁或 SQLite 唯一连接。失效执行者不能在正文加载后继续派发；SQL 失败允许留下可核对的孤立对象，不能留下指向未上传正文的已受理请求。
- 如何验证：在仅含本批改动的源码快照运行完整 server 测试、vet、golangci-lint、配置文档一致性、运行时定向测试及核心覆盖率门槛（90.0%），均通过；根目录三个门槛及 docs 构建、审计也通过。定向回归在 `-race -count=2` 下通过，覆盖阻塞 PUT/GET 时状态、续租、取消和其他已准备提交仍可进行，创建最外事务前上传、SQL 故障回滚、恢复不读取正文、加载后租约失效拒绝执行、旧来源变化完整保留，以及旧读取器和备份核对兼容。此项仅完成队列正文闭环，Journal、SoloMessage 与 WorkflowInstruction 的显式存储边界仍单独推进。
