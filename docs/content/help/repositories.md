---
title: 仓库与 Git 授权
description: 登记仓库，为私有克隆和推送配置授权，并核对运行采用的分支和提交身份。
section: use
order: 40
keywords: [Git, GitHub, GitLab, SSH, Token, PAT, clone, 仓库, OAuth, device flow]
---

## 登记仓库 {#register}

1. 在「设置 → 凭据」保存 GitHub、GitLab Token 或 SSH 密钥。GitHub 与 GitLab 的 Host 可填官方站点或自己的域名。用 SSH 时一并配置可信主机。
2. 打开「设置 → 仓库」新增记录。填入仓库 URL，选择与协议和主机匹配的凭据。公开仓库可不选择凭据。
3. 填写需要的默认分支和提交身份。然后在工作流设置绑定仓库，或在新建 Solo 时选择。

| 仓库字段 | 改动效果 |
| --- | --- |
| 名称 | 影响工作流和 Solo 选择仓库时的显示。 |
| URL | 决定克隆地址，也用于校验凭据的适用主机和协议。 |
| 默认分支 | 提供启动运行时的分支选择。运行前仍可指定其他分支。 |
| 凭据 | 控制沙箱访问该仓库时使用的账号。凭据被撤销或删除后，新的启动可能失败。 |
| 提交用户名和邮箱 | 设置该仓库中的提交署名。 |

## 运行时如何使用 {#at-runtime}

启动工作流时固定选中的仓库列表、顺序、地址和分支。沙箱每次启动或恢复时使用已登记仓库的当前授权与提交身份。Agent 的模型配置决定编码工具和模型，Git 授权随仓库应用。

在沙箱终端运行以下命令，核对当前目录的分支和署名。

```bash
git status --short --branch
git config --get user.name
git config --get user.email
```

如果无法克隆，先检查地址、凭据的 Host 和协议，再确认账号能访问该仓库。修改凭据后，按界面提示重开会话或重试初始化。

## 用 GitHub 登录 {#github-login}

管理员为自己的 GitHub OAuth App 开启 **Device Flow** 后，可通过 `github.oauth_client_id` 或 `GRASP_GITHUB_OAUTH_CLIENT_ID` 提供 Client ID 并重启 Grasp。详见[部署配置](/help/configuration/#secrets)。

添加 github.com 凭据时，选「用 GitHub 登录」，前往 GitHub 输入界面给出的验证码。授权成功后生成普通 GitHub 凭据，可在仓库记录里选用。使用已有凭据行的「重新登录」可以更新 Token 和保留引用。自建 GitHub 使用手动 Token，组织限制 OAuth App 时需要组织批准。申请的权限包括 `repo`、`read:org` 和 `workflow`。需要只授权单个仓库时，可选择细粒度 Token。
