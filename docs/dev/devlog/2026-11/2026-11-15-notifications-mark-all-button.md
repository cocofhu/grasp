# 2026-11-15（通知页「全部已读」按钮尺寸、对齐与禁用态修正）

- 日期：2026-11-15
- 范围：`web/src/views/NotificationsView.vue`、`web/src/views/NotificationsView.pagination.test.ts`
- 做了什么：「全部已读」按钮桌面端改为 32px 高（`h-8`），手机端保留 44px 触控高度（`max-md:min-h-11`）；去掉操作区 `padding-bottom: 8px`，使按钮与筛选标签垂直居中；悬停样式改为 `enabled:hover:`，禁用时不再变色并显示 `not-allowed`；去掉重复的圆角声明，焦点框改为外侧偏移，补上颜色过渡与 reduced-motion；「N 条未读」字号与按钮统一为 12px。
- 为什么：原按钮 44px 高配 12px 字显得臃肿，且被底部内边距顶高、贴近分割线，与左侧标签不对齐；禁用时悬停仍变紫，看起来可点。
- 如何验证：`npx vitest run src/views/NotificationsView`、`npx vue-tsc --noEmit`；Playwright 截图确认桌面按钮 32px 且与标签居中、手机 44px，悬停与禁用态正确。
