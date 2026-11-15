# 2026-11-15（E2B 生命周期改用官方接口）

- 日期：2026-11-15
- 范围：`sandbox-gateway/gateway/internal/driver/e2b/`、`sandbox-gateway/gateway/internal/driver/driver.go`、`server/internal/sandbox/`、`server/internal/services/`、E2B 部署/API/中英文生命周期文档、`CHANGELOG.md`。
- 做了什么：默认一份使用标准 memory pause/connect；多份保留使用具名独立快照，保存确切状态后删除源 guest，恢复时创建新 provider ID 并保留 gateway 业务 ID/端口。私有 journal 读取 v1、写入 v2，持久记录捕获、释放、恢复与删除意图；固定不可变模板 ID 以免别名改绑误删。原生暂停克隆依赖的基础模板保留删除标记，作为结构依赖不阻塞唤醒，取消后继续清理。普通恢复不再重复 Stop，只有未完成释放重试 Stop；计算容量释放与备份清理分别处理。去掉通用客户端对 `snapshotKeep` 和 `snapshot-status` 的要求，不改配置项、数据库 schema、UI 或沙箱镜像。
- 为什么：让官方 E2B 和可选自建增强使用同一个产品契约；物理落盘/进程退出/GC 由 provider 负责。独立 checkpoint 后 guest 会继续运行，因此不能把它和随后另一份原生暂停当成相同状态并静默回退。请求结果不明时保留意图，先查询认领而不是重复创建；保存失败不删旧点，API 或鉴权错误不伪装成工作负载已释放。
- 如何验证：Go 1.26.11 server 全量测试、lint、vet、配置文档检查、runtime 子集及生命周期 race 通过，核心覆盖率 90.6%；gateway 全量测试、lint、vet 与生命周期 race 三轮通过，门禁覆盖率 90.4%。回归覆盖原生同 ID 恢复、多份淘汰、最新点缺失拒绝回退、响应丢失/重启认领、释放重试不重捕获、明确拒绝与不明请求区分、取消/引用保护及 journal 版本校验。always-on 四项检查和 docs build/audit 通过。另以隔离 KVM 上的官方配套 API/Dashboard/节点验证真实内存与文件恢复、独立快照保留淘汰和 Dashboard 页面；Cloud 账号端到端仍未验收，生产 provider 未因本变更被重启。
