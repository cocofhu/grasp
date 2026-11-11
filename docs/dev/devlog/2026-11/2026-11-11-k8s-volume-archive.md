# 2026-11-11（Kubernetes 停止沙箱整盘备份并释放 PVC）

- 日期：2026-11-11
- 范围：`sandbox-gateway/gateway/internal/driver/kubernetes/{volume_archive,volume_exec,kubernetes,cold_start,environment_release}*`、`sandbox-gateway/gateway/internal/volumearchive/`、`sandbox-gateway/gateway/internal/config/`、`sandbox-gateway/gateway/cmd/server/main.go`、`sandbox-gateway/gateway/config.example.yaml`、`sandbox-gateway/deploy/k8s/role-volume-archive.yaml`、`docs/content/{,en/}help/gateway.md`、`CHANGELOG.md`
- 做了什么：新增可选的 `kubernetes.volumeArchive`。后台扫描找出停止超过 `afterSeconds` 的沙箱，用特权辅助 Pod 只读挂载整个数据 PVC，通过 `pods/exec` 流式执行 GNU tar，经 gzip 分片上传到 S3 兼容存储，同时计算 SHA-256 和字节数。核对对象大小后，把归档记录 CAS 写入 Deployment 注解，再以 UID 前置条件删除 PVC。Start 遇到已归档的卷时，只认领恢复并返回，`Status` 报告 Pending；后台核对对象大小后删除残留 PVC，按原容量和存储类重建，流式解压并校验摘要和大小，执行 `sync`，然后在同一次写入里清除注解并扩容到 1，最后删除已用完的对象。冷启动预览和启动、保留数据的重装以及环境释放预览需要原卷，会先触发恢复（不扩容），并返回等待状态；销毁和清空重装会删除归档。
- 为什么：停止只缩容到 0，每个停止的沙箱长期占着一块 PVC。集群不支持 VolumeSnapshot，文件快照又有目录排除和大小限制，无法完整保留缓存、依赖和 Docker 数据。整盘 tar 能保留所有者、权限、xattr 和稀疏文件，恢复后与原卷等价。冷启动本来就不保留进程内存，所以不会损失能力。
- 并发与安全：归档和恢复都持有带租约的认领令牌（10 分钟，每 1/4 周期续租），丢失认领就取消操作。扩容在同一次写入里作废进行中的归档，归档提交时如果副本数不为 0、冷启动待处理或状态已变就放弃。归档在提交确认前不删 PVC，恢复在对象校验通过前不删旧 PVC。丢弃对象前确认没有注解再引用它，防止响应丢失导致误删权威备份。租约过期的 archiving 记录由扫描清理，没有停止时间的旧停机会先补记时间，再等满 `afterSeconds`。
- 限制：每轮扫描串行处理；辅助 Pod 镜像需带 GNU tar（默认使用沙箱镜像）；回收策略为 `Retain` 的 PV 在删除 PVC 后不会自动释放底层磁盘；只释放 PVC，Deployment、Service 和 Secret 保留（不占计算资源）；网关需要额外的 `pods` 和 `pods/exec` 权限。
- 如何验证：新增 fake clientset 加内存存储的回归，覆盖完整的归档和恢复周期、不满足条件时跳过、归档各环节失败时保留 PVC、tar 进行中被启动抢占、启动和扩容作废归档、恢复中停止撤销延迟启动、恢复各环节失败时保留归档、残留 PVC 替换、冷启动和释放等待、销毁和重装删除归档、租约续期和丢失、过期认领清理、冲突重试，以及辅助 Pod 规格。S3 客户端用 httptest 假服务覆盖单次上传、分片上传、分片或完成失败时 abort，以及分片上限；配置覆盖默认值、校验和环境变量。
- 本地检查：网关 `golangci-lint`（v2）0 问题，`go vet` 通过，`cover-check.sh gateway 90` 总覆盖率 90.5%（kubernetes 包 90.2%，volumearchive 100%）；kubernetes 包在 `-race` 下通过。
