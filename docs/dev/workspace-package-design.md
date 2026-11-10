# Workspace 拆分为独立子包：接口设计

状态：设计稿，第 0 阶段已完成（文件归位），后续阶段逐个 PR 推进。

## 现状（2026-11-10 测量）

`server/internal/services` 共 192 个非测试文件，其中 `workspace_*` 47 个、约 1.08 万行。前缀下实际混着两类代码：

- **工作区运行时核心**（38 个文件）：binding、snapshot job / pipeline / publication、restore、release、import、authorization、capture、execution。
- **借用前缀的其他功能**：凭据（`credential_service.go`）、Token 统计分桶（`token_stats_buckets.go`）、MCP API Key（`mcp_api_key.go`）这三个已在第 0 阶段改名移出；`workspace_agents*`、`workspace_vcs*`（Agent 工作区目录与版本）、`workspace_vars`、`workspace_external_mcp` 属于 Agent 配置，不随核心迁移。

核心与包内其余代码**双向依赖**，无法直接移包（Go 不允许循环导入）：

| 方向 | 依赖 |
| --- | --- |
| 核心 → sandbox | `acquireSandboxOperation`(9)、`commitSandboxOperationPrepared`(8)、`commitSandboxOperation`(5)、`acpConnection*`(6)、`beginSandboxAccess`(3)、`effectiveAgent`(3)、`updateSandboxOperation`、`teardownLive`、`soloRecoveryAuthorization`、`currentAuthorizationFingerprint`；`errSandboxOperationLost`(10)、`sandboxOperationKey`、`sandboxOperationLeaseTTL` |
| 核心 → solo | `SoloService.Require`、`publishStatus`；`soloQueueScope`(8)、`messageOutsideTransaction`(8)、`soloErr`(9)、`prepareSoloMessage`、`insertPreparedSoloMessageTx`、`lockSoloCompute`、`selectSoloConfiguration` |
| 其余 → 核心 | `SandboxService.guardWorkspaceReleaseTx` / `canonicalSandboxAccess`、`PreviewService` 的预览绑定校验、`GuardWorkspaceBindingTx`、`closeStoppedWorkspaceConsumersTx`、`ErrWorkspaceJob*`、`WorkspaceAuthorization.ReadScope`；`solo_queue.go` 直接依赖 `workspace_authorization` / `workspace_binding` |

此外核心文件里定义了外部类型的方法：`SandboxService.SetWorkspaceBindings` / `WorkspaceFolder` / `BeginWorkspaceConsumer` / `canonicalSandboxAccess` / `guardWorkspaceReleaseTx` / `checkWorkspaceRelease`，以及 `PreviewService.BindPreviewPort` / `BeginPreviewConsumer` 等预览绑定方法。

## 目标分层

```
internal/sandboxop      沙箱操作租约（sandbox_operation.go，只依赖 models / execution lineage）
internal/workspace      核心：binding / snapshot / restore / release / import / authorization
internal/services       Sandbox / Solo / Preview / Agent 等，实现 workspace 需要的端口并注入
```

`workspace` 只依赖 `models`、`sandboxop` 和自身定义的端口接口；`services` 依赖 `workspace`。

## 端口接口（由 services 实现，workspace 定义）

```go
// Sandbox 侧能力：租约之外的沙箱读写全部经此接口。
type SandboxRuntime interface {
    Connection(ctx context.Context, sandboxID uint) (ACPConnection, error)
    BeginAccess(ctx context.Context, row *models.Sandbox) (context.Context, func(), error)
    EffectiveAgent(row *models.Sandbox) (*models.Agent, error)
    TeardownLive(sandboxID uint)
    RecoveryAuthorization(ctx context.Context, row *models.Sandbox) (Authorization, error)
    AuthorizationFingerprint(row *models.Sandbox) (string, error)
}

// Solo 侧能力：会话校验、状态广播与消息落库。
type SoloSessions interface {
    Require(ctx context.Context, sessionID string) (*models.SoloSession, error)
    PublishStatus(sessionID string)
    QueueScope(sessionID string) string
    LockCompute(tx *gorm.DB, sessionID string) error
    SelectConfiguration(tx *gorm.DB, session *models.SoloSession) (string, error)
    PrepareMessage(in SoloMessageInput) (PreparedSoloMessage, error)
    InsertPreparedMessageTx(tx *gorm.DB, msg PreparedSoloMessage) error
}
```

反向调用改为 workspace 导出、services 注入的守卫（沿用现有 `SetWorkspaceBindings` 的注入方式）：

```go
type Guards interface {
    GuardReleaseTx(ctx context.Context, tx *gorm.DB, sandboxID uint) error
    GuardBindingTx(tx *gorm.DB, sandboxID uint) error
    CloseStoppedConsumersTx(tx *gorm.DB, sandboxID uint) error
    CanonicalAccess(ctx context.Context, row *models.Sandbox) (context.Context, error)
    CheckPreviewBindingTx(tx *gorm.DB, rec mcp.PreviewPort) error
}
```

`SandboxService` / `PreviewService` 上现有的工作区方法保留为 services 内的薄适配层，委托给 workspace，对 handlers 保持原签名。错误统一导出（`workspace.ErrJobConflict` 等），services 侧 `errors.Is` 判断不变；`soloErr` / `SoloError` 换成 workspace 自有错误类型，在 services 边界映射。

签名中的 `ACPConnection`、`Authorization`、`SoloMessageInput` 等为占位，落地时以现有私有类型为准，按最小所需方法收窄。

## 分阶段计划

每阶段一个 PR；门槛：`go build ./...`、`go vet`、`go test -race ./internal/...`、restart-reattach e2e。

0. **文件归位**（已完成）：非工作区功能改名移出 `workspace_` 前缀，核心边界在文件名上可见。
1. **租约下沉**：`sandbox_operation.go` 移到 `internal/sandboxop`，导出 `Acquire` / `Commit` / `CommitPrepared` / `Update`、`ErrLost`、`Key`、`LeaseTTL`。它只依赖 sandbox 行模型和 execution lineage，风险最低。
2. **定义端口**：在 services 内先引入上面三个接口，由 `SandboxService` / `SoloService` 实现，核心文件改为经接口调用（仍在同一个包里）。这一步可以逐个文件做，行为不变。
3. **拆反向依赖**：`solo_queue.go` 对 authorization / binding 的直接调用改走 `Guards`；外部类型方法移成适配层。
4. **移包**：核心 38 个文件整体移到 `internal/workspace`。此时只剩机械改名（导出必要标识符），由编译器保证完整。

第 2、3 步完成前不建议移包，否则需要一次性导出约 60 个私有标识符，审查与回滚成本都过高。
