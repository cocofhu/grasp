# Grasp 品牌规范

Grasp 只有一套标识：**幽灵猫 + Fredoka 墨色「Grasp」字标**。本文件给前端、文档与验收同学使用。

## 1. Logo 资产（本目录）

| 文件 | 用途 |
|------|------|
| `logo-mark.svg` | 纯图标 128×128，只有幽灵猫，透明底，深浅底通用 |
| `logo-light.svg` | 浅色底完整 lockup 420×128（猫 96px + 字标 `#18181b`），README 头图 |
| `logo-dark.svg` | 深色底完整 lockup 420×128（猫 96px + 字标 `#ededf0`） |
| `grasp-logo-banner.svg` | 横幅源文件，内容同 `logo-light.svg` |
| `grasp-logo-banner.png` | 横幅位图 1200×400，透明底，由 `grasp-logo-banner.svg` 导出 |
| `favicon.svg` | 静态猫矢量源，`web/public/favicon.ico` 由它导出 |

`yume-mark.png`、`grasp-*.mp4`、`grasp-readme-video-poster.webp` 与 `screenshots/` 是首页插画和 README 演示素材，不属于 Logo。

`web/src/assets/brand/` 中的 `logo-mark.svg`、`logo-light.svg`、`logo-dark.svg` 与本目录同名文件内容保持一致；三份 `favicon.svg`（本目录、`web/src/assets/brand`、`web/public`）保持同步。

## 2. 使用规范

| 项 | 规则 |
|----|------|
| 图标 | 只用幽灵猫，几何与配色不可修改；白身 + `#1a1a1a` 描边 + `#c8c8c8` 腮红，深浅底都不反色。猫的路径以 `web/src/components/shell/BrandCharacterMark.vue` 为唯一数据源 |
| 字标 | Grasp，Fredoka SemiBold（600），字距 −0.01em；浅底 `#18181b`，深底 `#ededf0`；不用渐变 / 流光 / 描边 |
| 组合比例 | 猫高 ≈ 字号 × 1.45；间距 ≈ 猫高 × 0.25（28→8px、48→12px、96→22px） |
| 最小尺寸 | 猫 ≥ 16px；完整 lockup ≥ 120px 宽 |
| 品牌紫 | `#7B61FF` 只用于产品交互强调（按钮、焦点），不进入 Logo |
| 字体文件 | 应用内自托管 `web/public/fonts/fredoka-latin-600-normal.woff2`（OFL 1.1，见同目录 `NOTICE.md`）；静态 SVG 中字标转为路径，不引用外部字体 |

- **浅色文档 / README**：`logo-light.svg`
- **深色背景**：`logo-dark.svg`，或单独使用 `logo-mark.svg`
- **不要**把 `#ededf0` 字标放在浅底上，**不要**把 `#18181b` 字标放在近黑底上
- 保持比例，勿压扁；透明底导出，勿自带背景色块

## 3. 应用内落地

| 位置 | 猫 | 间距 | 字标 |
|------|----|------|------|
| 侧边栏品牌行（64px 高） | 28px | 8px | `BrandLogo size="md"`，20px |
| 登录页 | 48px | 12px | `BrandLogo size="lg"`，30px；副标题仍用 Inter |
| 移动端导航抽屉 | — | — | `BrandLogo size="sm"`，18px，带副标题 |

- 字标样式统一由 `web/src/styles/global.css` 的 `.brand-logo__name` 控制：`font-family: 'Fredoka', ui-rounded, system-ui, sans-serif`，颜色 `rgb(var(--c-txt))`，组件内不要再覆盖。
- 自定义产品名（品牌设置）同样以 Fredoka 显示，中文等非拉丁字符回退到系统字体；过长时省略号截断。
- 猫的眨眼、跳跃、翻转与待机动效保持不变；`prefers-reduced-motion` 下猫静止，字标本身没有动画。

## 4. 更新资产时的检查清单

- [ ] 猫的 `path d` 与 `BrandCharacterMark.vue` 完全一致
- [ ] 字标为 `<path>`，SVG 中没有 `<text>`、外链字体或外部图片
- [ ] `media/` 与 `web/src/assets/brand/` 同名文件一致，横幅 PNG 已重新导出
- [ ] 浅色与深色背景下均检查过侧边栏、登录页与 README 头图
