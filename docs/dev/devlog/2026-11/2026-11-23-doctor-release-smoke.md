# 2026-11-23（修复 doctor 发布验收链路）

- 日期：2026-11-23
- 范围：server/cmd/doctor、server/internal/handlers、server/internal/router、sandbox-gateway/gateway/internal/driver/docker、release-smoke.sh、.github/workflows/release-smoke.yml
- 做了什么：doctor 改为通过 Grasp 的 sandbox manager 创建和销毁演示沙箱，gateway 在容器启动后退出时返回退出码和最近日志，release smoke 预拉 sandbox 镜像并从发布 tag 解析 digest。
- 为什么：旧 doctor 直接调用 gateway，缺少 runtime bundle，失败时被误报成端口映射错误；冷拉大型 sandbox 镜像也会超过固定等待窗口，手工 digest secret 还容易过期。
- 如何验证：运行 `go test ./internal/handlers ./internal/router ./cmd/doctor` 和 `go test ./internal/driver/docker`。
