# 2026-11-17（放大思考强度闪电并接入 Codex Fast）

- 日期：2026-11-17
- 范围：`web/src/components/solo/`、模型选择与对话状态、API 类型和请求；`server/internal/{clicatalog,modelrouting,models,services,handlers,engine,runtime,sandbox}/`；`sandbox-gateway/sandbox/internal/{provider,service,handler}/`；沙箱集成检查脚本；`CHANGELOG.md`
- 做了什么：思考强度菜单入口使用 24px 闪电，面板内使用 36px 独立 Fast 按钮和 28px 图标，提供按下状态、键盘操作、明暗主题与“响应更快，用量更多”提示。Fast 使用独立 `fastMode` 状态，支持能力来自精确模型与认证来源的 `fastModeSupported`，目前只对内置清单中支持 Fast 的 Codex 官方模型开放。打通 Solo 创建、首页启动、会话与工作流访问、独立预览、流事件、应用与待应用状态、回滚和恢复；省略字段保留选择，切换到不支持的模型时清除，显式开启不支持的模型返回 `fast_mode_unavailable`。Codex 开启时设置 `features.fast_mode=true`、`service_tier="fast"`，关闭时明确设置 `service_tier="default"`，保持推理档位独立。
- 为什么：原闪电只作装饰且实际图形偏小，界面也没有可操作的速度档位。仅调整前端或省略关闭参数会导致实际请求仍走旧档位；需要保存、对齐并验证实际模型请求。
- 如何验证：服务端全量 Go 测试、vet、配置文档校验、golangci-lint 2.12.0、指定 runtime 回归通过，核心覆盖率 90.7%；模型设置上下文与 Fast / 回滚定向回归通过。沙箱全量 Go 测试、vet、lint、核心覆盖率 98.0%、Shell / 启动 / 注入 / Git 冒烟、46 项 Node 和 10 项 Python 契约检查通过；桌面与预览上游冒烟在干净 Docker 镜像中通过，避开宿主机现有 Openbox / 端口状态干扰。完整 8 种工具、84 轮切换及 8 项原生 Codex Fast HTTP 检查通过，验证开启的实际请求使用 `priority`、关闭会清除、推理档位独立、聊天隔离与恢复；模型端点为本地模拟服务，没有付费模型调用，不代表供应商平台实际推理验证。
  前端 lint、类型检查、6080 项全量单测通过，行覆盖率 91.72%；最后的 Fast-only 序列化和新建工作流默认模型能力判断分别通过增量回归，最终 35 项相关测试及构建通过（Node 堆上限 4096 MiB）。26 项模型路由浏览器回归通过，人工查看桌面 / 手机、明暗主题截图，并检查闪电实际图形尺寸。全量浏览器检查以 `--max-failures=1` 运行：221 通过，`run-live-continue.spec.ts:406` 的实时输出恢复断言失败，114 未运行；失败用例单独重跑通过，不能据此宣称全量浏览器检查通过。全局 CI 脚本、文档构建与审计通过；`git diff --check` 通过。
