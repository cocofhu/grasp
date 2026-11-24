# 2026-11-24（等待沙箱名额被取消不再报容量不足；任一仓库 clone 失败都停止启动）

- 日期：2026-11-24
- 范围：`server/internal/runtime/acp_sandbox.go`、`server/internal/runtime/acp_sandbox_slot_test.go`、`server/internal/runtime/acp_recovery_test.go`、`sandbox-gateway/sandbox/scripts/startup.sh`、`sandbox-gateway/scripts/test-git-auth.sh`、`server/README.md`
- 做了什么：`waitRunSandboxSlot` 在 ctx 结束时返回 `context.Cause(ctx)`（取消、超时、节点总时限、用户接管），不再返回上一次的 `ErrSandboxCapacity`；`openNodeSandbox` 在 ctx 已结束时以 `wait for sandbox slot: %w` 返回，不再包成可重试的 `errSandboxSetup` 且保留错误链，其他准入失败不变。`startup.sh` 的 clone 循环去掉按 scheme 区分的分支，HTTPS 与 SSH clone 失败都 `exit 1`，与 `reprime.sh` 一致。
- 为什么：排队等名额时运行被取消或节点超时，节点失败原因记成「沙箱容量不足」，原测试还固定了这个返回值。HTTPS clone 失败后 `continue`，backend 照常启动，平台连上 ACP 即视为就绪，多仓场景下 Agent 在缺仓的工作区里执行，`repos.json` 只是少一项，没有失败信号。
- 如何验证：`TestWaitRunSandboxSlotStopsWithContext` 改为要求 `context.Canceled`；新增超时、节点时限原因、`openNodeSandbox` 取消不可重试且不创建沙箱、其他准入错误仍为 `errSandboxSetup` 的用例，四条在原代码上失败。`test-git-auth.sh` 新增从 `startup.sh` 抽取 clone 循环在 `set -e` 下运行：本地仓库后接不可达 HTTPS 地址必须非零退出、不写 `repos.json`、不留目录；两个可用仓库照常写清单；原脚本上该段失败。server `go test ./...`、`go vet`、gen-configdoc、shellcheck-error 通过。
