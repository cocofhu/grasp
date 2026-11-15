# 2026-11-15（恢复/重连的工作流沙箱丢失模型，统计记为未知模型）

- 日期：2026-11-15
- 范围：`server/internal/runtime/sandbox_route_env.go`、`server/internal/runtime/acp_recovery.go`、`server/internal/runtime/acp_sandbox.go`
- 做了什么：工作流节点沙箱从空闲回收中恢复（`restoreReactEnvironment`）或平台重启后重连（`attachSandbox`）时，改为先 source 运行时环境文件 `/run/grasp-runtime/env`，再读取 `ACP_BRIDGE_MODEL`、`GRASP_MODEL_CONFIGURATION_ID`、`ACP_BRIDGE_REASONING_EFFORT`。三项只用一次 exec 读取。原来的做法是分别执行 `printenv`。
- 为什么：E2B 的 exec 会话不继承 bridge 进程的环境，`printenv` 读到的是空值；Docker 下读到的是容器创建时的环境，re-prime 之后已经过期。模型为空时，`execution.modelId` 为空，回复信息里的「模型」显示 Not reported；Cursor 等工具在 `prompt_done` 里上报的 weak key（`default`）也没法回填到 bridge model，于是 Token 统计里大部分用量记成了「未知模型」。统一空闲回收之后，等待人工回复的节点经常走恢复路径，这个问题出现得也更频繁。`services.sh`、`reprime.sh` 本来就以这个环境文件为准。
- 如何验证：`go test ./internal/runtime/`。新增的 `sandbox_route_env_test.go` 覆盖三种情况：环境文件优先于会话环境（包括被 re-prime 清掉的值）、没有环境文件时回退到会话环境、输出解析。另外在 E2B Cursor 沙箱里用 `env -i bash` 对照：`printenv` 为空，新脚本读到 `claude-opus-5-5-medium`。
