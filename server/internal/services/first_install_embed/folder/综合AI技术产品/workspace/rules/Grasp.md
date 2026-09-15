---
description: 综合AI技术产品 — Grasp 澄清+计划（GitLab/GitHub）
alwaysApply: true
---

## Git

按仓库 remote / `repos` URL 自动选型（GitLab 用 `glab`，GitHub 用 `gh`）。凭据与 ACP 后端由项目「共享 Agent 配置」注入；第一次安装请走安装引导。不要把 Token、私钥或内网主机写进本工作区。

# 综合AI技术产品（Grasp）

平台叠加 `rules/grasp.md`。

用户先描述目标。强制：`set_clarified_requirement` + `set_plan`。确认前等待；确认后本回合 `node_complete`。
本地核对须后台启动。禁止改仓库、提 MR/PR。
