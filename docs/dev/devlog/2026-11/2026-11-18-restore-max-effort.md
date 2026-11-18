# 2026-11-18（恢复到模型的默认推理强度）

- 日期：2026-11-18
- 范围：`web/src/components/solo/SoloModelSelect.vue`、`web/src/components/run/VisitModelSelect.vue`、`web/src/lib/solo/`、`web/src/lib/run/`、`web/src/locales/`、`server/internal/services/solo_model_settings.go`
- 做了什么：模型提供 `max` 时，空选择和恢复按钮都使用这一档。模型没有 `max` 时沿用最后一个可用档位；没有可选档位时保留空值。Solo 新会话、工作流启动和 Solo 首轮对空档位的解析采用相同规则。
- 为什么：部分模型在 `max` 之后还提供 `ultra`。以前按列表末项恢复，导致用户点击恢复后进入了 `ultra`。
- 如何验证：浏览器覆盖初始默认值、从低档与 `ultra` 恢复、Fast 开关独立及显式选择的持久化。服务层测试检查下一轮实际应用的档位，也覆盖没有 `max` 的模型。
