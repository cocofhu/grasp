# 2026-11-18（调整 Fast 闪电的开关样式）

- 日期：2026-11-18
- 范围：`web/src/components/solo/SoloModelSelect.vue`、`web/e2e/model-routing.spec.ts`
- 做了什么：为 Fast 开关绘制圆角闪电，关闭时显示灰色轮廓，开启时使用实心填充。按钮在两态和悬停时均使用透明底色。
- 为什么：用户希望通过闪电本身区分 Fast 的开关状态。
- 如何验证：在浏览器中切换 Fast，检查闪电的描边与填充、原有按钮尺寸和服务档位的保存。
