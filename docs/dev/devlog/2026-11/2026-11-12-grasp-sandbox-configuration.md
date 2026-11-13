# 2026-11-12 沙箱后端配置隔离与 Grasp 跟随网关默认值

- 日期：2026-11-12
- 范围：根目录配置总览、README、GATEWAY、Compose/start.sh；server 配置、镜像选择、内存设置及生成文档；gateway 配置加载、资源解析、E2B 客户端、API 与示例；E2B 模板构建工具；web 设置页；中英文帮助。
- 做了什么：Docker、Kubernetes、E2B 分别提供配置示例，每个 gateway 只接受当前后端的字段与环境变量。将 Docker/K8s 的 image、profiles 和 resources.default/max 放回各自后端，ports 独立为公共字段。E2B 用具名 templates，单模板自动选择、多模板显式 defaultTemplate，自动发布同名 profile；移除 domain 隐式云地址，要求 apiURL。模板构建用 --name 和 gatewayTemplates 输出。Grasp 无显式覆盖时不传 image/resources；内存 0 表示跟随网关，设置页可清除已有覆盖。选模板后再补齐资源，重装保持原模板；不匹配错误显示期望和请求规格。更新启动入口、API 文档、成对示例、完整默认值表、生成配置表及帮助页。
- 为什么：解决 #1207 中模板名称和资源需要在 Grasp、gateway 默认 image、模板列表、profile 反复复制的问题；让 self-hosted E2B 的地址含义明确，避免 Docker 资源隐藏在 Kubernetes 配置里。按本次需求不保留旧配置兼容层，升级需使用新的后端文件和对应程序；已有设置页内存覆盖需主动改回 0。
- 如何验证：gateway lint/vet/完整覆盖率门禁通过（91.4%）；server lint/vet/生成配置文档校验、全量单测、核心覆盖率门禁（90.4%）与指定 runtime 回归通过；sandbox smoke、lint/vet/覆盖率门禁通过（96.6%）。E2B 构建工具 11 项离线测试和五个 Compose 配置静态校验通过。docs 构建与依赖审计、仓库 gate 通过。前端 lint、类型检查、6,390 项单测（行覆盖率 91.9%）与构建通过；367 项浏览器回归与完整 Agent 切换矩阵（8 个工具、56 个有向切换配对、83 个回合）通过，原生 CLI/平台 fixture 边界按矩阵标注；K8s 部署清单中的 gateway YAML 由加载测试直接验证；本次没有连接真实 E2B/K8s provider 或执行部署。
