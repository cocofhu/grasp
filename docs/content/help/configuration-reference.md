---
title: 配置项索引
description: 按配置名查找 Grasp 环境变量、YAML 键、默认值与用途。
section: deploy
order: 20
reference: server
keywords: [环境变量, YAML, GRASP, SBGW, 默认值, 配置项, precedence, 端口]
---

[部署与配置](/help/configuration/)说明配置应放在哪里、怎么应用并验证。[常规设置](/help/settings/)覆盖界面可改的字段，包括只有 YAML 和界面入口的沙箱容量与空闲回收。

## Gateway、模板与沙箱内部选项 {#gateway}

按后端浏览[网关参数索引](/help/gateway-reference/)。使用[完整部署参数参考](https://github.com/cocofhu/grasp/blob/main/CONFIGURATION.md)查看 Docker、Kubernetes、E2B、Gateway 和沙箱内部字段。用[网关配置指南](/help/gateway/)选择连接方式和预览策略。

## Grasp 服务端字段 {#platform}

这张表从服务端生成的配置参考引入，包含环境变量、YAML 路径、类型、默认值、敏感性和用途。发布栈、设置保存值和 Gateway 默认值可能影响实际值，可根据[覆盖顺序](/help/configuration/#precedence)判断。
