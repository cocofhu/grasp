# 2026-11-09 已核对 K8s 原环境在冷启动前替换完整授权

- 日期：2026-11-09
- 范围：`server/internal/sandbox/cold_resume*.go`、Gateway client/discovery、`sandbox-gateway/gateway/internal/{api,database,driver,models,service,store}` 的 cold-start 接点。
- 做了什么：增加只读能力预检与原实例目标查询，再通过稳定 request ID 冻结完整配置摘要、注入档案字节和一次派发收据。K8s 只接受控制器已观察到 replicas=0 且无存活 Pod 的原标准 Deployment/PVC；以原 UID/resourceVersion 更新原 env Secret 的整个 Data，清除旧 inline env，并在新进程启动前严格核验新配置。保留原工作目录、Deployment、PVC 和 Secret 身份，不重新 clone。未知结果只 GET 原收据，不再发 Start。SQL admission 与 whole-environment release 使用 Fence→Sandbox→operation 顺序，未确认的冷配置不能被普通 Start、Destroy、Reinstall 或旧 release preview 越过。
- 为什么：旧 Solo 缺少已应用授权证明时，普通启动会先运行旧凭据，启动后再刷新不能构成安全恢复。Docker 的持久 rootfs 不能仅靠 env 替换证明所有旧 CLI 登录态均已撤销；E2B 的暂停内存也不能在恢复前安全替换。因此本次旧环境冷配置仅声明 K8s 能力，Docker/E2B 在 provider mutation 之前明确拒绝；已有完整授权证明的普通启停协议保持原契约。
- 如何验证：Manager 丢响应后原 nonce GET 恢复、改配置/未知查询不重发、unsupported 零 provider write 的 race 回归；两个 Gateway service 共用 SQL 的 16 并发、持久一次派发、完整 Secret 替换、原工作区/UID 保留、CAS 冲突、晚出现 Pod、provider GET 失败、启动 ACK 丢失、替换实例及旧 release preview 拒绝回归。严格预取脚本另有真实 Bash/Python/HTTP/缓存回归。Gateway 全量单测、lint/vet、90% 覆盖门禁；Server 新模块 focused race 和 lint/vet。上述证明区分 client-go API 模拟与真实脚本执行，实际 K8s provider 冷启动仍须在部署新 gateway/app 后独立验收。
