# 2026-11-09 代码导入按仓库解释能力限制

- 日期：2026-11-09
- 范围：`sandbox-gateway/sandbox/internal/workspacegit/`、`server/internal/services/workspace_import{,_test}.go`、`web/src/components/solo/SoloWorkflowImport{.vue,.test.ts}`、中英翻译及 `CHANGELOG.md`。
- 做了什么：Inventory 返回稳定的受限原因代码，服务端仅放行白名单，前端解释子模块、LFS、浅克隆、外部对象、关联工作树、自定义转换和不安全 Git 元数据等限制。未知内部错误仍使用通用提示，不显示主机路径或 Git 原始错误。导入说明明确完整历史包大小在预览时验证，来源须保留且正在运行。
- 为什么：安全边界应在用户选择仓库时清楚显示，避免只看到“不支持”后反复尝试。完整历史包的 64 MiB/仓库与 128 MiB 总上限仍保留；本次没有扩大支持范围或自动唤醒环境。
- 如何验证：13 项导入组件测试、后端导入 race 两轮、沙箱原生 Git race 三轮、类型检查及两模块定向 vet/lint 通过。另纳入隔离源码的后端、沙箱及前端完整门槛。此前固定提交的真实原生 CLI 工作流到代码导入验收通过；本次原因提示改动未借用该部署冒充已经上线。
