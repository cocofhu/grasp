# 2026-11-21（移动端对话定位条按住切换）

- 日期：2026-11-21
- 范围：`web/src/components/chat/ChatMessageRail.vue`、`web/src/components/chat/ChatMessageRail.test.ts`、`CHANGELOG.md`
- 做了什么：触屏上按住对话左侧定位条约 350ms（或按下后直接拖动超过 8px），会打开提问列表卡片。手指上下滑动时实时高亮对应提问并显示回复预览，滑到卡片上也能选中对应行；松手后跳转到高亮的提问，`pointercancel` 则只关闭卡片。快速轻点仍然直接跳转。定位条设置 `touch-action: none`，并禁用长按选中文字和系统菜单。
- 为什么：触屏没有悬停，原来点一下就跳转，无法先浏览提问列表再选择。刻度只有 10px 高，很难点准想要的那条。
- 如何验证：新增单测覆盖按住打开、滑动切换、松手跳转且不重复跳转、轻点直接跳转、`pointercancel` 只关闭卡片。前端 lint、类型检查和相关目录单测通过。用 Playwright 模拟 Pixel 7 发送 CDP 触摸事件，确认按住、滑动、松手、轻点和滑到卡片内选择都符合预期。未在 iOS Safari 真机验证。
