# 2026-11-20（「历史执行」标题改为折叠开关）

- 日期：2026-11-20
- 范围：`web/src/components/shell/SidebarWorkspaceLists.vue`、`web/src/data/settingsNav.ts`、`web/src/locales/{zh-CN,en}/nav.json`、侧栏和设置目录的单测与 e2e、`CHANGELOG.md`
- 做了什么：「历史执行」标题原来是指向 `/runs` 的链接加一个独立的箭头按钮，现在合成一个按钮（图标、文字、箭头），点击只切换折叠，不跳转也不触发 `navigate`，结构对齐「正在执行」的 `sidebar-section-label`。设置目录「工作区工具」在「看板」后新增「运行记录」（`/runs`），`useSidebarNavigation` 已按设置条目把它当作共享工具，从设置进入时保留设置侧栏。
- 为什么：标题文字和箭头的点击行为不一致，用户点文字想折叠却跳到了运行记录页。运行记录入口移到设置目录，不再藏在标题里。
- 如何验证：`npx vitest run src/components/shell src/data src/lib/composables src/locales --maxWorkers=2` 通过（新增点标题文字和图标折叠、路由不变、不 emit `navigate` 的用例，以及 `/runs`、`/runs/:id` 选中设置条目的断言）；`npx vue-tsc --noEmit` 通过；`workspace.spec.ts`、`notifications-center.spec.ts`、`workflow-favorites.spec.ts` 桌面 e2e 30 条通过，其中设置工具循环加入了 `/runs`。
