# 2026-11-09 对话事件正文显式发布

- 日期：2026-11-09
- 范围：`server/internal/chatsession/journal*`、`server/internal/services/solo_journal*`、`sandbox_operation.go`、`sandbox_operation_deadline_test.go`、`CHANGELOG.md`。
- 做了什么：事件先捕获 generation/head，事务外冻结时间和正文并完成真实对象发布，再在 Queue → Sandbox 的短事务中以 head/owner/generation CAS 同时写原列引用与游标。CAS 失败先回滚整个外层事务，释放锁后重做准备，整体期限为 10 秒。分页和控制帧在 SQL 快照内捕获 ref，释放连接后显式加载。强准备 API 拒绝 autocommit 和其他存储实例的事务；保留 v1 字节、原模型标签和备份兼容。通用环境发布上下文保留原调用方更早的期限，同时允许用户 Stop 后在合法所有权内保存结果。
- 为什么：正文对象延迟不能占据 SQL 锁，预分配序号也不能留下空洞；重试不能改变同批时间或大整数数据。旧 owner、过期保存、跨实例正文或晚到的控制帧不能替换已捕获历史。
- 如何验证：Journal、SoloJournal、SandboxOperation 定向 race 重复 2 次通过，vet/lint 通过；另以 race 重复 3 次验证真实 317 ACP 帧与 48 并发控制帧、外层回滚锁顺序、短期限共享上传竞争及 autocommit/跨 Store 强证明拒绝。回归覆盖阻塞 PUT/GET 时唯一连接可用、租约失效、实际 10 秒超时、原字节和 inline 迁移/备份闭包、缺对象不推进游标。独立源码的完整后端测试、vet、lint、配置一致性、根目录与 docs 门槛均通过；初轮核心覆盖率 89.8% 未通过，补齐 head 更新失败/零行 CAS、最终 guard 或取消失败回滚、退避期间取消等真实故障回归后，原门槛复跑为 90.1%，阈值及核心包列表未改。
