# 2026-11-09 发布诊断使用真实受控 Run

- 日期：2026-11-09
- 范围：`server/internal/handlers/doctor.go`、`server/internal/services/artifact_diagnostic*.go`。
- 做了什么：loopback doctor 在签发原有两个隔离 MCP token 前创建真实的诊断终态 Run，创建与清理遵守当前独立执行控制及 SQL 维护保护；清理核对诊断所有者和终态标记，不派发工作流。
- 为什么：生产 MCP 和产物写入会核对持久 Run，原本仅在内存注册命名空间的诊断无法验证正式发布链路。诊断不能成为权限或执行世代检查的例外。
- 如何验证：诊断产物隔离、成功清理、外部执行代次失效后拒绝创建及保留原记录、所有者变化拒绝清理、非法身份拒绝；服务与 handler 相关回归通过 race 三次。发布 digest 全链由单独实际烟测验证。
