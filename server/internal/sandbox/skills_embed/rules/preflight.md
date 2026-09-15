---
description: 环境确认(preflight)节点行为
alwaysApply: false
---

# 环境确认 (preflight)

本节点是**交互式 ReAct**:对照计划/仓库/已有运行变量,确认下游实现或测试所需的环境项。

## 推断缺口

- 先读计划(`get_plan`)、仓库与已有 vars,推断需要的环境项(地址、账号、密码、密钥、端口、回调 URL 等)。
- **无缺口**:不要为问而问。直接 `set_preflight(confirmed=true, fields=[])` 再 `node_complete`。
- **有缺口**:用选择题或表单采集,尽量在沙箱核验后再写入清单。

## 采集

- 有限选项 → `ask_question`(与澄清节点相同)。
- 需用户键入 → `ask_form`:`fields[].name`+`label` 必填;`type` 仅 `text|url`(默认 text)。**密码也用 text 明文**,无 password 类型、无 secret 标记。
- `why`/`reason` 写清计划缺口,界面悬停展示;不要依赖星号或内部 name。
- 每次 `ask_question` / `ask_form` 后**立即结束本轮**,等待用户。
- **表单提交只进入对话,不能代替 `set_preflight`。**

## 核验

- 优先在沙箱探测(连通、登录、读配置等),`verification=sandbox_probe`。
- 无法探测时请用户当面确认,`verification=user_attested`,并在 `notes` 说明。
- 可标 `source`: form|choice|chat。

## 唯一交付:set_preflight

- 写入 `preflight.json`:**summary** 非空、**confirmed=true**、**unresolved 为空**、`fields[]` 每项 **name+value 明文**(可空数组)。
- 禁止用 `write_artifact` 伪造 `preflight.json`。
- 写完后调用 `node_complete`。本节点**隐藏「确认并流转」**,由 Agent 自行结束。
- 完成后平台把每个 field 的 name→value **明文**写入运行 vars;**不改 SandboxEnv**。
