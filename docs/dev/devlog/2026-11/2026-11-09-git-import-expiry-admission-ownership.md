# 2026-11-09 Git 导入预览期限、未受理证明与仓库所有者

- 日期：2026-11-09
- 范围：`server/internal/models/workspace_import.go`、`server/internal/services/workspace_import*.go`、`sandbox-gateway/sandbox/internal/workspacegit/`、`web/src/components/solo/SoloWorkflowImport*`、中英文导入文案、`CHANGELOG.md`。
- 做了什么：新预览保存 24 小时固定期限；读取已授权预览、列表或创建预览时，每次最多撤销同一用户/Solo/Run 的 64 条已过期且未受理归档引用。确认的 SQL CAS 再检查期限；`applying`、`unknown`、`applied`、存在确认收据及旧无期限记录均不回收。仅明确发生于受理前的已知拒绝携带导入 ID、原确认 nonce 和 `confirmationAccepted=false`；页面核对这三项及 4xx 状态后才允许原 nonce 重试。已受理后的相同错误码、SQL 提交故障、5xx 和传输未知仍须核对结果。Git 源仓库的所有者例外只作用于已由 OpenRoot 验证并固定 FD 的原目录，使用 FD 派生且 SameFile 核对的精确 canonical 路径；每个命令独立传入 `safe.directory`，不修改全局/仓库配置，不允许通配信任。
- 为什么：避免从未确认的预览永久保持可执行资格；避免明确未受理时不必要地锁住确认，同时保留未知结果不重放的原约束；支持共享卷中实际 UID 不同但已获工作区授权的 Git 仓库。内容寻址对象可能被其他记录、并发发布或备份引用，因此本次仅撤销过期 SQL 引用，**不执行物理删除、不宣称释放对象存储空间**；全局 destructive GC 仍关闭，物理回收评论仍须按该边界说明。
- 如何验证：服务端导入回归、原生 Git 全包及定向 handler 回归均通过，相关包 `-race -count=3` 通过。新增用例覆盖受理前/后相同错误码、保留原 nonce、SQL 写失败无未受理证明、过期 CAS、扫描边界/跨用户隔离、旧零期限、未知收据保留及共享对象仍可读取；真实 chown 不同 UID 仓库导入、相邻仓库无隐式信任、路径重绑定不转向及通配目录拒绝均通过。前端新增精确证明/错误 nonce/其他导入/5xx/断连/过期状态回归；模块门槛结果随提交交付。
