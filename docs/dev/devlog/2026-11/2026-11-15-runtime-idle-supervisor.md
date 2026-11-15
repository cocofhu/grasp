# 2026-11-15 空闲探测识别运行时启动父进程

- 日期：2026-11-15
- 范围：`server/internal/sandbox/idle_activity.go`、对应测试、`CHANGELOG.md`
- 做了什么：空闲进程探测按已安装运行时的完整路径识别常驻 `startup.sh`，遵循 `GRASP_RUNTIME_ROOT`；同样识别 gateway 安装的 E2B bootstrap/relay 和预览守护脚本，只额外忽略预览脚本自己的 `sleep 2/5` 轮询，并将预览、services、bootstrap 的命令全文子串豁免改为完整安装路径匹配。用户同名脚本、仅在命令文本中提到运行时脚本的命令，以及这些父进程的其他子任务仍按活动处理。
- 为什么：启动脚本在工作区以前台方式等待 code-server；E2B bootstrap/relay 与预览轮询也持续存活。实机只读检查发现它们会被当成工作区任务或 shell 子任务，即使没有 Agent 回合也不会触发空闲回收。
- 如何验证：运行实际 Python 探测脚本的确定性进程表回归，覆盖默认/自定义运行时目录、E2B 转发及预览轮询、用户同名与相似目录脚本、子任务及命令文本误匹配。修复前只有基础进程的场景误报忙；修复后报告空闲。另按仓库规则执行 server、通用 CI 和文档门禁；没有为验证而暂停或修改业务沙箱。

只读实机对比中，仅有平台进程的沙箱从旧探测的 `busy=true` 变为 `false`；活跃 Agent 回合和后台用户 Python 所在沙箱都保持 `true`。当时线上运行时仍只提供旧 `/api/runtime/busy`，因此两份真实探测脚本都仅替换了该端点做对比；这一证据覆盖进程分类，不代替新 `/api/runtime/activity` 的发布验证。
