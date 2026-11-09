# 2026-11-09（发布与源码栈显式初始化新安装）

- 日期：2026-11-09
- 范围：`start.sh`、两套 Compose、`.env.example`、`release-smoke.sh`、`scripts/init-execution.*`、中英 README、`CHANGELOG.md`。
- 做了什么：增加 `./start.sh init-execution --offline` 及源码栈等价命令，先核对对应 Compose 应用已停止，再单独运行初始化管理工具，不自动停止服务。执行控制与原维护收据分别挂载到独立持久目录，控制存储显式使用自己的配置及凭据。发布镜像通过 Node 创建私有临时配置，不要求镜像内存在 `config.yaml`；源码模式使用原配置并运行源码管理命令。普通启动、重启不初始化。
- 为什么：新安装也须显式建立独立执行连续性身份。已有 SQL、缺失或变化的控制标记不能被普通安装流程误当成空库；原私有工作目录及真实外部票据允许确认中断的原请求。发布 smoke 只接受不存在的 `.localdata`，使用独立 Compose 项目，退出时只清理自身创建且 inode 仍一致的目录，避免失败时影响已有安装。
- 如何验证：Go 1.26.10 编译真实管理工具，Node 11 项测试覆盖真实 SQLite 初始化、原票据重试、成功 ACK 丢失、外部标记丢失或替换、旧 SQL 拒绝、路径隔离及独立凭据；source/release 启动参数、停机证明失败、普通启动不初始化及 smoke 顺序使用明确的 Docker CLI 协议夹具核验，不冒充完整发布镜像 E2E。实际 Docker Compose 渲染确认两套配置均转发独立控制参数并有四个不同持久根。文档安装、构建与审计通过；完整镜像 smoke 仍须使用配套迁移版镜像 digest 执行，现有 v3.2 数据按 `server/UPGRADING.md` 迁移。
