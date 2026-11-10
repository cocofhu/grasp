# 2026-11-09 工作区文件采集、校验传输与隔离目录恢复

- 日期：2026-11-09
- 范围：`sandbox-gateway/sandbox/internal/workspacesnapshot/`、`workspacejob/`、`handler/`、`router/`、`cmd/backend/`，gateway `internal/service/`，server `internal/workspacefiles/`、`internal/sandbox/`、`internal/services/workspace_snapshot*` 及快照模型。
- 做了什么：运行时增加真实文件采集、独立清单、受限 Git 对象及索引复制、分块传输和新目录物化；上传先取得持久占用，完整回读校验后，在同一元数据事务发布引用固定、ready 快照与完成回执。新读取任务使用新代次，过期采集任务不会被重新激活。后台只在认证、独立私有控制目录、恢复目录与可信资源身份齐备时开放能力；网关及本地沙箱固定真实资源身份，工具重配保留创建时的执行身份。
- 为什么：代码增量不能代表完整工作区。需要保留未推送提交、暂存区、冲突、未跟踪文件和二进制内容，同时避免复制平台认证、原生 CLI 私有状态、Git 凭证、配置及钩子。采集采用前后内容核对的 verified-copy 语义；不承诺文件系统原子快照、RAM 恢复或恢复原生工具进程。恢复始终创建独立目录，后续修改及来源目录保留；产品激活、Return、按节点采集及显式释放另行接入。
- 如何验证：以固定源码运行完整 server、gateway、sandbox 的 lint/vet、匹配测试和覆盖率门禁，核心覆盖率分别为 90.1%、91.4%、96.6%；root gate 与 docs 构建及审计通过。真实独立 backend 进程、认证 HTTP、原生 Git、生产传输客户端与对象存储契约 fixture 完成采集→分块读取→上传/完整回读→SQL ready/pin→新目录恢复；第一笔上传故意延迟 50 秒，在生产 45 秒租约及续期策略下通过，耗时 52.22 秒。运行时与服务端重点回归各通过 race 三轮，覆盖来源变化、部分恢复保留、坏清单、越界链接、忽略规则和认证目录排除。此 fixture 不替代真实 COS/S3 网络验证，也不代表产品激活和释放已完成。
