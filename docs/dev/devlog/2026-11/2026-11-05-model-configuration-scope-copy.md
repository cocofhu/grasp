# 2026-11-05（区分默认模型配置与允许切换的配置范围）

- 日期：2026-11-05
- 范围：`web/src/lib/agent/modelRouting.ts`、`web/src/components/agent/ModelBindingEditor.vue`、`web/src/components/agent/ModelBindingEditor.test.ts`、`web/e2e/model-routing.spec.ts`、`CHANGELOG.md`
- 做了什么：模型设置改用「默认使用的模型配置」「允许切换的配置」「全部已启用配置 / 仅选定配置」，说明全部范围包含系统中不同工具的配置，并自动纳入后续新增配置。同步中英文说明、空状态、添加入口、默认标记和继承摘要；窄屏按钮允许换行，避免较长的英文标签溢出。更新已有组件与浏览器断言，并将已有响应式检查扩展到英文。
- 为什么：默认配置名为 Codex 时，「全部模型组」容易被理解成 Codex 内的全部具体模型；明确配置范围和默认配置的区别，帮助用户判断 Agent 能切换到哪些工具和模型。
- 如何验证：Node 24 下 Web lint（0 errors）、类型检查、6,354 项单元测试（581 文件，Lines 91.84%）、生产构建、相关 Playwright 场景、文档构建与审计，以及仓库 actionlint、shellcheck 均通过。浏览器场景覆盖中英文、明暗主题、桌面与手机宽度，以及跨工具配置和新增配置的可切换范围。完整 Agent 切换矩阵在本机收到 SIGTERM 中断，45 项 Node 合约测试与 9 项 Python 测试通过，完整矩阵继续由 PR CI 验证。
