# 2026-11-11（SSH 终端找不到 /root/.local/bin 下的 Agent CLI）

- 日期：2026-11-11
- 范围：`sandbox-gateway/sandbox/scripts/startup.sh`、`sandbox-gateway/sandbox/Dockerfile`、`sandbox-gateway/deploy/e2b/prepare-template.py`
- 做了什么：
  - `startup.sh` 在启动 sshd 前把完整 PATH 写入 `/etc/environment`（`persist_session_path`），并补充加载 `/etc/profile.d/agent-path.sh`。
  - 镜像的 `/root/.bashrc` 加载 `agent-path.sh`；`agent-path.sh` 改为已存在时不重复追加。
  - E2B 模板的 `/etc/profile.d/grasp-e2b-env.sh` 不再整体覆盖 PATH，只补缺失项。
- 为什么：Web 终端通过 SSH 执行 `env -C <dir> bash -i`，`ExecScript` / `Exec` 也走 SSH。SSH 会话不继承容器 `ENV`，非登录 shell 也不读 `/etc/profile.d`，PATH 只来自 pam_env 读取的 `/etc/environment`，因此 `agent`、`cursor-agent`、`claude`、`trae-cli` 以及 Go / Java / mongocli 均找不到。Docker、Kubernetes、E2B 都经 sshd，问题相同。E2B 的 profile 片段按文件名排在 `devtools-path.sh` 之后，覆盖 PATH 会丢掉 Java / mongocli。
- 如何验证：在运行中的沙箱执行 `persist_session_path` 后用 SSH 登录，会话初始环境（`/proc/$$/environ`）的 PATH 含 `/root/.local/bin`，`env -C /root/workspace bash -i` 下 `agent --version` 等命令可用；临时文件上重复执行 `persist_session_path` 结果不变、保留其他变量、权限 644；生成的 E2B profile 片段重复加载不产生重复项且保留已有的 Java 路径；`python3 -m unittest test_prepare_template` 通过。
