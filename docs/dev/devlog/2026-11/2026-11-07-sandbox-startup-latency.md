# 2026-11-07（消除沙箱启动时的 SSH 认证放大与代理管道等待）

- 日期：2026-11-07（写下这条的当天，不是排期）
- 范围：`server/internal/sandbox/{artifactcli*,ssh*,seedhelpers/grasp-mcp-spa-proxy.py,testdata/mcp_spa_proxy_test.py}`、`server/scripts/grasp-mcp-spa-proxy.py`、`sandbox-gateway/sandbox/{scripts/startup.sh,README.md}`、`sandbox-gateway/scripts/test-{sshd-policy,runtime-e2e}.sh`、`CHANGELOG.md`。
- 做了什么：辅助文件以带权限的 tar 一次安装，复用 SSH 就绪检查获得的连接执行安装和可选代理检查；代理空映射直接返回，真正后台化时以 `dup2` 重定向文件描述符 0/1/2，绑定失败不再报告成功。运行时在主 SSH 配置最前面写入拒绝空密码的策略，已有 sshd 则重载配置，其余认证设置保留。增加真实 fork/HTTP 代理、真实 SSH 通道与文件落盘、OpenSSH 配置解析和密钥/密码认证回归，并把 OpenSSH 回归接入已有运行时镜像 E2E。
- 为什么：一次慢启动记录中，网关准备约 12.5 秒，之后八次 SSH 各约 3 秒，代理检查又等待到 15 秒超时。导入模板允许空密码，而运行时设置了非空密码，SSH 客户端的初始 `none` 认证进入 PAM 的失败延迟；多次重新认证放大了这项开销。代理仅替换 Python 的 `sys.stdout` 等对象，实际 SSH 管道仍被后台进程持有。两者都属于初始化路径问题，不应归因于 E2B 原生虚机启动耗时。
- 如何验证：新增回归已验证真实进程释放输出管道、空映射无副作用、重复启动幂等、绑定失败可见和带认证头的 MCP 转发；真实 SSH 安装只用一条连接、两条会话，逐个核对文件内容和权限，并验证安装失败不阻断创建且可重试。隔离 Ubuntu 22.04 容器运行真实 OpenSSH，验证模板 Include 的优先级、配置幂等与权限保留，以及公钥登录、密码回退和未知公钥拒绝。使用生产相同的 `golang.org/x/crypto/ssh` v0.57.0 在该容器分别建立三次连接并执行短命令，修改前为 2375.8 / 4671.8 / 2374.5 ms，重载 SSH 配置后为 75.5 / 73.5 / 74.6 ms；这只验证 SSH 认证环节，不能当作 E2B 全链路启动耗时。提交前还运行仓库规定的通用、服务端、沙箱和文档检查，具体结果列于 PR。

运行时仍只依赖现有镜像软件，bootstrap 子命令、退出码和 `IMAGE_LEVEL` 均无变化。SSH 配置在一次性启动阶段生效；热更新包不会替现有沙箱重跑启动。本次只提交 PR，未部署或修改在线沙箱、模板和 Apollo。
