# 2026-11-11（手机聊天输入框异常变高）

- 日期：2026-11-11
- 范围：`web/src/components/chat/ChatComposerTextarea{.vue,.test.ts}`、`CHANGELOG.md`
- 做了什么：输入框用 `ResizeObserver` 监听宽度，宽度变化时重新计算高度；内容为空或元素不可见（宽度为 0）时清掉行内高度，回到 `rows="2"` 的默认高度；最大高度从固定 220px 改为 `min(220px, 30dvh)`。
- 为什么：高度原先只在文字变化和挂载时计算。手机上挂载、键盘弹起、旋转或待发送队列出现时输入框可能很窄，占位文字几乎逐字换行，`scrollHeight` 被量得很大，高度卡在 220px 上限；宽度恢复后不再重算，输入框一直巨大，所以只是「有时」出现。
- 如何验证：新增单测覆盖窄宽度后恢复、宽度不变不重算、清空与隐藏时恢复默认、卸载时断开监听；`src/components/chat`、Solo 会话面板及分享页聊天相关 13 个测试文件通过；ESLint、`vue-tsc --noEmit`、`vite build` 通过，产物 CSS 包含 `max-height:min(220px,30dvh)`。
