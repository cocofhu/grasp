---
title: Grasp 核心概念
navTitle: 核心概念
description: 理解 Agent、模型组、运行、人工门禁、沙箱和产物之间的关系。
section: start
order: 30
keywords: [FSM, Agent, Solo, gate, Sandbox, artifact, MCP]
---

## Agent 与模型组 {#agents}

Agent 定义这项工作要用的指令、能力和工具。模型组选择实际执行的编码工具、授权和模型。一个 Agent 能绑定多个组，也能继承默认配置。仓库记录单独指定 Git 授权。见[模型组与智能体](/help/agents-models/)。

## 工作流、运行与人工门禁 {#workflows}

工作流是一张已定义步骤和跳转条件的图。发布后的版本可供启动。一次运行记录输入、活跃节点、对话和产物。人工门禁让运行在指定节点等待决定，随后按连线继续。运行页、待办和产物页都能帮助你追踪这次工作。

## Solo 与沙箱 {#sandboxes}

Solo 是一个可以继续对话的任务会话。Solo 和工作流都在由 Gateway 管理的沙箱里执行。沙箱提供工作目录、终端和可选的 IDE 与应用预览。网关决定镜像或模板，Grasp 管理会话与工作流在何时请求环境。见 [Solo 会话](/guide/solo/)和[沙箱生命周期](/help/sandbox-lifecycle/)。

## 产物与工具 {#artifacts}

产物是工作流节点读写的任务数据，例如需求、计划、测试报告和附件。下游步骤可以引用上游产物。Grasp 的内置工具把适用的工作流、对话和产物操作提供给 Agent。可在「设置 → 集成」查看内置工具目录。
