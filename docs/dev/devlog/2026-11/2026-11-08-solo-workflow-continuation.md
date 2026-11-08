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

- 实际 provider 验证：修复固定版本部署后，Docker/Kubernetes/E2B 各完成两次真实启停恢复。5 个 dirty/untracked/多仓/非 Git 文件哈希及两套 Git 状态一致，被动读取未唤醒停止环境；Docker/Kubernetes 的本地内存探针确认连接拒绝，E2B 的 RAM 随机值、PID、启动时间和 boot ID 一致。测试 guest 均独立、无业务仓库 push、付费推理调用 0；不将 provider 文件/进程验证当作完整产品续聊验收。
- 自动节点补充要求：独立持久化邮箱绑定 StateRun、轮次与 UUID 执行代次；同一 SQL 行锁覆盖入队、领取与结算封口，客户端身份去重，原生回合结束后在同一 ACP 会话顺序执行。每次补充清除旧 node_complete 并重验产物/结果；不明派发不自动重发，重试尝试更新代次并将未发要求标记过期，旧 worker 不能关闭新邮箱。抽屉沿用现有样式，区分所有交付状态、保留草稿和提交身份，代次变化需明确选用新目标；分页保留跨代次历史，工作流关闭后仍可管理已有任务。结果 outbox 和独立历史续聊包括明确的 Run 补充要求，不读取私人原 Solo 正文；物理删除 Run 同时清理关联 SQL 邮箱。
- 补充要求初步验证：并发幂等、入队/封口竞争、代次切换与不明派发保留、真实 ACP 协议下同一原生会话/旧结论清除和再次校验、断连及存储确认失败不重派、认证/伪造操作者/关闭开关/隐私边界已回归；关键数据库与协议用例 race 重复 5 次通过。7 项 UI 用例与 5 项人工启动/补充要求浏览器用例通过。完整模块门禁和分支集成仍待本轮验证，P4/P6/P7 不因此标为完成。
- 补充要求门禁：最终 server 全量测试、lint 0 issues、vet/configdoc、core 90.7% 与关键用例 race 重复 5 次通过；web 6305 项单测 / 586 文件、Lines 91.6%、lint/types/build 和 373 项 desktop/mobile Chromium/WebKit 浏览器回归通过，浏览器截图已检查。免费 Agent 矩阵 8 工具 / 84 用例、付费调用 0 通过，专有平台的协议 fixture 边界同前；根 gate 和 docs build/audit 通过。新增终态事务原子封口/回滚、跨重试与节点轮次分页单测通过。docs 生成预览已保留到仓库外，避免 web brand gate 把生成的兼容 locale 注释作为源码扫描。当前分支的产品联合部署验收继续进行，完整总需求尚未完成。
- 真实分支联合验收：固定 `7ddb6a0a` 的 Grasp 分支部署及 E2B 分支部署均通过。真实 OpenCode CLI 在独立 Kubernetes 沙箱中完成自动工作流及补充要求，同一原生会话保留此前回复锚点与工作区文件；原 Solo 同时完成独立对话，工作区标记互不混入，结果回到原 Solo 后的后续原生回复确认收到了补充要求上下文。完成后的同 client 身份重试返回原 applied 收据。模型端点仅为确定性的本地测试服务，CLI 未替换，付费调用 0；这与此前标注的平台 wire fixture 区分。
- 真实 Solo 生命周期补验：上述原生会话经产品 API 停止环境后，跨过 35 秒后台恢复窗口并反复只读历史，状态仍为 stopped；恢复继续使用原 sandbox 身份，非 Git 文件和平台聊天上下文均保留。Kubernetes 明确验证服务冷启动，不宣称进程内存恢复。临时模型路由和测试 guest 在验证后清理；完整快照/接手、代码导入、流式持久化/分页及迁移备份闭包仍未完成。

- 离线存储复制：新增 storage-admin、SQL 维护屏障/运行登记、SQLite WAL 一致快照和原生 MySQL 导出、版本化 JSONL 对象清单、目标独立 GET 哈希/元数据校验及原始 SQL 载荷/嵌套附件引用摘要。备份与位置切换分开，完整校验后事务登记副本并保留原位置；同源父子前缀及 endpoint 别名重叠受到保护。中断保留屏障、同操作续跑重验，异常退出登记仅可按确实终止的具体进程 ID 处理。服务退出先阻止正文/附件迟到发布及删除，再关闭 SQL/移除登记；工具不删除源对象、卷或沙箱。镜像附带 SQLite/S3 运维二进制，MySQL 导出需外部 Oracle 8+ 客户端。真实 SeaweedFS 已完成非空 8 对象 / 11.2 MB 分段载荷的冲突中断、启动拒绝、续跑与空本地目录冷恢复；主密钥/配置/账本/沙箱仍明确要求外部配套备份，不将清单作为 GC 删除授权。
- MySQL 兼容补验发现 WorkflowLaunch.RunID 的裸 uniqueIndex 生成 TEXT 索引，现补显式 size:191；真实 MySQL 8.4 原生导出/导入恢复 57 张表，原始载荷引用和恢复库维护屏障保留。最终 server 全量、vet、lint 0 issues、configdoc、core 90.7%、运行时子集、存储/登记/迟到发布 race 重复 5 次、根 gate、docs build/audit 与因镜像内运维二进制触发的 sandbox 全套门禁通过（core 96.6%）。SeaweedFS 冷恢复在最终代码上重验通过。实际新版应用分支部署仍待确认；完整总需求仍未完成。
- 新增发布交付要求已更新总 Issue：完整功能验收后先发布迁移小版本，再发布删除过渡兼容代码的小版本，清理版拒绝跳过迁移；文档列出升级顺序与整套快照回滚，最后完成内网部署。两个版本与最终部署尚未执行，现有草稿 PR/MR 保持未合并。
