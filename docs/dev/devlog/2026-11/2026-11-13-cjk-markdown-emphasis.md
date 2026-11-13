# 2026-11-13（Markdown：中文标点贴着 `**` 时粗体/斜体不生效）

- 日期：2026-11-13
- 范围：`web/src/lib/shared/markdown.ts`、`web/src/lib/shared/markdown.test.ts`、`CHANGELOG.md`。
- 做了什么：`marked` 共享实例和 `chatMarked` 都换上 `cjkEmStrong` 作为 `emStrong` 分词器。先调用原生 `Tokenizer.prototype.emStrong`，命中就直接返回；失败时在 `maskedSrc` 里按放宽后的左右侧定界规则找同长度的闭合符：紧贴的标点本身是中日韩/全角标点，或标点另一侧是中日韩字符时，视为可开/可闭。找闭合符时同长度的 `*` 串按 CommonMark 方式嵌套（中间的开启符先吃掉下一个闭合符）。只有开启符或闭合符确实需要放宽规则时才接管；两者都符合 CommonMark 却被 marked 拒绝，说明另有原因（如前面有未闭合的 `**`），保持 marked 的结果。`renderMarkdown`、`renderMarkdownBlocks`、`renderChatMarkdown`、`renderChatMarkdownBlocks` 都生效。
- 为什么：CommonMark 规定紧挨标点的定界符另一侧必须是空白或标点，中文不加空格，所以 `**结论：**正文`、`**注意（重要）**这里`、`是**“关键”**步骤` 会原样显示 `**`。Agent 回复里 `**小标题：**正文` 很常见。思路与 markdown-cjk-friendly 一致；marked 没有现成插件，提示词约束不可靠，所以在渲染端修。项目自身的模板和提示词（169 行含粗体的中文）用旧规则只有 1 行失败（截断的 Go 测试字符串），不受影响。只按“附近有中文”判断会覆盖 marked 的正确结果，`注意 **这里 还有 **重点**` 会变成整段加粗、`**重点` 原样显示，所以要求放宽规则确实起作用。弯引号、`…`、`—` 英文也用，只算普通标点、不算中日韩字符，否则 `He said **“hi”**there` 会被加粗。marked 的 `prevChar` 只有一个 UTF-16 单元，扩展 B 区汉字要从 `maskedSrc` 取完整字符；marked 在 `**` 失败后会从第二个 `*` 重试，此时 `prevChar` 为 `*`，不能当新的定界符。
- 如何验证：新增 `CJK emphasis boundary` 用例，覆盖四个渲染入口：上述写法和扩展 B 区汉字、内嵌斜体渲染为 `<strong>`/`<em>`；连续多段 `**小标题：**正文` 各自配对，`**注意 **结论：**正文` 与最近的开启符配对；`**结论**：`、英文 `a **b.**c`、`He said **“hi”**there`、`2 * 3`、未闭合的 `**`、转义的 `\**`、行内代码和代码块保持原样；前面有未闭合 `**` 时后面的粗体与 marked 一致。新增的回归用例在旧实现上失败。`web/` 下 `npm run lint`、`npx vue-tsc --noEmit`、`npm test -- --coverage`、`npm run build` 通过。
- 未做：下划线 `_` / `__` 不放宽（中文回复基本不用，且下划线在词内有额外限制）；`***` 三连符和长度不同的 `*` 串混合配对不处理。
