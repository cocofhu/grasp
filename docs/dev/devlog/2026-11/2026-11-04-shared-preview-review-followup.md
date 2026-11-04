# 2026-11-04（分享主会话停止操作按轮次校验）

- 日期：2026-11-04。
- 范围：`server/internal/{chatsession,services,engine,handlers}` 的预览取消、队列及快照代码与用例，`web/src/components/run/SharedAppPreviewChat*`、`web/src/lib/inbox/appPreviewShare*`、`web/src/views/EmbedNodeImageView*`、`web/src/components/pm/PmLeaderChat.test.ts`、中英文语言包、预览浏览器回归。
- 做了什么：#1106 先合并到 main，#1107 在其上 rebase 并保留全部 CHANGELOG 条目。主会话快照和尾部帧携带平台 FIFO 的活动轮次 ID；Stop 必须提交该 ID，底层在同一转移锁内比较、清队列及调用运行时取消，旧请求返回 409 且不改变新轮次或队列。前端收到轮次变化只刷新历史，等待用户再次操作。Solo／评审清空全部排队消息、澄清保留队列的行为保持与原聊天一致，并明确写入两处分享提示。图片 iframe 使用 parentOrigin()，Firefox 缺少 ancestorOrigins 时记住来自真实父窗口的消息 origin；重连健康时间提取为具名常量。
- 为什么：访客的旧视图不能使 Stop 误取消下一条排队消息；只读队列仍受原 Stop 策略影响，需要在分享和参与提示中准确说明。图片关闭消息应尽量定向发送给实际父窗口。
- 测试稳定性：全量覆盖率运行暴露 PM 切换线程加载态用例依赖 30ms 真实定时器，可能在断言前结束加载；改用显式完成的 Promise，先验证加载态再释放数据，产品代码不变。
- 如何验证：新增底层队列、Solo 服务、真实工作流接口测试，覆盖旧 ID、匹配 ID、下一轮自动开始、队列保留／清空及权限边界；组件覆盖 409 后刷新而不自动重试停止，Firefox 图片关闭消息覆盖父窗口与 origin 隔离。相关 35 项浏览器回归通过，其中真实分享页面验证旧 Stop 被拒后展示新轮次，再次点击才取消。独立会话既有真实 WS 用例覆盖首字前／部分输出后取消与继续发送；Desynced 重建与中断历史保留另经代码核对，不声称该用例执行了未确认取消后的完整重建。Go 重点包 race 用例、完整 lint／vet／配置文档／测试／90.5% 核心覆盖率及运行时检查通过；Web 安装、lint、类型检查、全量 6246 项单测（行覆盖率 91.86%）和生产构建通过；文档构建／审计及 actionlint／shellcheck 通过，三个注入脚本哈希一致。PR CI 随更新执行。
