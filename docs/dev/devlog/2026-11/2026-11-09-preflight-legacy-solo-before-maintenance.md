# 2026-11-09 旧 Solo 环境核对先确认实际冷配置能力

- 日期：2026-11-09
- 范围：`server/cmd/storage-admin/solo_review.go`、`solo_review_test.go`、`server/UPGRADING.md`
- 做了什么：离线原 Solo 环境核对在进入 SQL 维护前，先只读解析原 Sandbox 与 Solo、所有者、Agent 绑定并核对网关实际 Kubernetes 冷配置能力。进入维护后重核绑定，在停止前再次核对能力。不支持的 Docker、E2B 或旧网关不会先停机，也不会因此留下 SQL 维护。
- 为什么：只有保盘 Stop 不足以证明旧授权能在启动前完整替换。不能先改变不受支持的原环境，再将后续恢复标为不支持。
- 如何验证：CLI 回归覆盖三种不支持场景的单次能力 GET、零 Stop/Start/Delete、完整维护行不变、原绑定与隔离收据不变；能力或绑定在预检后变化时保持维护但不派发 Stop；原丢失停止响应仍只读取核对，不重发。
