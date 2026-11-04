# 2026-11-04（运行产物人工编辑：版本号 + 结构化表单）

- 日期：2026-11-04
- 范围：`server/internal/engine/run_artifact.go`、`server/internal/handlers/run_artifact.go`、`server/internal/engine/gate_artifact.go`、`server/internal/mcp/host.go`、`server/internal/router/router.go`、`web/src/lib/run/{artifactEdit,artifactForms,useArtifactEditor}.ts`、`web/src/components/run/editor/`、`web/src/components/run/{ArtifactPreview,ReactArtifactStage,ArtifactPanel,ClarifyChat}.vue`、`web/src/lib/inbox/useClarifyChat.ts`、`web/src/views/{RunDetailView,RunLiveView,GatesInboxView}.vue`、`web/e2e/artifact-edit*`
- 做了什么：
  - 新增 `PUT /api/runs/:id/artifacts/:name`：运行等待人工且会话空闲，或运行失败时可保存文本产物；保留原写入节点，内容有变化时新增一个修订版本，`If-Match` 不匹配返回 409 并带最新修订号；结构化产物按 schema 校验，错误带字段路径。运行详情返回 `artifactEdit` 状态。
  - 前端按运行共享草稿（`useArtifactEditor`）：9 类结构化产物用表单编辑（表单 / JSON 切换），其它文本用代码编辑器，`page.html` 可在代码和预览间切换。支持冲突提示（载入最新 / 仍然保存）、卡片 ✎ 快速编辑、标签页未保存圆点、Ctrl/⌘+S 保存、Esc 取消、列表删除可撤销、字段错误标红并滚动到位、「已保存为 vN」/「内容未变化」提示。
  - 未保存守卫：切换版本、关闭标签、切换详情标签页、关闭抽屉、离开路由、刷新页面都会确认；澄清对话发送前若有未保存编辑，弹窗提供「保存并发送 / 直接发送 / 取消」。
  - 不可编辑：非文本产物、feedback 台账、历史迭代 `#iter-N` 与历史版本，以及分享、嵌入、Solo 宿主。编辑时停用批注。
- 为什么：人工节点经常只需要小改上游产物（计划条目、需求措辞），此前只能在对话里让 Agent 重写，慢且容易改错其它部分。
- 如何验证：`go vet ./...`、`go test ./...`、`./scripts/cover-check-server.sh 90`；`npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build`；`npx playwright test e2e/artifact-edit.spec.ts`（舞台快速编辑 + Ctrl+S、详情面板保存、暗色与窄屏布局）。
