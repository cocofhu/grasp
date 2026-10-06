# acp-bridge 架构说明

## 依赖方向（自上而下）

```
浏览器 (WebSocket + HTTP)
    ↓
internal/handler   … 解析请求、调用 Bridge，不含业务规则
    ↓
internal/service   … 队列、广播、权限、时间线（应用层）
    ↓
internal/agents    … AGENT_PROVIDER 选型（唯一入口）
    ↓
internal/provider  … Session / Provider 抽象
    ├─ acpx + acp + backend/*   … 长驻 ACP（JSON-RPC over stdio）
    └─ oneshot + codecs         … 每轮起进程 + resume
```

- **`internal/router`**：注册路由、静态资源、CORS、优雅退出；**不**写业务逻辑。
- **`internal/correl`**：短 ID（cid / oid），仅用于日志关联。
- **`internal/config`**：进程级配置结构体。
- **`internal/backend`**：仅提供长驻 ACP 的 argv/AuthEnv 实现，供 `acpx.FromBackend` 复用；**不是**第二套选型入口（选型只在 `agents`）。

## `service` 包内文件职责

| 文件                     | 职责                                                                 |
|------------------------|--------------------------------------------------------------------|
| `bridge.go`            | `Bridge` 结构体、队列快照、WS 客户端集、Prompt 条目类型                              |
| `bridge_ws.go`         | 注册/注销客户端、线程安全写 WebSocket、广播 JSON                                   |
| `bridge_chat.go`       | Prompt 队列、`pumpPromptQueue`、`executePrompt`、`ChatWithOpID`、取消      |
| `bridge_connect.go`    | Agent 握手、`Connect`/`EnsureAgent`/`RestartAgent`、`ConnectedPayload` |
| `bridge_permission.go` | `session/request_permission` 与浏览器回包                                |
| `bridge_timeline.go`   | `userDoneBuf`、`UserTimelineForClient`（刷新对齐用户句）                     |

## 前端 `web/static/js` 分层

```
app/main.js          … 入口：DOM 绑定、Session、队列面板
ws/session.js        … WebSocket 生命周期、connected / queue_state
ui/chat_view.js      … 聊天 DOM、工具卡、持久化
ui/chat_payload.js   … session/update 纯函数归一化
ui/queue_panel.js    … 底部队列 UI
core/acp_protocol.js … kind 归一化、工具类 kind 判断
core/md.js、paths.js
conversation/      … session/update 路由与扩展点（见下）
```

### 扩展 session/update 子类型

1. 改 `conversation/builtin_session_handlers.js`，或
2. `import { registerSessionUpdateHandler } from '/assets/js/conversation/index.js'`（路径随静态挂载而定）。

**`conversation/index.js`** 为对外唯一聚合导出（`CardType`、`dispatchSessionUpdate`、`registerSessionUpdateHandler`
）；内部实现勿从子文件深路径引用以免循环依赖。

## 桥接心智模型（摘要）

- **事件与快照**：`Bridge.Broadcast` 下发 `op:event`、`connected`、`queue_state` 等，驱动前端与会话状态同步。
- **队列与串行**：`ChatWithOpID` 配合 `enqueueMu`、`pumpPromptQueue` 实现每会话 FIFO 与单 worker 消费。
- **持久化**：助手侧卡片在浏览器 **IndexedDB**（`acp-bridge-chat`，按 `sessionId` 单键）；用户侧已发送句由 **`userTimeline`** 补齐，刷新后可重建交替流；IndexedDB 失败时依赖 eventLog 降级。

## 日志约定（现状）

| 前缀 / 形态                            | 含义                               |
|------------------------------------|----------------------------------|
| `ws cid=`                          | WebSocket 连接级（`correl` 生成的 cid）  |
| `ws … oid=`                        | 单次 chat 入队 id（与队列面板、prompt 日志一致） |
| `prompt sid= … oid=`               | 某轮 `session/prompt` 执行           |
| `acp sid=` / `acp %s`（conn logTag） | 子进程 JSON-RPC 与 Panel             |
| `oneshot:`                         | 每轮起进程 transport 的启动 / I/O / resume |
| `agents:`                          | 未知 `AGENT_PROVIDER` / 回退选型         |
| `bridge sid=`                      | 广播序列化/写客户端失败                     |
| `perm sid=`                        | 权限等待与浏览器回包                       |

排查链路：**同一用户消息**应对齐 `ws … oid=` → `prompt … oid=`。

## 复用与重复

- **已复用**：`queueSnapshot` / `PromptQueueSnapshot` / `ConnectedPayload` 单一路径；`mergeSessionUpdateEnvelope` +
  `flattenToolPayload`；`conversation/index.js` 聚合导出。
- **仍偏大**：`ui/chat_view.js` 单文件承担 DOM + 持久化 + 工具合并（可再拆 `tool_merge.js` 仅当体积成问题时）。
- **未抽象成接口**：`Bridge` 为具体结构体，便于小项目；若要多实现可再抽 `PanelHost` 接口。

## 尚未「做到最好」（可选演进）

- 结构化日志（`slog` + level）、请求级 trace id 贯通前端 `cid`。
- 集成测试（假 Panel / 假 WS）覆盖 `handleChat`、队列满、取消。
- 前端 E2E（Playwright）关键路径。
- 附件已统一为 `/tmp` 落盘 + 路径引用（图片与普通文件）；原生 image block 路径可按需清理。
