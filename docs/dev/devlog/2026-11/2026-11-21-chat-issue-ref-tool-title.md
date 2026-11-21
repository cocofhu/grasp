# 2026-11-21（行首 `#1304` 被渲染成标题、Codex 工具命令被逐词大写）

- 日期：2026-11-21
- 范围：`web/src/lib/shared/markdown.ts`、`web/src/lib/solo/acpTurn.ts`、`web/src/components/agent/AgentChatTester.vue`、`web/src/lib/shared/markdown.test.ts`、`web/src/lib/solo/solo.test.ts`、`CHANGELOG.md`
- 做了什么：聊天 Markdown 的「`#` 后可不带空格」标题规则不再匹配 `#` 后紧跟数字的情况，`#1304` 这类 issue / PR 编号按正文显示。工具名只有由字母、数字、下划线、连字符和空格组成时才逐词首字母大写，含 `/`、引号、`.` 等的命令或路径原样显示。
- 为什么：`tightHeading` 扩展为支持 `##上游交接` 而放宽了 ATX 标题，行首的 `#1307`、`#1304 上次的 web-e2e 被取消…` 被渲染成 h1，整段变成粗体大字且吞掉 `#`。Codex 把原始命令（`/bin/bash -lc 'gh pr view 1307 …'`）作为工具 title 发来，`humanizeTool` 对所有未知名称做 `\b\w` 大写，显示成 `/Bin/Bash -Lc 'Gh Pr View …'`。流式渲染（`renderChatMarkdownBlocks`）与最终渲染共用 `chatMarked`，一并修复。
- 如何验证：新增单测：行首 `#1304` / `#1307` 不产生标题；命令和路径 title 原样保留，`web-search` 仍为 `Web-Search`。原有 `##上游交接`、`正文\n#标题`、`@#815` 用例保持通过。前端 lint、类型检查、单测和构建通过。
