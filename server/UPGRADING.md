# Solo 续聊版本升级

升级路径为 **v3.2 → v3.3 迁移版 → v3.4 清理版**。迁移版保留旧格式读取与离线转换工具；清理版移除已完成迁移的运行时兼容路径。已有 v3.2 数据必须经过迁移版，不能直接跳到清理版。命令和运行镜像必须来自对应版本，保留选定镜像的不可变 digest。新安装使用空 SQL 与空数据目录，并执行独立执行控制初始化。

这里的版本升级不恢复运行进程，也不重新执行旧请求。工作流、Solo 历史、原附件和沙箱工作区分别保留。Docker / Kubernetes 重新启动服务；进程内存恢复不属于本流程。

## 升级前

1. 停止全部应用副本、调度器、脚本和其他 SQL / 对象写入者，暂停自动扩缩与滚动发布。v3.2 进程没有新的运行登记机制，必须从部署系统核实它们已经退出。
2. 成套备份原 SQL、正文与附件目录或对象、沙箱工作区、配置、凭据加密主密钥及 Token 账本。独立执行控制位置不纳入 SQL / 正文回滚范围。
3. 使用迁移版本镜像内的 `/app/grasp-storage-admin`，或从同一版本源码编译。保留原 `blobs_root` 挂载，明确配置实际 SQL 和目标支持的载荷存储。MySQL 维护环境另需 Oracle MySQL 8.4+ 的原生备份客户端，见 [STORAGE.md](STORAGE.md)。
4. 准备新的私有维护目录，以及与 SQL / 正文相互独立的执行控制配置。配置文件包含凭据，使用 Secret 或权限为 `0600` 的文件，不能提交到 Git。

## 转换并校验

```bash
grasp-storage-admin --mode upgrade --config /private/application.yaml \
  --offline --workdir /private/upgrades/solo-migration
```

维护目录由工具私有创建，记录原操作 UUID、配置身份、原 SQL 快照和 SHA-256。SQL 快照在格式迁移前保存，中断重试不替换它。升级顺序为 schema 迁移、正文规范化、旧 Solo Events 转为历史日志、原引用闭包与对象字节校验，最后同时保存 SQL 完成收据和 `upgrade-complete.json`。

原 Events 字节仍作为历史来源保留，读取绑定原消息 ID、来源摘要及准确事件范围。原版本在 6,000 条处可能已经截断的日志仍显示不完整提示；未记录的执行身份和时间不会补造。原工作流事件、聊天正文、图片与附件保持各自的引用。

确认输出 `completed=true`、`maintenanceActive=true`。成功仍保持停写；失败同样保留维护。缺失对象、损坏 JSON、并发来源变化或收据不一致会阻止完成，不返回部分成功。

中断后使用相同配置、相同原维护目录续办：

```bash
grasp-storage-admin --mode upgrade --config /private/application.yaml \
  --offline --workdir /private/upgrades/solo-migration --resume
```

如有新版本运行登记残留，仅在核实对应进程确已退出后添加 `--acknowledge-stopped <原精确ID[,原精确ID...]>`。不按超时、名称或通配符清除登记。

## 显式完成执行初始化

转换完成不授予执行权限。准备切换时，使用**同一原维护目录**单独执行：

```bash
grasp-storage-admin --mode execution-init --config /private/application.yaml \
  --execution-control /private/independent-control.yaml \
  --offline --workdir /private/upgrades/solo-migration
grasp-storage-admin --mode execution-status --config /private/application.yaml \
  --execution-control /private/independent-control.yaml
```

初始化先重核原 SQL 备份、SQL / 文件收据、历史转换结果和对象引用闭包，再使用原升级 UUID 进入独立执行控制协议。旧待执行及结果不明的 Run、节点和旧排队请求隔离待核对，不自动成为新的可执行请求。

仅当 `active=true`、`sqlMatches=true`、`maintenanceActive=false` 时启动配套新版本应用、网关和运行时。验证原聊天、附件和工作区，再提交新的用户任务。常规部署不得自动升级、初始化或以新控制标记覆盖缺失状态。

初始化中断时使用原目录与原票据续办。激活完成但维护未解除也必须继续原操作；不能手动清除维护或另建目录绕过收据。

## 逐个核对保留的旧 Solo 环境

旧库中仍标记运行、启动中或停机中的沙箱会单独隔离。它们不是可以自动回放的用户请求；隔离保留原 Solo、提供方资源和文件关联。旧库没有新版完整授权证明时，也不能仅将隔离状态改为已核对就恢复旧进程。

完成执行初始化后，继续保持应用和其他写入者停止。通过只读 SQL 核对 `execution_quarantines` 中 `entity_kind=sandbox`、`review_state=pending` 的原记录及其对应的 Solo、所有者和沙箱。对每个需要继续使用的原 Solo 环境单独执行，示例中的 `123` 必须替换为已核对的原沙箱 SQL 主键，每个环境使用独立新目录：

```bash
grasp-storage-admin --mode review-solo-environment --config /private/application.yaml \
  --execution-control /private/independent-control.yaml \
  --offline --sandbox-id 123 --workdir /private/upgrades/solo-review-123
grasp-storage-admin --mode review-solo-environment --config /private/application.yaml \
  --execution-control /private/independent-control.yaml \
  --offline --sandbox-id 123 --workdir /private/upgrades/solo-review-123 --resume --finalize
```

第一步固定原操作 UUID、独立执行身份、Solo 绑定和原提供方资源，核验当前有效的 Agent、模型、Git 及其他凭据，然后只对这一原环境请求保盘停机。只有再次读到同一资源的正向停止证明，才原子保存停机收据、将这一条沙箱隔离标记为已核对，并将原 Solo 和沙箱标记为停止。原文件、历史、其他环境和旧请求的隔离记录不变。第一步成功仍保持维护；第二步在原目录重核并明确解除这一操作的维护。命令不会初始化、激活或轮换独立执行控制。

原队列有待发送、已执行但结果不明的请求、其他父实体仍被隔离、正在切换工作区或已有环境释放操作时，核对会拒绝继续。不能通过此命令取消、重发或批量解除这些请求。若此前已经向尚未核对的 Solo 提交了新消息，应先在原聊天中明确取消该新消息，再停写进行环境核对。

停止响应不明、身份改变或核验失败会保留隔离和维护。使用原目录 `--resume` 只查询原停止结果，不重复发起停止；不要换目录、清除隔离或删除原沙箱来绕过未知状态。已经完成的原 `--resume --finalize` 只返回收据，即使环境后来恢复，也不会再次停机。

核对完成后启动配套新版本应用，在原 Solo 中明确点击“恢复环境”。恢复必须在**原环境启动前**完整写入当前受信任配置和授权；只有原目标、当前授权、执行身份及操作租约再次核验通过，才发布新的授权证明和运行状态。恢复沿用原工作区，不重新克隆或覆盖未提交、未跟踪及非 Git 文件，下一条新的手动消息继续使用原聊天历史。

此旧授权迁移路径目前仅支持具有明确冷配置能力的 Kubernetes 网关。命令在进入 SQL 维护前只读核对原 Solo 绑定和网关能力，派发停止前再次核验。Docker 私有根目录可能残留旧 CLI 授权；E2B 等暂停环境可能保留旧授权内存。两者均在派发前明确拒绝，不停止、启动或删除原环境，也不因此建立 SQL 维护。原隔离记录、聊天和文件保持原样；此类环境须先取得提供方支持的文件备份和经确认的冷迁移路径，再进行环境迁移，本命令不提供这一能力。已有新版完整且当前未变授权的 Docker 环境仍使用普通保盘停启，不属于此旧授权迁移入口。

## 进入清理版前验证实际主存储

在迁移版完成历史转换、实际新消息读写及备份验证后，停止所有写入者，再升级清理版。清理版会在迁移 schema、登记进程或写入对象之前检查规范格式和物理存储证明；执行控制已激活不能代替这些检查。

继续使用本地主存储时，保留 SQL 登记的同一本地路径及原文件。已有 S3 / COS 主存储必须具有实际核验完成的目标副本证明，或使用迁移版提供的独立离线命令认证当前主存储：

```bash
grasp-storage-admin --mode certify-primary --config /private/application.yaml \
  --offline --workdir /private/upgrades/primary-closure
grasp-storage-admin --mode certify-primary --config /private/application.yaml \
  --offline --workdir /private/upgrades/primary-closure --resume --finalize
```

第一步直接从配置指向的主存储读取全部引用及嵌套附件，校验字节、大小与 SHA-256，不使用保留的本地回退，也不上传或删除对象。成功仍保持维护；第二步再次校验并原子完成认证和解除原维护。失败时修复原主存储后，仅使用同一目录 `--resume`；不要删除票据或手动清除维护。确认最终输出 `finalized=true`，并用 `execution-status` 核对原命名空间、dataset、lineage 和绑定状态保持一致。

迁移版的后台归档日志、本地源文件仍存在、空安装来源印记或仅登记 endpoint 均不等于远端认证完成。清理版的远端读取不会回退本地文件；缺失或损坏的主存储对象必须在切换前修复。首次绑定远端的真正空安装可以在同一事务中建立空集合证明；已有主存储不能借空安装来源印记绕过认证。

清理版继续使用既有独立执行控制和原 SQL 收据，**不得再次执行 `execution-init`**。历史查询与新消息使当前引用集合正常增长，不要求其摘要始终等于历史迁移或认证收据。

## 回滚与验证范围

升级目录的 `upgrade.json` 和原 SQL 快照用于证明本次格式转换，**不是**副本恢复所需的 `checkpoint.json`。迁移版完成显式初始化后，保持停写，在新的独立目录执行 `copy --offline`，创建并核验真实 SQL / 对象副本及完整清单，见 [STORAGE.md](STORAGE.md#离线复制切换与恢复)。工作区、配置及主密钥仍需配套备份。

恢复数据时使用当前独立执行控制的 `restore-begin` / `restore-finalize` 协议，在隔离位置恢复匹配的 SQL、对象和工作区备份，见 [STORAGE.md](STORAGE.md#在隔离位置验证恢复)。不能把格式升级目录当作副本恢复票据，也不能随 SQL 回滚控制标记。

清理版回退到迁移版二进制时，必须停写并保留当前 SQL、对象及控制身份，核对配套版本兼容和原执行绑定后再启动，不重新初始化。独立执行控制首次激活后，不能承诺直接回退到不认识该协议的 v3.2 可以安全恢复执行；优先在迁移版向前修复。保留原备份、来源文件和票据，升级完成不等于允许删除它们。

本补丁使用原 v3.2.0 源码生成原始数据，已验证以下完整升级组合：

| 原数据库 | 载荷存储 | 结果 |
| --- | --- | --- |
| SQLite | 本地文件 | 通过 |
| SQLite | SeaweedFS S3 | 通过 |
| MySQL 8.4.13 | SeaweedFS S3 | 通过 |

每个组合均逐项核对 6,000 条历史事件、原正文、图片与附件 SHA，验证原 SQL 备份在同目录重试中不被覆盖、原 UUID 的显式初始化及原票据续办。旧执行保留并隔离，未创建新的可执行轮次或调用模型。MySQL 验证使用原生客户端与服务，保留正常 JSON 统计字段，并校验含保留字列名的引用扫描与正文迁移；不以 SQLite 结果代替 MySQL 验证。
