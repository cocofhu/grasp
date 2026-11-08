# 持久化存储

Grasp 将会话、权限、索引和任务状态保存在 SQL，将长正文、事件载荷和附件保存在载荷存储中。默认 `local` 使用 `storage.blobs_root`，无需云账号。`s3` 可连接集群内的 SeaweedFS、已有的 MinIO 或其他兼容服务；`cos` 使用腾讯云 COS 的 S3 接口。

沙箱工作区由环境生命周期负责。Docker / Kubernetes 停止环境后会重新启动服务；E2B 的原生暂停能力由对应 gateway 声明。

## 配置

```yaml
storage:
  driver: s3
  blobs_root: /data/blobs
  s3:
    endpoint: http://objectstore:8333
    region: us-east-1
    bucket: grasp
    prefix: production
    path_style: true
```

通过环境变量 `GRASP_S3_ACCESS_KEY_ID`、`GRASP_S3_SECRET_ACCESS_KEY` 注入凭据；临时凭据可附带 `GRASP_S3_SESSION_TOKEN`。Kubernetes 使用 Secret 引用，配置和凭据在部署时注入。完整参数见 [CONFIGURATION.md](CONFIGURATION.md)。

Bucket 需预先创建并设为私有，应用身份需有该 bucket / 前缀的 HEAD、LIST、GET、PUT、DELETE 和分段上传权限。独立部署使用独立 bucket 或前缀，并按部署隔离凭据。不要将对象服务管理端口或管理身份提供给普通应用流量。

COS 配置 `driver: cos`，填写实际 bucket、地域及对应的 HTTPS S3 endpoint。COS 默认使用虚拟主机寻址；`s3` 默认使用路径寻址，可用 `GRASP_S3_PATH_STYLE` 显式覆盖。

自托管部署可在 Kubernetes 中部署 SeaweedFS，使用持久卷保留对象数据，并通过 ClusterIP 向 Grasp 提供 S3 服务。对象服务初始化应复用已有 Secret、bucket 和卷；升级或重启不重新生成凭据或删除 PVC。

## 数据布局与升级

- `payloads/sha256/<前两位>/<SHA256>`：内容寻址的长正文和序列化载荷，SQL 中保留稳定的 `fileref:v1/...` 引用。
- `attachments/<ID>`：原始附件及经过编码的文件元数据，保留原有 `blob:<ID>`。
- SQL 中的 `payload_storage_locations`：物理位置登记，包含 endpoint、bucket、前缀及旧本地目录，不包含密钥。

从本地改为 S3 / COS 时，保持原来的 `blobs_root` 挂载。新写入先保存对象再提交 SQL 引用；后台逐个复制旧正文和附件，回读验证大小及 SHA-256，失败后重试，保留本地源文件。读取旧数据时仍可使用保留的本地文件。

日志中的 `legacy payload archive verified` 表示发现的本地文件已复制并回读校验。删除旧卷前，还需核对 SQL 引用闭包并验证备份恢复。后台归档不会自动删除本地文件。

修改 endpoint、bucket 或前缀会改变既有对象的位置。启动流程拒绝仅改配置的物理切换；迁移必须先复制完整对象集、验证引用与校验值，再切换位置登记。凭据轮换不改变存储位置。

## 备份与故障

备份范围包括 SQL、载荷对象、尚未归档的本地文件、Agent 配置、凭据加密主密钥及 Token 账本日志。SQL 与对象的保留期需一致；从数据库备份恢复时，相应对象版本也应仍然可用。

对象写入失败不会返回可提交的引用，也不会静默改写到其他存储位置。对象读取会验证大小及 SHA-256；权限错误、服务故障与对象缺失分别处理。迁移或对象服务故障时，保留源文件与现有引用，再重试恢复。
