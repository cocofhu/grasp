# 2026-11-02（#1088 复查：沙箱浏览器打开请求不重放，open 仅限 http(s)）

- 日期：2026-11-02。
- 范围：`web/src/components/run/NovncPreviewPanel.vue`、`web/src/components/run/ReactArtifactStage.vue`、`server/internal/handlers/preview_vnc.go`。
- 做了什么：`ReactArtifactStage` 一直保留最近一次 `browserOpenRequest`，浏览器面板重新挂载（回到首页、换沙箱）时 `immediate` watch 会把旧网址再开一次。现在面板发出 `open` 后 emit `open-sent`，stage 清空请求；seq 改为独立计数。服务端 `open` 只接受 http(s) 网址。
- 为什么：#1088 合并后复查发现。
- 如何验证：`go vet`、`go test ./internal/handlers -run Vnc`；`web` 下 `vue-tsc --noEmit`、eslint、`NovncPreviewPanel` 与 `ReactArtifactStage` 用例通过。
