# 2026-11-13（沙箱 GitHub CLI 显示真实账号）

- 日期：2026-11-13
- 范围：`server/internal/services/{workspace_credentials,github_device_flow}.go`、`server/cmd/server/main.go`、`server/internal/runtime/{credential_selection,repo_git}.go`、`server/internal/sandbox/{manager,inject_bundle}.go`、`sandbox-gateway/sandbox/scripts/git-credentials.py` 及对应测试
- 做了什么：
  - 手动创建或更换 github.com 的 GitHub Token 时，服务端用设备授权同一套 `/user` 查询（`LookupLogin`，超时 5 秒）记下 `metadata.githubLogin`；查询失败照常保存，不带账号。GitHub Enterprise 等其他主机不查询，避免按用户配置的主机发起服务端请求。
  - `ResolveGitValue` 读出 `githubLogin`，经 `GitCredential.Login` 写入凭据目录 `manifest.json` 的 `login` 字段；沙箱生成 `~/.config/gh/hosts.yml` 时优先用它作为 `user`，缺失或不符合 GitHub 用户名规则时仍写 `grasp-selected`。`Login` 不计入 `SpecFingerprint`，升级不会让已有共享沙箱重新准备。
- 为什么：`gh auth status` 一律显示 `grasp-selected`，用户和 Agent 都误以为沙箱登录了陌生账号。
- 如何验证：`go test ./internal/sandbox ./internal/runtime ./internal/services ./internal/handlers ./cmd/...`，`go test -race ./internal/services -run 'GitHub|Credential'`；`python3 -m unittest test_git_credentials`（新增用户名写入与非法值回退用例）；用真实 `gh` 读取生成的 `hosts.yml`，`gh auth status` 显示真实账号。
