---
title: 贡献
description: 如何为 Grasp 做贡献；指向仓库贡献指南。
section: support
order: 20
---

欢迎贡献。请先阅读：

- [CODE_OF_CONDUCT.md](https://github.com/cocofhu/approving/blob/main/CODE_OF_CONDUCT.md)
- [SECURITY.md](https://github.com/cocofhu/approving/blob/main/SECURITY.md)
- [CONTRIBUTING.md](https://github.com/cocofhu/approving/blob/main/CONTRIBUTING.md)

面向 Agent / 贡献者的短规则（path→命令、门禁、勿碰清单）：

- [AGENTS.md](https://github.com/cocofhu/approving/blob/main/AGENTS.md)

## 本站（docs/）本地构建

```bash
cd docs
npm ci --no-audit --no-fund
npm run build
```

预览：

```bash
BASE_PATH=/ npm run server
```

产物在 `docs/public/`。推送到 `main` 且改动 `docs/**` 时，`ci-docs` 会构建并部署到独立 Pages 仓库 `cocofhu/approving-pages`（需配置 Secret `PAGES_DEPLOY_KEY`）。

## 相关模块门禁

改 `server/`、`web/`、`sandbox-gateway/` 时请按 `AGENTS.md` 跑对应本地套件，不要只依赖 always-on 的 `ci` gate。
