# 2026-11-09 工作流 Git 代码导入 Solo

- 日期：2026-11-09
- 范围：`server/internal/{models,router,handlers,sandbox,services}/workspace_import*` 与 `workspace_code*`、模型和路由注册、`filestore/catalogue.go` 与迁移/备份扫描、`sandbox-gateway/sandbox/internal/{handler,workspacegit,router}`、`web/src/components/solo/SoloWorkflow{Panel,Import}*`、中英文页面文案、新浏览器用例与必需测试列表、`CHANGELOG.md`。
- 做了什么：终态工作流卡片使用现有抽屉提供来源、仓库和完整提交选择，先生成不透明预览收据，再由用户明确确认。受限 Git bundle 包含所选提交及可达历史，支持未推送提交；原 index、refs、脏文件与目标工作区不变，结果只落入排他新建目录并写回持久聊天上下文。未知派发只读核对运行时完成收据，不自动重复；刷新通过已授权的元数据列表找回。远端请求检查已确认 generation/expiry 并在到期或被新代次取代时终止 Git 进程组。显式 Binary 归档引用加入无 ORM hook 的类型目录，迁移/备份保留旧扫描顺序并验证新归档及冷恢复。
- 为什么：工作流与 Solo 独立运行，导入需明确选择来源及代码版本，不能把执行的最后目录当成任意历史轮次，也不能用覆盖 Solo 原目录或自动提交来衔接。此批只完成已提交 Git 的增量能力，完整文件快照、接管和返回继续分别推进。
- 如何验证：仅含本批的独立源码快照通过完整 server 测试/vet/lint/配置一致性/运行时定向及核心覆盖率 90.0%；沙箱模块 lint/vet/核心覆盖率 96.6%，运行时注入、Git 授权、bootstrap、mock chat 和预览注入烟测通过。前端 6,346 项通过、Lines 91.58%，类型检查与构建通过；必需浏览器列表新增导入用例后 380/380 通过，桌面及 390px 窄屏截图已核对。定向服务和真实 Git 回归 race 重复 3 次通过，验证源和目标脏文件保留、未推送多仓库历史、重复收据不覆盖后续编辑、丢失执行权、部分失败、归档安全及进程组取消；新 raw 引用的迁移、备份及目标单独冷恢复 race 重复 2 次通过。运行时逻辑使用 bundle，不修改镜像包、bootstrap 契约或 IMAGE_LEVEL。新提交的分支部署及真实产品 API 导入另行验证，不以此前 f999 的部署报告代替。
