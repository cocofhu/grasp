# 2026-11-07 Solo 沙箱启动对账保留数据并恢复原容器

- 日期：2026-11-07
- 范围：`server/cmd/server/main.go`、`server/internal/{models,sandbox,services}/`、`sandbox-gateway/gateway/internal/{api,driver,service}/`、`sandbox-gateway/sandbox/{scripts/grasp-bootstrap.sh,README.md}`、`sandbox-gateway/scripts/test-runtime-bootstrap.sh`、`scripts/build-sandbox-runtime.sh`、`CHANGELOG.md`。
- 做了什么：
  - Solo 使用独立的保守对账逻辑，停止、启动中、错误和查询失败均保留资源；清掉遗留销毁期限，并保护关联名称，避免孤儿清理误删。查询错误与底层明确的资源不存在分开。
  - HTTP 就绪后恢复原容器，按沙箱串行化恢复和显式销毁，不阻塞其他会话或服务关闭。恢复失败定期重试，保留尚未执行的队列；恢复后用保留工作区的准确提示恢复历史上下文。
  - 提前注册 Solo 生命周期钩子，清理已确认丢失的资源及旧版本留下的悬空会话绑定；主动关闭仍释放资源。
  - 运行时包携带 bootstrap，Docker 恢复前复制新版到原停止容器，同时刷新下载输入。Kubernetes 冷启动前刷新 Secret 中的运行时地址。bootstrap 校验并复用已安装运行时，旧创建链接过期不再阻止恢复。
- 为什么：#1120 中宿主机重启后，原容器内文件仍在，但 server 把非 running 状态当成销毁许可，删除可写层；销毁钩子晚注册又留下 running 会话。仅增加 start 无法绕过旧 bootstrap 的过期运行时地址。
- 如何验证：新增服务、网关客户端、Docker/Kubernetes 驱动和 API 回归，覆盖状态查询失败、恢复失败重试、孤儿保护、缺失资源、悬空会话、队列保留与恢复、历史上下文和主动关闭。真实 Docker 用例迁移旧启动入口并连续恢复两次，核对容器 ID 与未提交文件保持不变。根目录门禁通过；server 全量测试、vet、配置文档检查及 runtime 回归通过，核心覆盖率 90.4%；gateway lint/vet/全量测试及覆盖率门禁通过（91.6%）；sandbox 冒烟、lint/vet、预览注入与覆盖率门禁通过（96.6%）；docs 构建与官方 registry 的 audit 通过。新增恢复与队列并发回归在 race 下重复 20 次通过。完整 Agent E2E 覆盖 8 个工具及全部 56 个有序切换组合；Cursor、Trae、Kimi Code 仅覆盖明确标注的协议夹具，不声称原生厂商推理。
