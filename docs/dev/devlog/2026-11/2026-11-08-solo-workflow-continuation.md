# 2026-11-08 Solo 工作流管理与持久化基础

- 日期：2026-11-08
- 范围：`server/internal/{objectstore,payloadstore,filestore,blob,config,services,engine,handlers,models,router,sandbox}`、`server/cmd/server`、`web/src/{components/solo,components/workflow,views/SettingsView.vue,lib,locales}`。
- 做了什么：接入 S3 / COS 长正文及附件存储、位置登记与校验后的旧文件归档；统一 Web / API / PM / Solo 工作流启动校验、功能开关、持久化启动幂等及容量控制；Solo 工作流任务卡复用现有澄清和审批聊天；状态变化与结果 outbox 同事务提交；新增保留工作区的 Solo 环境启停、数据库操作租约、计算配额预留及按 Run 的安全边界暂停。
- 为什么：将 Solo 作为持续对话与任务管理的基础，避免历史与附件依赖单个 server 本地目录、浏览器离线丢结果、手动停机被自动恢复，以及重复启动造成重复任务。保留已有外部 API / PM 启动渠道，统一受开关约束。
- 如何验证：已完成新存储单元回归、启动并发重试和权限/开关回归、跨服务实例的环境操作租约、手动停止后零唤醒和失败恢复保留、离线结果幂等补投、单 Run 暂停隔离；实际 Kubernetes SeaweedFS 经 Go SDK 验证 9.75 MB 分段上传、旧本地正文/附件归档与清空本地目录后的完整恢复。前端类型检查与相关组件回归已运行。完整模块 gates、聊天优先历史、自动节点补充指令、工作区快照/接手和集成分支验收仍在进行。

- 补充实现：历史 Run 默认聊天路由，原 Solo 仅由其所有者复用；缺失/已删除/私人 origin 使用有权查看的 Run 对话，明确点击后事务创建唯一 reference Solo。原需求、对话链与产物冻结为独立的持久化上下文，简短通知仍使用项目聊天 UI；已补上系统结果进入下一轮模型输入及 get_history 的路径。首页保留角色/仓库/模型/草稿/@引用/附件/品牌样式，只提供 Solo，工作流定义留在设置。删除了对应已移除的首页工作流 rail 行为测试，改为手动启动、多任务与开关的浏览器验收。
- 补充修复：附件输入归档改为返回新值，避免并发启动重试修改请求体和幂等指纹；带附件并发重试 5 轮 race 通过。聊天优先入口、私人 origin、删除 fallback、跨服务并发创建、结果上下文回流及旧详情深链已回归。文件快照/代码导入、自动节点邮箱、流式持久化与分页、完整恢复和最终分支集成验收仍未完成。

- 本轮门禁：server 全量测试、lint/vet、配置文档检查与 core 90.7% 通过；web 6298 项单测（Lines 91.6%）、类型/lint/build、371 项 desktop/mobile Chromium/WebKit 浏览器回归通过。docs build/audit 与根目录三个 gate 通过。新增历史/附件/结果回流并发用例在 race 下重复 5 次通过。
