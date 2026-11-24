# 2026-11-24（Markdown 里的 Mermaid 代码块绘制成图）

- 日期：2026-11-24
- 范围：`web/src/lib/shared/markdown.ts`、`web/src/lib/shared/markdownMermaid.ts`、`web/src/components/run/mermaidTheme.ts`、`web/src/styles/global.css`、`web/e2e/markdown-mermaid*`
- 做了什么：`marked` 的 `code` 渲染器把已闭合的 ` ```mermaid ` 围栏输出为 `pre.md-mermaid`（源码在内作为回退），`renderMarkdown` 与 `renderChatMarkdown` 两套实例都接上。`markdownMermaid.ts` 在首次输出图块时挂一个文档级 `MutationObserver`，发现图块后经 `withMermaidSerial` 串行绘制 SVG，按主题+源码缓存，仅在明暗切换时按新主题重绘，切换前发起的渲染完成后不再上屏；同一图的每份副本改写 Mermaid 内部 id，避免 marker/样式串到另一份；解析失败保留源码，渲染失败重试一次后对同一主题+源码不再重试。宽图最小缩放 0.75，超出在卡片内横向滚动。`themeVars` 的连线改用 `--c-txt3`，节点与分组边框改用 `--c-line-strong`，方案页的图同样受益。
- 为什么：对话回复、产物预览和节点输出只有 Plan/RootCause 的结构化 `diagram` 字段会画图，正文里的 Mermaid 围栏一直显示源码；Agent 回复里常见这种写法。浅色主题下原连线色（边框 token）几乎看不见，宽流程图缩放后文字约 8px。
- 如何验证：`markdown.test.ts` 与新增 `markdownMermaid.test.ts` 覆盖闭合判断、转义、主题切换、解析失败、重试、缓存复用与已移除节点；新增 `e2e/markdown-mermaid.spec.ts` 在 Chromium 用真实 Mermaid 与 DOMPurify 检查深色/浅色配色、运行中切换主题、流式闭合、缩放下限与连线对比度，并加入 `test:e2e:ci`。web 全量 vitest、`vue-tsc`、eslint 通过。
