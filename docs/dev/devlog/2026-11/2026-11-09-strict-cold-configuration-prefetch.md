# 2026-11-09 Kubernetes 冷启动前严格核验当前配置

- 日期：2026-11-09
- 范围：`sandbox-gateway/gateway/internal/driver/cold_bootstrap.go`、`cold_bootstrap_test.go`
- 做了什么：为受支持的 Kubernetes 原环境冷启动生成独立校验脚本。两个当前配置归档须经过限时下载、字节数与 SHA-256 回读、受限 gzip/tar 解析和必要清单校验，才写入平台私有配置目录并启动运行时。拒绝路径越界、链接、重复或冲突条目、加载器钩子和超额展开；工作区文件不参与提取。成功归档按原请求身份缓存在私有目录，后续同授权停启可在临时 URL 过期后完整重验复用；已有缓存不匹配时拒绝启动，不静默替换。
- 为什么：旧版宽松配置注入可能在下载失败后继续启动，也无法保证后续冷启动时临时地址仍有效。旧 Docker 私有根目录不具备同等授权清理证明，本实现仅用于具有新进程私有根目录的 Kubernetes 冷启动协议。
- 如何验证：真实 Bash、隔离 Python、HTTP 下载与缓存重启用例通过 `go test -race ./internal/driver -run '^TestColdBootstrap' -count=3`；失败包、归档链接及路径、坏缓存、缓存身份和权限均不得执行后续引导脚本；`go vet` 与 gateway driver lint 通过。测试私有路径重定位保证无 `/root` 写权限的 CI 也实际执行，不以跳过代替验证。
