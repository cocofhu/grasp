# Grasp × cocofhu 品牌简报

给前端动效 / 验收同学：logo 资产、配色、首页英雄动效方向。

## 1. Logo 资产（本目录）

| 文件 | 用途 |
|------|------|
| `logo-mark.svg` | 纯图标 128×128，深浅底通用 |
| `logo-dark.svg` | 深色底完整 lockup（mark + Grasp 字标，奶油色字） |
| `logo-light.svg` | 浅色底完整 lockup（mark + Grasp 字标，墨色字） |
| `favicon.svg` | 当前 Grasp 角色的静态矢量源，透明背景，与侧栏角色保持一致 |
| `preview-*.png` | 评审用预览图 |

字标已转成 **Fredoka Bold 路径**，不依赖外链字体。

### 使用规则

- **深色 UI / 首页英雄**：`logo-dark.svg` 或单独 `logo-mark.svg`
- **浅色文档 / README 浅底**：`logo-light.svg`
- **Favicon**：使用 `web/public/favicon.ico`，由静态角色 `favicon.svg` 导出 16/24/32/48/64/128/256px 多尺寸 ICO；浏览器入口使用已有的 `/favicon.ico` 静态路径。三份 SVG（本目录、`web/src/assets/brand`、`web/public`）保持同步。
- **不要**把奶油色字放在浅底上，**不要**把墨色字放在近黑底上
- 最小可读尺寸：mark ≥ 16px；完整 lockup ≥ 120px 宽
- 保持描边比例，勿压扁；透明底导出，勿自带背景色块

### 设计概念（一句话）

把原 Grasp「对握夹住旋转紫芯」升级成 cocofhu 玩具盒风：粗墨描边 + 粉/天蓝握臂 + 紫渐变圆角菱形芯，可读作图标，也可作英雄区「触发角色」剪影。

---

## 2. 颜色 Token

### 品牌主色

| Token | Hex | 用途 |
|-------|-----|------|
| `--ink` | `#3b2f4a` | 描边、浅底字标、强调轮廓 |
| `--cream` | `#fff8ef` | 浅底纸感、深底字标 |
| `--cream-soft` | `#fffdf9` | 更浅的纸白 |
| `--grasp-purple` | `#7B61FF` | 产品主强调 / 芯起点 |
| `--grasp-purple-mid` | `#818CF8` | 过渡 / 焦点环 |
| `--grasp-purple-soft` | `#A78BFA` | 芯渐变终点 / 高光紫 |
| `--pink` | `#ff9db5` | 左握臂、偶尔粒子 |
| `--sky` | `#9fe3ff` | 右握臂、偶尔粒子 |
| `--yellow` | `#ffd36e` | 得分/奖励点缀（少用） |
| `--lav` | `#e9e3ff` | 柔光垫色（少用） |
| `--bg-dark` | `#121214` | 首页主底 |
| `--bg-darker` | `#0A0A0B` | 更深层/遮罩 |

### 首页动效推荐调色（Grasp × cocofhu）

- **场景底**：`#121214` → 必要时落到 `#0A0A0B`
- **主强调 / 平台高光 / 角色芯**：`#7B61FF`（可渐变到 `#A78BFA`）
- **cocofhu 点缀（克制）**：`#ff9db5`、`#9fe3ff`、`#ffd36e`、`#e9e3ff`
- **墨描边瞬间**（平台描边、角色 outline、转场边框）：`#3b2f4a`
- 大面积仍保持暗色产品感；粉/天蓝/黄只做「玩具盒」亮点，不要铺满

---

## 3. 首页英雄动效结构（对齐参考视频）

参考 `/workspace/copilot-ref.mp4`（GitHub Copilot 英雄）**不是**简单循环 Lottie，而是：

1. **触发**：悬停 / 按键作用在中心 logo（或 logo 区域）
2. **进入玩法**：角色上跳；原 UI 向下滑出视野；平台从上方滑入（Doodle Jump 式竖向平台跳跃）
3. **镜头**：游玩时**只向上**平移，跳跃之间不要向下回拉镜头
4. **失败 / 结束**：快速但平滑的向下卷动；平台向上退出；原 UI 滑回**精确起始位置**；分数可叠在 logo 上
5. **静止**：结束停在静态英雄态，**不自动循环**；再次触发才重开

### 硬性体验（用户原话落地）

- **不要往上跳**：禁止滚动跳动、禁止布局偏移（CLS≈0）；用 `transform` / 绝对层做场景切换，不要改文档流高度突然变
- **切换顺滑**：场景交接用缓动，禁止硬切
- **尽头平滑**：失败回位要对齐像素级起点；不要「播完突然跳回第 0 帧」式硬循环
- **`prefers-reduced-motion`**：减动时只保留淡入/静态插画，跳过平台跳跃与镜头平移

### 推荐缓动

- 玩味 UI 弹跳：`cubic-bezier(.2, 1.5, .4, 1)`（有回弹但可控）
- 场景擦除 / UI 滑出滑入：expo-out（例如 `cubic-bezier(.16, 1, .3, 1)`）
- 失败回卷：稍快的 ease-in-out，仍要连续，禁止 teleport

### Logo 在动效里的角色

- `logo-mark` 适合作为**触发点 / 角色剪影**：圆角紫芯 + 两侧握臂在小尺寸仍可辨
- 进入玩法后可用紫芯作「跳跃体」，握臂可作轻微 squash/stretch 或拖尾（保持 ink 描边）
- 回位后 logo 与 wordmark 必须回到触发前同一几何位置，避免视觉跳动

---

## 4. 给实现同学的检查清单

- [ ] 深/浅 lockup 与 favicon 已替换（含 README / title / favicon）
- [ ] 英雄区触发与回位无 scroll jump、无 layout shift
- [ ] 失败回位后与初始状态像素对齐
- [ ] 支持 `prefers-reduced-motion: reduce`
- [ ] 动效主色为 Grasp 紫，pastel 仅点缀
