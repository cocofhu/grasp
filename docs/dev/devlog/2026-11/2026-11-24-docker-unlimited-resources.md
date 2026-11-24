# 2026-11-24（Docker 沙箱默认不限 CPU 与内存）

- 日期：2026-11-24
- 范围：`sandbox-gateway/gateway/internal/config/`、`sandbox-gateway/gateway/config.docker.example.yaml`、`sandbox-gateway/deploy/config/config.local.yaml`、`sandbox-gateway/deploy/docker/README.md`、`CONFIGURATION.md`、`docs/content/{,en/}help/configuration.md`
- 做了什么：Docker 后端 `resources.default` 内置值改为 0，资源解析允许默认后仍为 0 并视为不限制，`docker run` 不再带 `--cpus`/`--memory`；`resources.max` 仍约束显式请求。Docker 校验允许 `default` 为 0，Kubernetes 仍要求正数，E2B 不变。示例配置与文档同步。
- 为什么：单机 Docker 部署希望沙箱直接共享宿主资源互相抢占，原来默认 2 CPU / 4096 MiB 且校验不允许配置为 0，只能改代码。
- 如何验证：`sandbox-gateway/gateway` 下 `go vet` 与 `go test ./...` 全部通过；新增用例覆盖默认不限、显式请求受 `max` 约束、Kubernetes 默认为 0 被拒。
