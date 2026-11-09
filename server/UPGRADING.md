# Solo 续聊版本升级

迁移版保留旧格式读取与离线转换工具；后续清理版将移除已完成迁移的兼容路径。已有 v3.2 数据必须经过迁移版，不能直接跳到清理版。新安装使用空 SQL 与空数据目录，并执行独立执行控制初始化。

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

## 回滚与验证范围

回滚使用独立执行控制的 `restore-begin` / `restore-finalize` 协议，恢复匹配的 SQL、对象和工作区备份，见 [STORAGE.md](STORAGE.md#在隔离位置验证恢复)。只降级二进制、只恢复 SQL 或连同 SQL 回滚控制标记都不能证明原操作不会再次执行。保留原备份、来源文件和票据，升级完成不等于允许删除它们。

本补丁已使用原 v3.2.0 源码生成的 SQLite 数据分别验证本地载荷与 SeaweedFS S3：6,000 条历史事件、原正文、图片、附件 SHA 和旧待执行标识保持一致，同目录重试与独立控制初始化通过，未创建执行轮次或调用模型。原 v3.2 MySQL 的完整升级验证需单独完成后才列入已验证矩阵；已有 MySQL 备份能力不替代该验证。
