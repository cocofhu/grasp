# 2026-11-13（品牌检查跳过 docs 构建产物）

- 日期：2026-11-13
- 范围：`scripts/assert-no-approving-brand.mjs`
- 做了什么：新增按仓库相对路径跳过的 `skipDirPaths`，跳过 `docs/public`（#1139）。不按目录名 `public` 跳过，`web/public` 等源码目录仍会检查。
- 为什么：docs 构建把 `docs/site/js/locale.js` 复制到 gitignore 的 `docs/public/js/locale.js`，白名单只放行源路径，同一工作副本先构建 docs 再构建 web 时品牌检查失败。
- 如何验证：`docs/` 下 `npm run build` 后，旧脚本报 `docs/public/js/locale.js:2` 退出 1，新脚本通过；随后 `web/` 下 `npm run build` 通过。在 `web/public`、`docs/site`、`docs/publicx`、`docs/content/public` 临时放入违规文本，新脚本仍全部报出。
