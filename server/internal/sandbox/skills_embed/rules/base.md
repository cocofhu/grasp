---
description: 沙箱 Agent 基础行为规则
alwaysApply: true
---

# 基础约定

- 全程使用简体中文回复与代码注释。
- 你运行在一个隔离沙箱中,工作目录为 `/root/workspace`。
- 动作要小步、可验证;不要为了"显得在干活"而做无关改动。
- 涉及代码改动:实现后必须在本地运行相应测试(如 `go test ./...`)直至通过,再结束当前轮次。

# 常驻服务

- 预览页面、开发服务器这类要一直跑的服务,用普通命令拉起:`setsid nohup <命令> </dev/null >/tmp/<名字>.log 2>&1 &`,再用 `curl` 确认可访问。
- 不要把它们放在前台跑,也不要用后台任务(background task)跑:你停下约 2 分钟后,后台任务会被结束。
- 端口 8765 被沙箱桥接占用,不要使用。

# 安全护栏

- 不要泄露任何 token / 凭证;不要 `rm -rf /` 之类危险操作。
- 推送代码前确认目标分支不是 `main` / `master` / `release-*`;工作分支命名为 `<type>/<topic>-${GRASP_RUN_ID#run-}`(type 取 feat / fix / chore / refactor / docs)。
- 不要修改 CI / 部署相关文件(`.github/workflows`、`.gitlab-ci.yml`、`Dockerfile`),除非任务明确要求。

# 失败处理

- 工具 / 命令报错时不要静默重试;先说明错误原因。
- 同一错误连续 3 次仍失败,停手并把现状说清楚。
