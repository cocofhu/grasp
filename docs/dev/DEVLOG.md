# 开发日志

仓库内部流水，不发布到公开站点。最新条目在上。不回填 git 历史，不写排期。

对外行为变化仍按原有惯例另记仓库根目录 `CHANGELOG.md` 的 `Unreleased`。没有对外行为时不要改 `CHANGELOG.md`。

## 条目模板

复制下面这一块，填完后放在本文件「记录」一节的最上方。

```text
### YYYY-MM-DD

- 日期：YYYY-MM-DD（写下这条的当天，不是排期）
- 范围：路径
- 做了什么：
- 为什么：
- 如何验证：
```

## 记录

### 2026-10-04

- 日期：2026-10-04
- 范围：`docs/agent/README.md`、`docs/dev/DEVLOG.md`、`docs/README.md`、`AGENTS.md`
- 做了什么：新增英文文档边界手册；在根 `AGENTS.md` 文首增加「读哪份文档」表和开发日志硬规则，命令、门禁数字、六条已知坑和勿碰清单保持原样；建立本日志并写入这一条；在 `docs/README.md` 布局表标明 `agent/` 与 `dev/` 不进入 `public/`。
- 为什么：编码 Agent 需要从根短入口找到命令、文档边界和日志写法，同时不把角色包使命、公开帮助站和对外 CHANGELOG 混在一起。
- 如何验证：根 `AGENTS.md` 到 `docs/agent/README.md` 与 `docs/dev/DEVLOG.md` 的相对链接可打开；`CHANGELOG.md` 与 `agents/*/workspace/AGENTS.md` 无改动；在 `docs/` 执行 `npm run build` 后，`public/` 中不出现 `agent/` 或 `dev/` 页面。
