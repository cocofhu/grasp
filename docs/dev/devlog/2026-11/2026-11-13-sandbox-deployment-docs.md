# 2026-11-13（补齐 Docker、Kubernetes 与 E2B 部署说明）

- 日期：2026-11-13
- 范围：`sandbox-gateway/deploy/{docker,k8s,e2b}/`、`sandbox-gateway/{README.md,gateway/README.md}`、`GATEWAY.md`、根目录中英文 README、中英文快速开始与网关帮助、`server/config.example.yaml`
- 做了什么：新增 Docker 生产 Compose override 与完整操作指南；新增 Kubernetes 单副本部署的命名空间/RBAC、ConfigMap、Secret 引用、PVC、Deployment、Service 和 Ingress 示例及操作指南；补充三种驱动的内网访问、回连、鉴权、数据生命周期、备份升级和产品验收。E2B 说明 provider 白名单的实际配置位置、节点范围和 guest 规则验证，并对齐主分支的新 guest 自动暂停、Solo 按需显式恢复行为。统一文档入口，修正独立预览域名要求、缺失 runtime 的手工 create 示例，以及应用数据目录包含沙箱工作文件的误导。
- 为什么：已有 Docker 快速开始不足以指导生产配置注入；Kubernetes 仅有增量日志权限补丁；E2B 内网说明缺少文件定位与应用步骤。原备份说明遗漏 gateway 状态和固定加密密钥，容易导致无法恢复。示例只使用通用地址和占位凭据，不包含实际部署地址或密钥。
- 如何验证：对照配置加载、驱动、bootstrap 与持久化实现核对说明；此前仅以只读方式核对实际 E2B 部署及上游防火墙实现，没有修改部署或重启服务。使用合成凭据验证 Compose 合并、账号哈希中的 `$` 保留及变量转发；18 个 Kubernetes 对象通过严格 API 类型解码、资源引用与 RBAC subject 校验，两份内嵌配置经真实配置加载器验证。Bash/YAML 示例与文档链接检查通过；docs 安装、构建、high/critical audit、通用 CI gate 和品牌检查通过；同步主分支后 gateway lint/vet/覆盖率 91.3%，server lint/vet/配置文档检查、全量测试、core 覆盖率 90.4% 及 runtime 定向回归通过。Docker/K8s 示例未在实际主机或集群执行部署，真实产品验收仍由部署者按指南完成。
