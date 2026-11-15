# 2026-11-15（「Grasp」字标换成 Fredoka 圆体，全部 Logo 统一为幽灵猫）

- 日期：2026-11-15
- 范围：`web/public/fonts`、`web/src/styles/fonts.css`、`web/src/styles/global.css`、`web/index.html`、`web/src/components/shell/AppSidebar.vue`、`media/`、`web/src/assets/brand/`
- 做了什么：自托管 Fredoka 600 Latin 子集（来自 `@fontsource/fredoka` 5.3.0，OFL 1.1）；`.brand-logo__name` 改为 Fredoka 600 墨色（`--c-txt`），字距 −0.01em，字号 sm 18 / md 20 / lg 30px，去掉 `--grad-logo` 渐变与 `shimmer` 流光，删除侧边栏的冗余覆盖；启动壳同步为墨色 Fredoka 并 preload 字体。`media/logo-mark|light|dark.svg`、`grasp-logo-banner.svg/png` 与 `web/src/assets/brand` 副本从紫色菱形换成幽灵猫 + Fredoka 字标路径（opentype.js，字号 300，整数坐标）。重写 `media/BRAND.md`。`.brand-logo` 补 `min-width: 0`，侧边栏长自定义产品名此前会压到隐藏按钮上，现在按省略号截断。猫本身未改。
- 为什么：仓库里有两套标识（应用内幽灵猫、README 紫色菱形）和两种字标（侧边栏墨色 Inter、登录页紫色渐变流光）；圆体字标与猫的圆润轮廓更协调。
- 如何验证：`BrandLogo.test.ts`、`AppSidebar.test.ts` 与新增 `brandAssets.test.ts`（猫路径一致、无旧配色与 `<text>`、两处副本一致、字体已声明）；web 类型检查、lint、vitest、`node scripts/assert-no-approving-brand.mjs`；浏览器中检查浅色 / 深色主题下侧边栏、登录页与长产品名截断。
