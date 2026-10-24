---
title: 配置
description: 配置要点摘要；完整说明见源码 CONFIGURATION.md。
---

Grasp 平台服务配置以 YAML / 环境变量为主（本地示例见 `server/config.example.yaml` 与根目录 `.env.example`）。ACP、Git 等运行时凭据只在「设置 → 凭据」 中管理。

## 完整文档

请以仓库内权威文档为准（避免本站与源码双份漂移）：

- [server/CONFIGURATION.md](https://github.com/cocofhu/approving/blob/main/server/CONFIGURATION.md)

该文档由 `go run ./cmd/gen-configdoc` 生成/校验，CI 会 `-check`。

## 本地快速路径

```bash
./start.sh -d          # 发布镜像栈
./start.sh dev -d      # 源码 + HMR
```

镜像 tag / digest、网关与沙箱相关变量见 `.env.example`。Agent 侧 API key、GitHub / GitLab Token、SSH 等只能保存到工作区凭据设置；写进共享 Agent 配置、Agent meta 或平台 `sandbox.env` 会被拒绝。先在「凭据」标签保存预设或自定义服务的 API Base 和 Key，再在「模型」标签组合编码工具、凭据与所选模型，Agent 通过 `modelBinding.configurationId` 引用具名配置。模型路由字段由选定配置提供，Agent env 不能覆盖。其他非敏感项可放在 Agent meta env（值可引用 `${vars.<name>}`）。模型运行时参数只来自当前模型配置。

Agent 可绑定多个已保存的 GitHub、GitLab 或 SSH 授权，并指定一个首选。所选授权都会进入沙箱，按站点或仓库范围使用；同一平台的多个账号可在授权行的可选设置中分配适用范围。授权列表与待克隆仓库分别配置。工作流节点使用所选 Agent 的绑定，共享沙箱切换节点时先清理旧授权再应用新列表；空列表表示不注入这组默认 Git 授权。独立配置的仓库权限另行管理，新建 Agent 不会自动继承共享仓库授权。失效引用会阻止运行，不自动更换账号。

GitHub / GitLab 凭据在同一表单配置 Host 与 Token，Host 默认是官方站点，也支持自建域名及端口；不再单独配置 GitLab URL。Agent 默认配置可保存具名模型和多个 Git 授权；每个 Agent 用独立开关选择是否继承模型或 Git 授权，关闭时使用自己的选择，默认项失效时不会自动换用其他配置。模型选择同时确定 CLI。

## 数据库与附件同生命周期

正式栈中 SQLite（`./.localdata/db`）与应用数据/默认 blobs（`./.localdata/app-data`，或自定义 `GRASP_BLOBS_ROOT`）必须**成对备份与成对清理**；迁移/升级勿只搬库。否则复合变量附图会出现孤儿 `blob:` 引用（GET `/api/blobs/:id` → 404）。历史孤儿仅界面永久失败占位，本次不做巡检台。详见 [快速开始](../guide/quick-start.md#数据库与附件同生命周期备份--清理)。

## 相关

- [网关](../gateway/)
- [贡献](../contributing/)
