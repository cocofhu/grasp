# 2026-11-15（Solo：拆分单轮执行，并按职责拆分 solo.go）

- 日期：2026-11-15
- 范围：`server/internal/services/solo.go`，新增 `solo_turn.go`、`solo_model_settings.go`、`solo_queue.go`、`solo_sandbox.go`、`solo_turn_test.go`。
- 做了什么：#1206 第 2 项。
  - `soloChat.execute` 原有 158 行，现在主流程只写出几个步骤：`saveUserMessage`（固定引用并保存用户消息）、`prepareTurn`（等待沙箱、重读会话、模型对齐、发布执行快照，返回 `soloTurnPrep`）、`buildTurnInput`（批注、引用块、重启/恢复/换模型/推理强度前言，返回 `soloTurnInput`）、`runAgent`（收发问答、调用 Agent、取出流式回复、判定 ok/stopped/failed，返回 `soloTurnOutput`）、`saveTurnResult`（合并已交付文件后 upsert 回复、更新用户消息状态、清除前言标志与过期的模型提示、记录用量）。准备阶段失败仍统一走 `recordPreparationFailure`。
  - `solo.go`（2042 行）按职责拆成同包文件，每个声明逐字搬移（用 AST 核对过名称和源码一致）：单轮执行 → `solo_turn.go`；模型与推理强度设置及切换对齐 → `solo_model_settings.go`；会话 FIFO（发送、入队、持久队列恢复、取消、排序、订阅，以及 chat 注册）→ `solo_queue.go`；沙箱生命周期回调 → `solo_sandbox.go`。`solo.go` 留下类型定义、构造、会话生命周期和只读视图。
  - 结构体注释写明锁的拥有者：`SoloService.mu` 保护 `chats` / `github` / `pausedSandboxes`；`soloChat.mu` 保护当前轮次的实时状态（轮次开始时重置，由 `execute` / `onAcp` 填充，轮次结束的 publish 清空，`Subscribe` 和预览读取方只读）。
- 为什么：单轮执行和会话、队列、模型、沙箱的代码混在一个文件里，状态归谁管不容易看出来。执行仍复用 `chatsession` 的 FIFO 和单 worker，没有新增队列。
- 保持不变：持久化顺序和条件逐条一致（包括交付文件不被覆盖的 upsert 列、`ok` 时才清除前言标志和旧模型提示、停止和失败两种分类、准备失败时的 stopped 返回值）。HTTP/WS 字段、错误文案、状态转换都没动。纯结构调整，不改 CHANGELOG。`SandboxService` 已按职责分布在 `sandbox_*.go` 多个文件中，本次不动。
- 如何验证：重构前先在原代码上新增并通过（`-count=3`）两个此前没有覆盖的表征测试：`TestSoloTurnReplyKeepsDeliveredFiles`（Agent 运行期间交付的文件进入回复）、`TestSoloTurnRestartPreambleThenCleared`（重建沙箱后的重启前言、成功后清除 `needs_preamble`、下一轮按原文发送）；重构后不变通过。`go test -race ./internal/services -run Solo -count=2` 通过。`server/` 下 golangci-lint、`go vet ./...`、gen-configdoc 检查、`go test ./...`、`cover-check-server.sh 90` 通过。
