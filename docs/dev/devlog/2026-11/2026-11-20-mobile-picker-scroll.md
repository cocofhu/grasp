# 2026-11-20（移动端模型选择器可以滑动）

- 日期：2026-11-20
- 范围：`web/src/components/solo/SoloPickerShell.vue`、`web/src/components/solo/SoloModelSelect.test.ts`、`CHANGELOG.md`
- 做了什么：触屏设备打开 Solo 选择器时不再自动聚焦搜索框。面板按 `visualViewport` 的可见高度设置最大高度，键盘弹起或收起时重新定位。列表加上 `overscroll-behavior: contain` 和 `touch-action: pan-y`。
- 为什么：移动端打开模型选择器后列表滑不动，看不到下方模型。自动聚焦会弹出软键盘挡住列表，iOS 上滑动会带动整页；`100vh` 比手机可见区域高，面板可能超出屏幕。
- 如何验证：新增单测覆盖触屏下不聚焦搜索框、面板最大高度和列表滚动样式。前端全量单测、lint 和类型检查通过。Playwright 按 iPhone 13 参数发送触摸滑动，向下和向上弹出的列表都能滚动且菜单保持打开。未在 iOS Safari 真机验证。
