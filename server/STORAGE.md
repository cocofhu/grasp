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

v3.2 安装升级到 Solo 续聊迁移版时，先执行[离线版本升级](UPGRADING.md)。正文存储位置复制与版本升级是两个不同操作；普通启动、对象归档和 `execution-init` 不替代历史格式转换。

- `payloads/sha256/<前两位>/<SHA256>`：内容寻址的长正文和序列化载荷，SQL 中保留稳定的 `fileref:v1/...` 引用。
- `attachments/<ID>`：原始附件及经过编码的文件元数据，保留原有 `blob:<ID>`。
- SQL 中的 `payload_storage_locations`：物理位置登记，包含 endpoint、bucket、前缀及旧本地目录，不包含密钥。

从本地改为 S3 / COS 必须停写并执行下文的显式离线复制、验证与切换，保持原来的 `blobs_root` 挂载作为备份来源。清理版不在启动时提升存储位置、归档旧目录或回退读取本地文件。远端缺少或损坏对象时明确返回错误；保留的本地副本不会掩盖故障。

迁移版已经使用远端、但没有经过验证复制切换的部署，先按 [UPGRADING.md](UPGRADING.md#进入清理版前验证实际主存储) 完成 `certify-primary` 及显式 `--finalize`。认证不会复制缺失对象，也不会授予执行权限或允许删除旧卷；缺失字节需先从原备份恢复。

迁移到清理版前，远端主存储还需完成独立的物理引用闭包认证或具备已核验的目标副本证明。`certify-primary` 只读取当前主存储，不接受本地回退代替远端对象；完整命令和原票据收尾见 [UPGRADING.md](UPGRADING.md#进入清理版前验证实际主存储)。

修改 endpoint、bucket 或前缀会改变既有对象的位置。启动流程拒绝仅改配置的物理切换；迁移必须先复制完整对象集、验证引用与校验值，再切换位置登记。凭据轮换不改变存储位置。

## 离线复制、切换与恢复

镜像提供 `/app/grasp-storage-admin`；也可从本版本源码在运维机编译：

```bash
cd server
go build -o grasp-storage-admin ./cmd/storage-admin
```

### SQL 执行绑定与部署检查

服务端与离线工具统一使用 SQL 中的执行绑定（`execution_datasets`）：全新空库自动生成一次绑定，已有绑定沿用原 dataset / lineage，并照常遵守 SQL 隔离记录和离线维护锁。因此 SQL 中的绑定和隔离记录就是执行连续性的依据：**直接把 SQL 备份覆盖回运行库会沿用备份时的绑定，备份时未完成的工作不会被隔离，可能在启动后重新执行**。恢复必须走下文的 `restore-begin` / `restore-finalize`，由它写入新的执行代次并隔离旧工作后再启动服务。

离线操作使用 SQL 维护锁和原私有 `workdir` 中的持久票据，不需要额外的执行控制 bucket、凭据或服务。业务正文、附件和工作区归档仍按前文的 `storage` 配置保存在本地、S3 或 COS。旧 `execution_control` YAML 和 `GRASP_EXECUTION_CONTROL_*` 环境变量不再读取，可从部署配置移除；本版本不会删除已有的控制对象、目录或 Secret。旧 `--execution-control` 参数会明确报错，不再读取其文件。

升级前若有旧工具留下的未完成初始化或恢复票据，先用对应原版本和原目录完成该操作。新工具拒绝旧外部控制票据，不自动转换或绕过其未知状态。已完成初始化的安装继续沿用 SQL 绑定，不重新初始化。

部署前后可执行只读检查，无需停机：

```bash
./grasp-storage-admin --config source.yaml --mode execution-status
```

命令读取普通服务配置及其环境覆盖，只读取 SQL 执行绑定与离线维护状态，不读取对象存储、不执行迁移、登记进程、创建目录或初始化。SQLite 连接禁止写入并读取已提交 WAL；不要使用忽略 WAL 的 immutable 打开方式。

输出为版本化 JSON：`status` 为 `active`、`uninitialized`、`inactive`、`unbound` 或 `unavailable`，并保留 `active`、`sqlMatches`、`sqlDatasetPresent`、`maintenanceActive`。命名空间、dataset、lineage 和 revision 来自当前 SQL 绑定；这些字段用于诊断 SQL 内部一致性，不是独立于数据库的防回滚证明。仅 `active`、SQL 完整匹配且维护已解除时退出码为 0，其余情况均非 0。离线命令尚未解除维护时只允许用原私有目录和原票据显式完成收尾；已有历史或隔离记录不能被当作空安装重新初始化。

新安装无需此步骤，直接启动即可。采用旧历史（含 v3.2 升级）时，先核实所有写入者停止，再在首次启动新版本前运行一次显式初始化；已有升级收据但没有执行绑定的数据库会被服务端拒绝启动：

已经完成版本转换时，`--workdir` 必须使用该次升级的原目录，不能另建目录替代原票据；以下路径按实际原操作目录填写。

```bash
./grasp-storage-admin --config source.yaml --mode execution-init --offline \
  --workdir /maintenance/execution-init
```

保留该私有目录中的原初始化身份和票据；中断后仅以同一目录核对续办。已有历史的待执行和结果不明操作会隔离，不自动重发。滚动发布和 `execution-status` 都不会代替这一步。

该工具用于停写期间的 SQL / 载荷复制，不是在线迁移或沙箱磁盘快照。先停止所有 Grasp 副本、迁移脚本和外部 SQL 写入者；Kubernetes 中关闭自动扩容/滚动发布，在维护 Pod 中挂载原 SQL / blobs 卷、配置与 Secret。先前未提供维护登记的旧版本也必须停止，不能只依赖登记列表判断。

准备私有的 `target-storage.yaml`，结构为前文的 `storage:` 配置，包含目标 S3 / COS 的实际凭据及寻址方式。此文件不使用服务端的 `GRASP_S3_*` 覆盖，防止源环境变量把目标悄悄指回原 bucket。用 Secret 文件或权限为 `0600` 的临时文件提供；不要提交到 Git。目标前缀需独立，同一物理存储中父子前缀也不能互相复制；工具会探测不同 endpoint 别名指向同一重叠位置的情况。

```bash
./grasp-storage-admin --config source.yaml --mode status
./grasp-storage-admin --config source.yaml --mode copy --offline \
  --target-storage target-storage.yaml --workdir /backup/grasp-20261108 --switch
```

`workdir` 必须是新的目录，工具以 `0700` 创建并保留以下文件：

- `database.sqlite` 或 `database.sql`：SQL 快照，保留权限、关联、原始引用和全部表。SQLite 使用 `VACUUM INTO`，包含已提交 WAL；MySQL 使用原生 `mysqldump`。
- `manifest.jsonl`：版本化对象清单、逐对象大小/SHA-256/元数据及完整结束记录；结束记录还包含 SQL 载荷引用集合的摘要。
- `checkpoint.json`：不含密钥的源/目标身份、SQL 文件校验值、操作 ID 和进度。`.partial` 文件不是可恢复备份。

复制包含源前缀内的全部对象，包括暂未被 SQL 引用的对象，保守保留待关联上传和将来扩展的快照。旧本地正文与附件先归档，源文件保留；目标通过独立 GET 验证字节和元数据，随后从原始 SQL 遍历载荷、待发送消息及嵌套图片引用，验证时不使用本地回退。清单完整后才以 SQL 事务切换 `primary`，同时保留旧位置与校验凭据。工具从不删除源对象、旧卷或沙箱。

省略 `--switch` 即创建备份副本，原服务继续使用原位置；备份目标应有独立的故障域。在同一 SeaweedFS 实例中复制到另一个前缀可验证迁移流程，但不能抵御该实例或其磁盘损坏。SQL 文件、清单和目标对象必须成套保留，源配置也要安全留存，以便恢复时识别原位置。

MySQL 导出要求执行工具的环境有 **Oracle MySQL 8.4+ 的 `mysqldump`** 和备份权限；标准 Grasp 镜像未捆绑该客户端，MySQL 运维需在准备了客户端的维护环境执行。连接凭据仅进入短期 `0600` option file，不进入 argv、输出或子进程环境，也禁用隐含 login path；失败不转发可能含敏感信息的原生 stderr。TCP / Unix socket 及标准 TLS 模式受支持，自定义 Go TLS profile 明确拒绝，需另行准备兼容的原生备份配置。原生导入请参考 [MySQL 的导出与恢复说明](https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html)。

### 中断和异常退出

维护状态持久保存在 SQL，不因超时或服务重启自动解锁。运行中的服务先登记，退出时先阻止载荷/附件发布并关闭 SQL，最后移除登记。异常退出保留登记，避免把暂停/断网当作进程已停止；`status` 会显示具体 ID。只有核实对应进程确实终止后，才可在复制命令附加 `--acknowledge-stopped <ID[,ID...]>`，没有通配清理。

登记和离线复制针对持久 SQL。`:memory:` / `mode=memory` SQLite 属于进程内临时数据库，不登记跨进程维护状态，也不支持离线迁移；关闭时不会重连并创建一个无关的新数据库。

修复网络、权限、磁盘或目标冲突后，保持相同配置与 `--switch` 选择续跑：

```bash
./grasp-storage-admin --config source.yaml --mode copy --offline --resume \
  --target-storage target-storage.yaml --workdir /backup/grasp-20261108 --switch
```

续跑保留原 SQL 快照，重新核对源和已复制目标，不把上次 PUT 成功当作已验证。目标同名但不同字节或附件元数据时拒绝覆盖。失败继续保持启动屏障；如决定放弃操作，可用相同 `workdir` 执行 `--mode release --offline` 显式解锁。解锁不会回滚已完成的位置切换；启动配置仍须匹配 SQL 登记，否则启动被拒绝。

### 在隔离位置验证恢复

恢复前先停止所有写入者，并在整个恢复期间保持停止。执行身份、隔离和维护记录均在 SQL 中，直接覆盖数据库无法提供数据库之外的回滚检测。使用当前 SQL 配置执行 `restore-begin`，保留备份本身和原 `workdir`；该步骤取得 SQL 维护锁，并在私有目录持久保存本次恢复身份和原绑定，再允许导入：

```bash
./grasp-storage-admin --config source.yaml --mode restore-begin --offline \
  --target-storage target-storage.yaml --workdir /backup/grasp-20261108
```

随后将 SQL 快照复制/原生导入到**新的隔离数据库**，恢复外部主密钥、配置和所需目录。为恢复数据库准备 `restore-source.yaml`：数据库指向新位置，`storage` 保留备份中原位置身份。目标凭据由原 `target-storage.yaml` 提供。使用同一原私有目录中的票据完成恢复：

```bash
./grasp-storage-admin --config restore-source.yaml --mode restore-finalize --offline \
  --target-storage target-storage.yaml --workdir /backup/grasp-20261108
```

恢复命令检查 SQL 备份文件和完整清单，回读目标对象，核对恢复数据库的载荷引用集合，再以新的 SQL 执行绑定激活目标、隔离备份中的待执行与结果不明操作，并解除本次维护。原 `restore` 是完成阶段的别名，也必须提供开始阶段的原票据。它不自动执行 SQL 导入、不覆盖当前数据库，也不执行沙箱进程恢复或凭据注入。SQL 中的权限/凭据记录是备份时的状态，切流前仍须按当前授权源核对已撤销的权限。新库的服务配置随后改为目标存储，空本地载荷目录也应能读取历史及附件；按业务权限验证原始聊天、历史产物和任务关联后再切流。`--mode verify --offline` 可在同样的已恢复数据库中仅核验，不切换位置。

校验清单用于证明复制和引用完整，不是 GC 删除名单；当前不根据清单自动回收任何源数据。流式历史日志、工作区快照和最终 GC 的业务引用仍需各自纳入生命周期，不以这份副本清单代替。

## 备份与故障

备份范围包括 SQL、载荷对象、尚未归档的本地文件、Agent 配置、凭据加密主密钥及 Token 账本日志。SQL 与对象的保留期需一致；从数据库备份恢复时，相应对象版本也应仍然可用。

复制工具覆盖 SQL 和载荷对象；输出会列出仍需外部配套备份的主密钥、Agent/配置文件、Token 账本及沙箱工作区。仅有 SQL / 对象清单不能宣称完整系统灾备。

对象写入失败不会返回可提交的引用，也不会静默改写到其他存储位置。对象读取会验证大小及 SHA-256；权限错误、服务故障与对象缺失分别处理。迁移或对象服务故障时，保留源文件与现有引用，再重试恢复。
