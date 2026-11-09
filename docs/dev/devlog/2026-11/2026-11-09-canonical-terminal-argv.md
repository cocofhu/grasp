# 2026-11-09 当前工作区终端使用可验证的目录参数

- 日期：2026-11-09
- 范围：`server/internal/services/sandbox_view.go`、`server/internal/services/sandbox_terminal_cwd_test.go`。
- 做了什么：远程终端以 `env -C <当前工作区> bash -i` 启动，沿用 SSH 的逐参数校验，不再将带引号和变量的 shell 脚本当普通参数传入。
- 为什么：真实 Kubernetes 验收中，终端 WebSocket 已建立，但原 `bash -c` 脚本被既有安全参数校验拒绝。该错误在只检查参数列表或 stub 网关的测试中没有出现。
- 如何验证：回归通过真实 SSH PTY 请求和本机真实 PTY 执行生产命令，从另一个初始目录启动，检查实际 `$PWD` 与当前目录的独有文件；同一 SSH 边界仍拒绝含分隔符的非法目录，未放宽验证器。此回归不替代部署后的 Kubernetes 终端复验。
