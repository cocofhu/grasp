# 2026-11-10 工作流入口并入 @ 选择器

- 日期：2026-11-10
- 范围：`web/src/components/chat/`、`web/src/components/solo/`、`web/src/lib/chat/`、`web/src/lib/solo/useReferenceComposer.ts`、`web/src/lib/run/{homeComposerDraft*,draftIdb.ts}`、`web/src/views/DashboardView*`、`web/LIB_DOMAIN_MAP.json`、`web/e2e/{solo-workflow*,dashboard-home-chat.spec.ts}`、`README.md`、`CHANGELOG.md`
- 做了什么：移除独立的斜杠菜单，将“选择工作流”作为可选动作并入首页和 Solo 共用的 @ 选择器；支持 `@workflow` 和中文名称筛选，沿用方向键选择、Enter 确认、Tab 切换分类。选中只移除当前 @ 查询，保留其余草稿、引用和附件，再打开配置；禁用工作流及嵌入对话不显示该入口。手动输入 `/workflow` 的既有处理仍兼容。
- 为什么：工作流选择统一收在 @ 入口，避免为同一个输入框增加另一套选择菜单，并继续由用户确认启动。
- Review 修正：首页草稿同时保存引用及其原始文本区间，返回首页或刷新后恢复引用关联，并保留 IndexedDB 失败时的 localStorage 回退；恢复不依赖仓库列表加载，也不抢焦点。字段可选，无需修改数据库版本，旧草稿继续可读。
- 如何验证：Web Lint、vue-tsc、构建及品牌断言通过；全量 6,446 个单元测试通过，Lines 覆盖率 91.63%；完整 Playwright CI 套件 393 个用例通过，包含 Chromium/WebKit 移动端检查。Review 的新增单测先复现引用丢失，修正后单测与浏览器均验证“选引用 → 打开工作流 → 返回/刷新首页 → 继续发送”保留原引用且不会提前发送模型提示；覆盖 IndexedDB 和 localStorage 回退。Solo 390px 视口内的菜单和手动启动路径正常。Docs 构建、npm audit，以及仓库四项常驻检查均通过。
