# Solo 停止备份与恢复

- **日期**：2026-11-11
- **范围**：`server/internal/solobackup/`、`server/internal/config`（`solo_backup`）、`server/internal/sandbox/workspace_archive.go`、`server/internal/sandbox/resume.go`、`server/internal/services/solo_stop.go`、`server/internal/services/sandbox_solo_stop.go`、`server/internal/handlers/solo.go`、`web/src/components/solo/`、`web/src/components/shell/AppSidebarNav.vue`、`web/src/lib/solo/`、`sandbox-gateway/gateway/internal/service/service.go`。
- **做了什么**：
  - 新增 `POST /api/solo/sessions/:id/stop` 和 `/resume`，会话状态多了 `stopping` 和 `stopped`。
  - 停止：先确认会话空闲，然后通过 SSH 执行 `tar -czf -` 把工作区流式写入备份存储，再停止沙箱（E2B 是暂停，Docker 保留容器，Kubernetes 缩容但保留 PVC）。备份或停机失败时，会话保持运行并显示原因；停机失败会删掉刚写的备份。
  - 备份存储：配置了 `solo_backup.s3.bucket` 用 S3 兼容存储（分片上传），否则用本地目录。写入时计算 SHA-256，并按 `max_bytes` 截断。新备份成功后才删旧备份，关闭或删除会话时也会删掉备份。
  - 恢复：原沙箱还在就启动它。E2B 恢复时进程还在，标记为 warm；其他驱动恢复后 Agent 进程重新启动，按冷恢复补发上下文。原沙箱已不存在（gateway 返回 not_found）时，新建一个不克隆仓库的沙箱，ACP 就绪后从备份解包并校验 SHA-256，再连接 Agent。恢复失败时回到 `stopped` 并保留备份，可以重试。
  - 配额：`stopping` 仍占名额，`stopped` 不占；恢复前检查配额。服务重启时被中断的 `stopping` 会话改回 `running`，沙箱若已停止由现有运行中会话的恢复逻辑重新拉起。
  - 前端：输入框旁加「停止环境」（需确认，说明哪些目录不备份），停止后显示停止条，带备份时间、大小和「恢复」按钮；侧栏右键菜单和配额弹窗也能停止或恢复。
  - 配额弹窗（移植 #1201）：会话列表接口返回 `usedSandboxes`（全工作区占用），弹窗显示已用/上限、仍绑定沙箱的失败会话，以及其他成员占用的数量。
  - 网关孤儿清理移植了 #1202 中的安全检查：用请求上下文查询，请求取消时停止；删除前再查一次记录，数据库出错时停止清理。v3.2 本来就会删除孤儿，所以没有移植“只删 Kubernetes”的限制。
- **为什么**：v3.3 / v3.4 为了“保留环境续聊”引入了迁移、存储认证、执行控制等大量机制，实际只需要停止释放配额、之后恢复。本次回到 v3.2，只实现这一能力。
- **如何验证**：`services/solo_stop_test.go` 覆盖同沙箱恢复、新沙箱从备份恢复、恢复失败保留备份、无存储/无备份、停机失败回滚、忙碌和配额拦截、服务重启中断停止；`solobackup` 覆盖本地与模拟 S3 的写入、上限、删除；前端覆盖停止确认、停止条、恢复失败和侧栏与配额弹窗入口。
- **限制与后续**：依赖目录不备份，在新沙箱恢复后需要重新安装；长时间不用自动停止和释放留待后续实现；Docker 和 Kubernetes 不恢复进程。
