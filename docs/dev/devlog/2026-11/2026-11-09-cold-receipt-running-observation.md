# 2026-11-09 冷启动收据区分受理与当前运行观测

- 日期：2026-11-09
- 范围：`sandbox-gateway/gateway/internal/driver/kubernetes/cold_start.go` 及回归。
- 做了什么：原冷启动派发受理后，GET 仍保留同一 nonce、配置摘要和 native UID；Deployment 尚未就绪或后来已停止时返回 unknown，不重新启动。只有当前控制器状态为 running 才提供 started 观测，Manager 后续仍独立等待真实服务就绪。
- 为什么：一次成功受理不能被描述为任意时刻仍在运行，否则停止后的重查会长时间等待一个不会自动启动的环境。
- 如何验证：原 Start ACK 丢失后显式 GET、控制器尚未就绪、实际 ready 状态和后续停止的只读回归；原资源不变，GET 不产生任何 provider write。Gateway focused race 与原 90% 门禁。
