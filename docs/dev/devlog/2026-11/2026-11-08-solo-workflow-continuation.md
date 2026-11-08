# 2026-11-08 Solo 工作流管理与持久化基础

- 日期：2026-11-08
- 范围：`server/internal/{objectstore,payloadstore,filestore,blob,config,services,engine,handlers,models,router,sandbox}`、`server/cmd/server`、`web/src/{components/solo,components/workflow,views/SettingsView.vue,lib,locales}`。
- 做了什么：接入 S3 / COS 长正文及附件存储、位置登记与校验后的旧文件归档；统一 Web / API / PM / Solo 工作流启动校验、功能开关、持久化启动幂等及容量控制；Solo 工作流任务卡复用现有澄清和审批聊天；状态变化与结果 outbox 同事务提交；新增保留工作区的 Solo 环境启停、数据库操作租约、计算配额预留及按 Run 的安全边界暂停。
- 为什么：将 Solo 作为持续对话与任务管理的基础，避免历史与附件依赖单个 server 本地目录、浏览器离线丢结果、手动停机被自动恢复，以及重复启动造成重复任务。保留已有外部 API / PM 启动渠道，统一受开关约束。
- 如何验证：已完成新存储单元回归、启动并发重试和权限/开关回归、跨服务实例的环境操作租约、手动停止后零唤醒和失败恢复保留、离线结果幂等补投、单 Run 暂停隔离；实际 Kubernetes SeaweedFS 经 Go SDK 验证 9.75 MB 分段上传、旧本地正文/附件归档与清空本地目录后的完整恢复。前端类型检查与相关组件回归已运行。完整模块 gates、聊天优先历史、自动节点补充指令、工作区快照/接手和集成分支验收仍在进行。

- 补充实现：历史 Run 默认聊天路由，原 Solo 仅由其所有者复用；缺失/已删除/私人 origin 使用有权查看的 Run 对话，明确点击后事务创建唯一 reference Solo。原需求、对话链与产物冻结为独立的持久化上下文，简短通知仍使用项目聊天 UI；已补上系统结果进入下一轮模型输入及 get_history 的路径。首页保留角色/仓库/模型/草稿/@引用/附件/品牌样式，只提供 Solo，工作流定义留在设置。删除了对应已移除的首页工作流 rail 行为测试，改为手动启动、多任务与开关的浏览器验收。
- 补充修复：附件输入归档改为返回新值，避免并发启动重试修改请求体和幂等指纹；带附件并发重试 5 轮 race 通过。聊天优先入口、私人 origin、删除 fallback、跨服务并发创建、结果上下文回流及旧详情深链已回归。文件快照/代码导入、自动节点邮箱、流式持久化与分页、完整恢复和最终分支集成验收仍未完成。

- 本轮门禁：server 全量测试、lint/vet、配置文档检查与 core 90.7% 通过；web 6298 项单测（Lines 91.6%）、类型/lint/build、371 项 desktop/mobile Chromium/WebKit 浏览器回归通过。docs build/audit 与根目录三个 gate 通过。新增历史/附件/结果回流并发用例在 race 下重复 5 次通过。

- 文件保护补充：Run 调试租约到期改为保留身份和磁盘的停机，provider 停机失败持久化错误并重试；启动观察保留停止/不可用 Run、未归属资源和 Solo 恢复关联。每次创建以独立 placeholder 记录，登记真实 gateway 身份时关联原 placeholder 和 iteration；运行失败重试也通过生产 registry 退休保留旧工作区。暂停 Run 继续持有 MCP 和直到结束的租约。历史上下文使用固定字段次序，避免大型产物挤掉原需求；完整原文仍可通过 get_history 读取。完整快照、接手、分页和邮箱继续作为未完成工作保留，不能以停机保护代替快照实现。

- 文件保护回归：server 全量测试通过，lint 0 issues、vet/configdoc、core 90.7%、根 gate、docs build/audit 通过；新停机失败重试/丢失资源保留/每次尝试工作区/大型产物上下文用例通过，关键用例 race 重复 5 次通过。完整免费 Agent 矩阵（8 个工具、全部有序切换，付费调用 0）通过：5 个可配置工具运行原生 CLI，Cursor/Trae/Kimi Code 仅声明平台协议 fixture；不宣称这三种 vendor inference 已验证。

- 快照格式基础：新增有解压额度和逐文件哈希清单的规范化 tar.gz，去除所有者、扩展属性与特权位，保留普通权限、可执行位与合法链接；拒绝越界/影射路径、设备、损坏流和尾部隐藏数据。恢复只创建授权根目录下的新目标，重新校验清单，独占创建文件并同步数据，失败清理新目标，不覆盖已有工作区。原生格式/文件恢复安全回归已通过；尚未接入快照作业、provider 导出/恢复或接手 UI，不能宣称完整 P7 已完成。
- 网关补充：孤立扫描改为报告并保留未归属资源，数据库缺失记录不再触发 driver.Destroy/PVC 删除。年轻工作负载仍经过年龄保护；日志计数改为 retained。网关全量测试、lint/vet 与 91.4% 覆盖门禁通过；实际新版网关部署仍待本轮确认。

- 整轮操作租约：模型准备、原生聊天与回复入库共享沙箱租约，嵌套调用复用同一上下文；续期失败或 owner/generation 改变时取消执行，最终回复与上下文标记在同一事务中检查所有权和有效期，旧实例不得覆盖后继状态。用户取消仍可在自身租约有效时保存停止结果。恢复队列只因明确关闭/删除会话清除，临时错误、数据库读失败和活动租约均保留；删除 provider 失败也保留 SQL 关联。直接网关发现优先 v2，仅 404 时兼容 v1，避免漏掉 E2B 原生暂停能力。跨服务整轮互斥、失去所有权、取消后保存与恢复队列回归通过；持久化 FIFO/流式日志仍待实施。
- 租约回归：server 全量测试、lint 0 issues、vet/configdoc、core 90.7%、关键恢复/并发用例 race 重复 5 次及 docs build/audit 通过。原生免费 Agent 矩阵 8 工具 / 84 用例、付费调用 0 通过，平台 fixture 边界同前。最终事务使用 SELECT FOR UPDATE 校验，兼容 MySQL 的“无变化 UPDATE 不计 affected rows”；最终全量/关键 race 已重跑通过。

- Kubernetes 实测修复：快速停止/启动仍可能连回旧 Pod，因为原 Stop 只修改 desired replicas。现在等待 deployment observed generation/零实际副本及目标非终态 Pod 退出，并让只读状态观察在收敛前保持 pending；读取失败、取消或后续启动覆盖意图时不返回停止成功。单测覆盖 controller lag、Pod 尚存、已完成/其他任务 Pod、取消、读取错误与 superseded。真实 E2B 已在专用测试 guest 验证两轮 RAM 随机值/PID/进程启动时间、5 个 dirty/untracked/多仓/非 Git 文件与 Git 状态保留；Kubernetes 修复的实际部署/复测仍待完成。
- 停止确认门禁：gateway 全量测试、lint 0 issues、vet、91.4% 模块覆盖率、停止确认用例 race 重复 5 次及 docs build/audit 通过。严格 JSON 探针排除空响应/执行错误；修复前 Kubernetes 的真实快速启停仍观察到存活进程，不把该轮标为通过。
