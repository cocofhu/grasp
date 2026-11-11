# 2026-11-12 恢复 Kubernetes 孤立沙箱自动清理

- 日期：2026-11-12
- 范围：`sandbox-gateway/gateway/internal/{service,config}/`、`sandbox-gateway/gateway/{config.example.yaml,README.md}`、`CHANGELOG.md`。
- 做了什么：启用现有 `orphanGC` 开关时，Kubernetes 网关重新删除数据库无记录的沙箱资源，复用驱动销毁以清理 Deployment、关联 Pod、Service、Secret 与 PVC，也处理仅剩 PVC 的资源。保留年龄门槛、命名空间和托管标签筛选；删除前再查一次记录，数据库异常和上下文取消会停止扫描。其他驱动继续报告并保留未登记资源，默认开关保持关闭。周期扫描日志恢复 `removed` 计数。
- 为什么：仅告警会让已经失去数据库记录的 Kubernetes 沙箱持续占用计算、负载均衡和存储资源，无法按现有配置自动回收。
- 如何验证：新增真实 Kubernetes 驱动与 fake client 的服务回归，修复前确认旧孤立 Deployment 未被删除；覆盖关联资源、仅剩 PVC、年轻资源、已登记停机资源、非托管资源、其他命名空间、重复扫描、数据库读取失败、扫描期间新增记录，以及启动后持续出现的孤立资源自动清理。Go 1.26.11 下网关全量测试及覆盖率门禁通过（90.6% ≥ 90%），lint/vet 通过，清理回归在 race 下重复 20 次通过。环境 `/tmp` 的 setgid 继承导致现有冷启动验证用例拒绝临时目录权限，使用独立、权限 0700 的 `TMPDIR` 后相关用例及完整网关套件通过，未修改冷启动代码。根目录品牌、工具下载、actionlint、shellcheck、Pages 发布运输与本地对象存储回归通过；docs build 和官方 registry 高危审计通过，`git diff --check` 通过。未连接或清理真实集群，未部署。
