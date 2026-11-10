# 2026-11-09 正文读取界限与可取消发布锁

- 日期：2026-11-09
- 范围：`server/internal/filestore/context_mutex*`、`store.go`、`prepared*`、`CHANGELOG.md`。
- 做了什么：正文临时文件创建、最终对象发布和远端后端读取的共享锁均使用可取消等待；短请求无需等待另一笔长上传。关闭仍先取消本存储的发布，再等待活动操作退出，保留原关闭及命名空间隔离契约。增加统一 `LoadBounded`，对旧 inline 与引用正文同时限制保留字节、核验编码与实际 SHA256。
- 为什么：仅把 S3 I/O 移出 SQL 事务还不够；不可取消的全局上传锁仍会延长 Journal 等短请求的整体截止时间。有界加载也不能依赖读取后才发现过大的数据。
- 如何验证：filestore、objectstore、payloadstore 全包 race 重复 2 次通过，包含长上传期间短 Prepare/Open 按期限退出、关闭取消与收尾、失败证明不可用、有界 inline/引用读取、错误编码及损坏正文拒绝。独立源码快照的完整 server 测试、vet、golangci-lint、配置一致性、运行时定向测试及核心覆盖率 90.0% 均通过；根目录门槛与 docs 构建、审计通过。该批与消息、Journal 的调用方改造分开提交，后续集成仍须重跑对应检查。
