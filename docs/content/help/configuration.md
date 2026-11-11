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

Agent 可绑定多个已保存的 GitHub、GitLab 或 SSH 授权。所选授权都会进入沙箱，按站点或仓库范围使用；范围同样具体时使用列表里先出现的授权。同一平台的多个账号可在授权行的可选设置中分配适用范围。授权列表与待克隆仓库分别配置。仓库登记只保存名称、地址、分支和目录，不再保存访问方式、凭据绑定或环境变量。工作流节点使用所选 Agent 的绑定，共享沙箱切换节点时先清理旧授权再应用新列表；空列表表示不注入这组默认 Git 授权。升级会清除历史仓库和 Solo 快照里的旧权限与环境，不会把它们迁移到任何 Agent；需要私有仓库或变量时，在当前 Agent 中重新配置。失效引用会阻止运行，不自动更换账号。

GitHub / GitLab 凭据在同一表单配置 Host 与 Token，Host 默认是官方站点，也支持自建域名及端口；不再单独配置 GitLab URL。默认配置可保存多个模型组和多个 Git 授权；每个 Agent 用独立开关选择是否继承模型或 Git 授权，关闭时使用自己的选择，默认项失效时不会自动换用其他配置。

### 用 GitHub 登录（设备码）

配置后，新增 github.com 的 GitHub 凭据时可以选「用 GitHub 登录」：页面显示 8 位验证码，用户在 `https://github.com/login/device` 输入并授权后，平台自动保存 Token，不需要手动创建 PAT。结果是一条普通 GitHub 凭据（`GITHUB_TOKEN`，Host 为 `https://github.com`），绑定和注入方式与粘贴的 Token 相同；凭据行会显示 GitHub 账号，并可「重新登录」更换 Token，凭据 ID、别名和已有绑定不变。未配置时界面只有粘贴 Token。

部署方需要：

1. 在 GitHub 的 **Settings → Developer settings → OAuth Apps** 新建 OAuth App（个人或组织均可）。Homepage URL 填平台地址；设备码流程不用回调，Authorization callback URL 可填同一地址。
2. 在 App 设置中勾选 **Enable Device Flow**。没勾选时登录会提示 Device Flow 未开启。
3. 把 App 的 **Client ID** 配给服务端：环境变量 `GRASP_GITHUB_OAUTH_CLIENT_ID`，或 YAML `github.oauth_client_id`（环境变量优先），然后重启服务。不需要 client secret，也不要配置它。

说明与限制：

- 申请的 scope 固定为 `repo read:org workflow`。Token 对授权用户能访问的所有仓库都有 `repo` 权限；需要按仓库细分权限时，仍粘贴 fine-grained PAT。授权结果缺少 `repo` 时凭据照常保存，但会提示可能无法克隆私有仓库。
- 组织开启了 OAuth App 访问限制时，需要组织管理员批准该 OAuth App，否则 Token 访问不到该组织的仓库。
- 只支持 github.com。自建 GitHub Enterprise Server 继续粘贴 Token。
- 授权会话保存在服务进程内存，有效期以 GitHub 返回为准（通常 15 分钟）。服务重启后进行中的登录失效，重新获取验证码即可；多实例部署需要粘性会话。
- OAuth App 的 Token 一年未使用会被 GitHub 自动吊销，届时在凭据行「重新登录」。

### 多模型组与上下文接续

Agent 可绑定不同编码工具的多个模型组，并指定一个默认组。首页、Solo 新建会话及会话内的模型选择包含全部已绑定组的主模型和辅助模型；同名模型会标明所属模型组和工具。未主动选择时使用默认组的主模型，工作流自动执行也使用默认组。

在会话内选择模型时，会同时选择该组的编码工具、账号和推理参数。正在生成的这一轮继续完成，新选择从下一轮生效。换工具会保留当前工作目录与未提交改动，并将早期要求、近期对话、附件引用和工具结果交给新工具；需要更早原文时，Agent 可通过 artifact-store 的 `get_history` 分页回查。不同工具的原生会话状态不能完整互通，接续依据平台保存的任务记录和当前工作区。

模型组或绑定保存成功后，本页和同一浏览器的其他标签页自动刷新可选项；其他用户刷新后看到最新配置。当前选择有效时保持不变，停用、删除或解除绑定后需要明确重新选择。暂时读取失败不会被当成模型已删除。使用不同专用沙箱镜像、或 Git 授权发生变化时，界面会提示需要重新打开会话。

### 每条回复的时间与运行信息

Solo、工作流、Agent 测试和 PM 使用同一套聊天展示。悬停消息可查看时间；回复旁的信息图标支持悬停、键盘聚焦和触屏点击，显示该轮实际 Agent、编码工具、模型、推理强度、耗时和 Token 用量。输入、输出、缓存及不同模型的统计会分别显示，并保留服务商上报或估算的来源说明。

这些信息属于该条回复的执行快照，后续切换模型不会改写旧回复。历史消息没有记录的字段显示为未提供，不使用当前选择推测；尚未完成的回复只显示已收到的信息。

## 数据库与附件同生命周期

正式栈中 SQLite（`./.localdata/db`）与应用数据/默认 blobs（`./.localdata/app-data`，或自定义 `GRASP_BLOBS_ROOT`）必须**成对备份与成对清理**；迁移/升级勿只搬库。否则复合变量附图会出现孤儿 `blob:` 引用（GET `/api/blobs/:id` → 404）。历史孤儿仅界面永久失败占位，本次不做巡检台。详见 [快速开始](../guide/quick-start.md#数据库与附件同生命周期备份--清理)。

## 相关

- [网关](../gateway/)
- [贡献](../contributing/)
