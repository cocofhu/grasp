# 2026-11-05（GitHub 凭据默认设备码）

- 日期：2026-11-05（写下这条的当天，不是排期）
- 范围：`server/internal/config/{config,descriptor,config_test}.go`、`server/internal/services/github_device_flow_test.go`、`server/{CONFIGURATION.md,config.example.yaml}`、`docker-compose.yml`、`.env.example`、`docs/content/{,en/}help/configuration.md`、`web/src/components/workspace/{GitHubDeviceLoginCard,CodexDeviceLoginCard,WorkspaceCredentialsPanel}.vue` 及对应测试、`web/src/locales/{zh-CN,en}/pages.json`、`CHANGELOG.md`
- 做了什么：GitHub Client ID 按环境变量、YAML、内置常量的顺序解析，部署填写的值优先。内置常量目前仍是空字符串：本环境拿不到 Grasp 自有、已开启 Device Flow 的 OAuth App，不能写占位值，也不能复用其他产品的 Client ID，所以空配置仍关闭设备码。GitHub 设备码卡片对齐 Codex 的步骤、复制、打开页面、倒计时和失败后改粘贴，并去掉申请权限。两张卡片的复制改走 `copyToClipboard`。
- 为什么：内网 HTTP 上直接调用剪贴板接口会失败；卡片需要和 Codex 使用同一套步骤与动效。默认开启要等填入真实 Client ID。
- 如何验证：在 `server` 下 `go test ./internal/config/ ./internal/services/ ./internal/handlers/ -count=1 -timeout 180s -run 'GitHub'`；在 `web` 下 `NODE_OPTIONS=--max-old-space-size=3072 npx vitest run --maxWorkers=2 src/components/workspace/GitHubDeviceLoginCard.test.ts src/components/workspace/CodexDeviceLoginCard.test.ts src/components/workspace/WorkspaceCredentialsPanel.githubDevice.test.ts src/components/workspace/WorkspaceCredentialsPanel.codexDevice.test.ts src/components/workspace/deviceLoginMotion.test.ts src/lib/shared/copyToClipboard.test.ts`。
