---
title: 常见问题与排查
navTitle: 常见问题
description: 按失败的阶段排查凭据、克隆、沙箱容量、预览和配置未生效。
section: support
order: 10
keywords: [401, 403, 404, clone failed, timeout, capacity, OOM, failed, 找不到模型, 回收, 排错]
---

## 先找到失败的阶段 {#diagnose}

打开运行详情、待办或 Solo 的沙箱信息。记录失败的阶段、报错原文、Agent 与模型组、网关后端、是否新建环境。查看日志时保留错误上下文，隐藏密钥和凭据。根据第一个失败的阶段定位。

| 现象 | 先检查 | 下一步 |
| --- | --- | --- |
| 模型组不可用、模型为空或 API 返回 401 | 凭据是否有效、API 地址与协议、Agent 绑定 | 在凭据页刷新账号或重选组，用一条短指令验证。[模型组](/help/agents-models/) |
| 仓库克隆返回 401、403 或找不到分支 | URL、Host、协议和仓库权限 | 检查保存的凭据，再用已授权的分支重试。[仓库与 Git](/help/repositories/) |
| 一直在准备沙箱 | 首次拉镜像、模板数据、启动日志和网关健康 | 查看日志第一个失败步骤，核对就绪超时和模板规格。[网关](/help/gateway/) |
| 容量不足或排队 | 当前占用与上限、是否有待释放的环境 | 停止不用的会话或调大容量。Docker/Kubernetes 会持续占用直到显式释放。[常规设置](/help/settings/#sandbox-capacity) |
| 预览入口不可用 | 应用是否监听、是否已登记预览、后端传输策略 | 用终端验证服务，再查预览暴露规则。[预览配置](/help/gateway/#preview) |
| 设置保存后结果不符 | 来源、锁定状态、修改的是运行时还是启动配置 | 对照覆盖规则和生效时机，必要时用新环境验证。[覆盖顺序](/help/configuration/#precedence) |
| 迁移后附件无法显示 | 数据库与附件目录的备份版本 | 从同一恢复集恢复并核对自定义附件存储。[备份](/guide/quick-start/#backup) |

## Agent 回合卡住或结束 {#timeouts}

先确认是等待沙箱、Agent 无动作、节点总时限还是在等待人工。运行详情的状态与失败原因决定下一步。任务持续产出或运行构建时，CPU、I/O 活动会影响 Agent 无动作判断。若回合已终止，保存需要的日志和产物后，在可用环境重新运行最小步骤确认。各时限见[部署配置](/help/configuration/#time-limits)。

## 看懂空闲回收与恢复 {#restore}

E2B 可以回收空闲环境并在再次使用时恢复。先确认环境是否显示已停止、保存中或正在恢复。若需要保持开发服务器，核对「保持运行」和本地 IDE 连接状态。Docker 和 Kubernetes 的自动空闲回收规则不同，见[生命周期对照](/help/sandbox-lifecycle/)。

提供问题反馈时，带上 Grasp 与 Gateway 的版本、后端类型、重现步骤和最早的错误信息。 [提交问题](https://github.com/cocofhu/grasp/issues/new)
