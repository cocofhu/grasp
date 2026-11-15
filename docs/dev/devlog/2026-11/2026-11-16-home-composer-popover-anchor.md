# 2026-11-16（首页输入框「+」菜单与 @ 引用面板贴近触发位置）

- 日期：2026-11-16
- 范围：`web/src/views/DashboardView.vue`、`web/src/components/solo/ReferencePicker.vue`、`web/src/lib/shared/textareaCaret.ts`（新增）。
- 做了什么：首页输入框的「+」菜单改为定位在「+」按钮正下方；@ 引用面板改为像 IDE 补全弹窗一样钉在输入的 `@` 旁边：从 `@` 所在行下方弹出、左边对齐 `@`，宽度最多 360px 并收在输入框宽度内；下方放不下而上方放得下时改从该行上方弹出。`@` 坐标由新增的 `textareaCaretRect`（隐藏镜像元素按相同换行规则测量）算出，输入、文本框滚动和输入框尺寸变化时重新计算。两个弹层仍放在输入框表单外（表单 `overflow: hidden`）。@ 面板打开时 `scrollIntoView({ block: 'nearest' })`。引用面板的标签与 Open/全部 切换不再换行，标签栏过窄时横向滚动。
- 为什么：手机端工具栏换成多行后，「+」菜单固定在输入框左下角，离按钮很远；@ 面板出现在整个输入框下方，靠近屏幕底部，会被软键盘挡住，也离光标很远。中文下窄屏标签「全部」「会话」被折成两行。
- 如何验证：`web/` 下 `npx vitest run src/views/DashboardView src/components/solo`（196 个用例通过）、`npx vue-tsc --noEmit`、`npx eslint`；在本地预览页（模拟接口数据）用 1024 宽桌面和 390×844 手机视口检查：「+」菜单在按钮下方，@ 面板在 `@` 所在行下方（`@` 折到第二行时跟到第二行）。
