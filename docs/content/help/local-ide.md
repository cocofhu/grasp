# 用本地 IDE 连接沙箱

开启内置 SSH 网关后，可以用本地的 VS Code、Cursor（Remote-SSH）或任意终端直接打开沙箱，E2B、Docker、Kubernetes 用同一种方式。本地只需要 IDE 和系统自带的 OpenSSH，不用安装其他程序。

```
VS Code / Cursor ──SSH──> Grasp SSH 网关（默认 2222 端口）──> 沙箱 sshd
```

SSH 用户名决定连哪个沙箱：`sbx-<沙箱 ID>`。网关用你在 Grasp 登记的公钥认证，再以 root 身份连进沙箱。公钥只保存在 Grasp，不会写入沙箱；删除公钥后，使用它的连接会立即断开。

## 管理员：开启网关

| 配置 | 环境变量 | 说明 |
| --- | --- | --- |
| `ssh_gateway.enabled` | `GRASP_SSH_GATEWAY_ENABLED` | 设为 `true` 开启，默认关闭 |
| `ssh_gateway.listen` | `GRASP_SSH_GATEWAY_LISTEN` | 监听地址，默认 `:2222` |
| `ssh_gateway.public_host` | `GRASP_SSH_GATEWAY_PUBLIC_HOST` | 用户连接的主机名；留空使用浏览器访问 Grasp 的主机名 |
| `ssh_gateway.public_port` | `GRASP_SSH_GATEWAY_PUBLIC_PORT` | 用户连接的端口；端口映射或负载均衡改了端口时设置 |
| `ssh_gateway.host_key_path` | `GRASP_SSH_GATEWAY_HOST_KEY_PATH` | 网关主机私钥，首次启动自动生成 |
| `ssh_gateway.max_conns` / `max_conns_per_sandbox` | 同名大写 | 全局 / 单沙箱连接上限，默认 200 / 10 |
| `ssh_gateway.admins_only` | `GRASP_SSH_GATEWAY_ADMINS_ONLY` | 只允许管理员账号连接 |

部署时注意：

- **对外开放 TCP 端口。** Docker Compose 示例使用主机网络，放行防火墙端口即可。Kubernetes 的 Ingress 只转发 HTTP，需要另建 `LoadBalancer` 或 `NodePort` 类型的 Service，示例见 `sandbox-gateway/deploy/k8s/stack.example.yaml` 末尾的 `grasp-ssh`。公司网络只放行 443 时，这个端口可能不通。
- **主机私钥要持久化。** 所有沙箱共用这一把主机密钥，所以沙箱重建或 E2B 恢复后不会出现「主机密钥已变更」的提示。它必须放在持久卷上（示例都放在 `/data` 下），多副本部署时还要共享，否则每次重新部署客户端都会报警。
- **配置只在启动时读取**，修改后需要重启服务。

## 安全说明

- 访问范围按账号授权：Solo 会话的所有者可以进入自己会话的沙箱，管理员可以进入任意沙箱；工作流、测试等没有所有者的沙箱只对管理员开放。不存在和无权访问的沙箱返回同样的提示。会话被删除或转给他人、公钥被删除、账号被移除时，已有连接会在一分钟内断开（删除公钥时立即断开）。开启 `admins_only` 后只有管理员能连接。
- 网关会解开并重新建立 SSH 连接，能看到会话明文。平台本来就持有沙箱的 root 权限，安全边界不变。
- 只支持公钥认证。同一来源地址在 5 分钟内连续认证失败 20 次后会被暂时拒绝；尚未完成认证的连接单独限量（全局 64 条、每个来源 8 条），不会占用已认证连接的名额。
- 不支持远程端口转发（`-R`）、agent 转发和 X11 转发；本地端口转发（`-L`）、SFTP 和 VS Code/Cursor 需要的通道都可用。
- 网关重启时现有连接会断开，IDE 会自动重连。

## 用户：第一次连接

1. 在本机生成密钥（已有可跳过）：`ssh-keygen -t ed25519`。
2. 在左下角账号菜单打开 **「SSH 公钥」**，粘贴 `~/.ssh/id_ed25519.pub` 的内容。支持 ed25519、ECDSA 和至少 2048 位的 RSA。
3. 打开一个 Solo 会话，点击顶部右侧的 **信息图标**，在「本地 IDE」里选择一个 IDE。还没选过时这里显示 **「SSH 连接」**。菜单列出 VS Code、Cursor、CodeBuddy、CodeBuddy 中国版、Trae、Trae 中国版、Antigravity 和 Codex，上次的选择会被记住。
4. 浏览器会询问是否打开对应应用，IDE 第一次还会确认是否打开这个远程文件夹，都选打开即可。IDE 连接 `sbx-12@grasp.example.com:2222`，直接打开 `/root/workspace`，不需要修改 `~/.ssh/config`。

| IDE | 打开方式 |
| --- | --- |
| VS Code | 需要安装 Remote - SSH 扩展 |
| Cursor | 首次使用时按提示安装 Cursor 的 Remote SSH 扩展 |
| CodeBuddy / Trae | 自带 Remote-SSH；国际版和国内版是两个应用，菜单里各占一行 |
| Antigravity / Codex | 用各自的 Remote-SSH 链接打开 `/root/workspace` |

菜单底部的 **「SSH 连接信息」** 会弹出用户名、主机、端口、项目目录和一条 SSH 命令（例如 `ssh -p 2222 sbx-12@grasp.example.com`），可直接在终端运行。想用 `ssh grasp` 这样的短名字时，可以把弹窗里的可选配置加入 `~/.ssh/config`：

```
Host grasp
  HostName grasp.example.com
  Port 2222
  ServerAliveInterval 30
```

一台电脑可以同时连接多个沙箱，每个沙箱一个窗口。

## 连接与沙箱状态

- **已暂停的 E2B 沙箱**会在连接时自动唤醒，可能需要几十秒。VS Code 默认 15 秒超时，如果连接失败，把设置 `remote.SSH.connectTimeout` 调到 60。
- **手动停止或已关闭的会话**不会被 SSH 连接唤醒，终端会显示原因，例如「会话已手动停止，请先恢复」。先在 Grasp 中恢复会话再连接。
- **连接期间不会被空闲回收。** 只要还有 IDE 连接，E2B 沙箱就不会被自动保存并暂停，也不会清理内存缓存；最后一个连接断开后重新开始计时。信息面板里会显示「本地 IDE 连接中，暂停计时」。
- 信息面板还会显示环境类型、自动回收倒计时、最近模型活动和最近备份，并提供停止、唤醒、恢复和「保持运行」开关。

## 资源占用

VS Code / Cursor 会在沙箱里运行远程服务端，约占 0.5–1.5 GB 内存（JetBrains 约 2–4 GB），规格较小的模板可能挤占 Agent 的内存。Docker/Kubernetes 沙箱停止后，`~/.vscode-server`、`~/.cursor-server` 不在工作区归档内，下次连接会重新下载。
