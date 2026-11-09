# 2026-11-09 发布诊断先准备真实 runtime 启动包

- 日期：2026-11-09
- 范围：`server/cmd/doctor/main.go` 及测试、`server/internal/handlers/doctor.go` 及测试、`server/internal/sandbox/doctor_bootstrap.go` 及测试。
- 做了什么：`doctor --run-demo` 先通过原 loopback 与 doctor token 保护的诊断会话取得 runtime 下载地址、下载凭据及独立的临时登录密码，再创建明确归属的临时沙箱。会话先建立受当前执行控制保护的真实终态 Run，使用 Manager 当前 runtime 与原 inject store；产物隔离验证复用同一组 Run。普通产物诊断不申请沙箱启动凭据。会话结束或原 TTL 到期撤销自身 bundle，SQL 清理仍核验执行权限；撤权时保留诊断记录，不保留临时下载或 MCP 凭据。
- 为什么：沙箱镜像只提供系统环境，正式启动必须下载服务器 runtime。原诊断直接调用 Gateway.Create 且环境变量为空，fake gateway 测试无法暴露正式镜像缺失 `GRASP_RUNTIME_URL` 的失败。
- 如何验证：CLI 协议回归覆盖先取得 bootstrap 再创建、缺失或无效会话不创建、创建/等待/隔离失败清理及只使用一组会话。真实 BundleStore 回归校验实际 runtime 字节、下载鉴权、并发幂等撤销及其他会话保留；真实 SQL、独立控制目录与 MCP HTTP 回归校验终态 Run、同 Run 读取、跨 Run 拒绝、撤销及权限失效后的记录保留。相关三包 focused race、lint、vet 通过；这些单元和协议测试不代表真实 Docker 或 Kubernetes 镜像验收。
