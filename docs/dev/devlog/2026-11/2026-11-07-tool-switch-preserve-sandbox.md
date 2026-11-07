# 2026-11-07 用户切换工具保留沙箱与仓库状态

- 日期：2026-11-07
- 范围：`server/internal/{sandbox,runtime,services}/`、`sandbox-gateway/sandbox/{scripts/reprime.sh,README.md}`、`sandbox-gateway/scripts/test-reprime.sh`、`CHANGELOG.md`。
- 做了什么：新增明确的 `SwitchTool` 操作，将 Solo 和工作流聊天的工具/模型组切换及回滚接入仅更新工具的模式。容器内跳过 Git/SSH 凭据初始化、全局 Git 配置修改和仓库克隆；本地工作树仍只替换后端进程。共享工作流节点交接继续保留新增仓库准备及克隆失败语义。
- 为什么：#1132 中 HTTPS 仓库首次克隆失败后会话仍可用，后续切换工具复用完整 re-prime 再次补克隆，导致普通对话在进入模型前等待数分钟并失败回滚。用户切换工具应复用同一沙箱和工作区，不受仓库补下载影响。
- 如何验证：新增脚本回归覆盖继承/显式仓库列表、缺失仓库、普通目录、脏仓库、真实 worktree、后端启动失败与回滚，逐次校验工作区和 Git/SSH 文件保持原样，并禁止任何 Git 调用及 startup 依赖；另验共享节点仍能克隆本地仓库并报告克隆失败。新增 Go 回归区分工具切换与节点初始化入口，验证工作流聊天复用同一沙箱；Solo 现有成功、回滚和本地工作区保留回归改走新操作。Go 1.26.10 下服务端全量测试、lint/vet、配置文档检查及指定 runtime 回归通过，核心覆盖率 90.6%；沙箱 lint/vet、Shell 语法、注入/凭据/bootstrap/预览冒烟和覆盖率门禁通过（96.6%）。切换与回滚相关 Go 回归在 race 下重复 20 次通过。完整 Agent E2E 覆盖 8 个工具、全部 56 个有向切换组合及模型/推理设置，共 84 轮通过，文件和历史连续；5 个原生 CLI，Cursor/Trae/Kimi Code 明确为协议夹具，不声称厂商推理。根目录品牌、actionlint、shellcheck、离线文档发布门禁及 docs 构建/官方 registry audit 均通过。
