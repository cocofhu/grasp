# 2026-11-15（沙箱运行时：拆分 oneshot.runOnce 的进程管理与输出处理）

- 日期：2026-11-15
- 范围：`sandbox-gateway/sandbox/internal/provider/oneshot/oneshot.go`、`sandbox-gateway/sandbox/internal/provider/oneshot/turn.go`（新增）。
- 做了什么：#1206 第 5 项。`runOnce` 从约 320 行缩为按步骤调用的主流程：`turnArgs`（argv 与 LogFile 临时日志）、`startTurnProcess`（独占输出管道、可选 stdin、启动后关闭父进程写端）、`killGroupOnCancel`（取消时杀进程组，CLI 退出后立即解除）、`feedStdin`（后台写 prompt，返回等待函数）、`waitTurnProcess`（`cmd.Wait` 与 `superviseTurn`）、`joinOutputs`（进程退出后按单次读超时排空并识别被后代占用的管道）、`applyLogFile`。输出侧由新的 `turnAccumulator` 拥有：`apply` 处理事件、会话 id、用量与停止原因；`settleUsage` 做原生会话累计用量差分与平铺会话对账并并入累计用量；`outcome` 判定停止原因与错误包装。`consumeStdout` 保留 `Codec` 边界（WholeOutput / Stateful / 逐行）。
- 为什么：进程、管道、事件、用量和结果判定原本交织在一个函数里，修改任一部分都要通读全部并发细节。现在每段状态只有一个拥有者：进程与管道归 `turnProcess`，输出产生的状态归 `turnAccumulator`（仅由 stdout 消费 goroutine 写入，join 之后才由回合 goroutine 读取或再次写入）。
- 保持不变：回合以进程退出而非管道 EOF 结束（#619）；后台 shell 监督与结果后宽限杀进程（#765、#767）；被常驻服务持有的管道放弃读取但继续排空到 `io.Discard`，不会 SIGPIPE；慢消费者只按阻塞读计时；取消优先于 SIGKILL 退出码；stdin 写错误的两种包装；resume 失败回退与诊断只发布最后一次尝试（`Prompt` 未改）；原生会话累计用量差分与零值终态不覆盖已观测用量（`5b2701fd`、`62915df2`）。各步骤调用顺序与原实现一致，纯结构调整，不改 CHANGELOG。属于运行时包逻辑，随 server 发布，不需要新镜像。
- 如何验证：`go test ./internal/provider/oneshot -count=1` 与 `-race -count=3` 通过（覆盖进程退出与 EOF 区分、后台进程保留、取消、慢消费者尾部输出、用量差分等既有用例）。`sandbox/` 下 golangci-lint、`go vet ./...`、`go test ./...`，`sandbox-gateway/` 下 `cover-check-sandbox.sh 90`（96.6%）通过。本机是 Grasp 沙箱，会注入 `CONFIG_ROOT` / `AGENT_PROVIDER` 等变量，使 `internal/agents`、`internal/handler` 中 4 个用例在未改动的 main 上同样失败；在 `env -i` 的干净环境中全部通过。
