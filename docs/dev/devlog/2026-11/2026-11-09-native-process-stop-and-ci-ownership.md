# 2026-11-09 等待工作区进程组实际退出并修复验收进程归属检查

- 日期：2026-11-09
- 范围：`sandbox-gateway/sandbox/internal/workspacejob/process.go`、`sandbox-gateway/sandbox/Dockerfile`、`sandbox-gateway/scripts/test-agent-switching.py`、对应 Python 测试与 `server/internal/services/solo_model_route_test.go`。
- 做了什么：工作区任务发送 SIGKILL 并回收直接子进程后，继续核对同一进程组，只有所有成员均不能执行才结束监督并释放原任务通道；无法读取进程表时保留未确认状态。原生 Agent 验收先按进程名排除无关的同用户进程，再读取受保护的 exe/cwd；命中的 backend 仍须完整证明归属，权限错误仍失败。历史读取故障测试在 worker 启动前注册 GORM 回调，用原子开关注入故障，避免运行中重编译回调注册表。沙箱镜像 npm 下载默认使用官方源，允许显式选择源并限制请求重试时间。
- 为什么：CI 真实复现了信号已送达而子进程仍可执行、无关受保护进程导致验收清理失败、测试回调注册表数据竞争及镜像源超时。信号返回不能作为停止计算的证明，也不能通过跳过真实 backend 身份检查掩盖清理失败。
- 如何验证：原生 workspacejob 停机回归使用 `-race -count=30` 通过；Python 完整回归包含无关受保护进程与匹配 backend 权限拒绝两条分支；历史读取故障回归使用 `-race -count=20` 通过。完整模块门禁、原生 Agent 矩阵与镜像构建由最终统一源版本执行，定向结果不替代这些门禁。
