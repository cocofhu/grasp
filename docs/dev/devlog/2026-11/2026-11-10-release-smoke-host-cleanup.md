# 2026-11-10 官方发布冒烟在 Ubuntu runner 上完成宿主机清理

- 日期：2026-11-10
- 范围：`.github/workflows/release-smoke.yml`
- 做了什么：官方工作流以 `sudo` 执行原有 `release-smoke.sh`，只显式保留三个已固定 digest 的公开镜像变量 `GRASP_IMAGE`、`SANDBOX_GATEWAY_IMAGE`、`SANDBOX_IMAGE`。脚本、功能断言、超时和隔离保护不变，不调整发布 tag 或运行时镜像。
- 为什么：v3.3.0 官方运行 `42735565305` 已通过原生服务、沙箱、产物隔离和清理检查，并产出 `result=passed`；工作流随后在 EXIT trap 删除 `.localdata` 时失败。容器在 bind mount 中创建的目录及数据库属于 root，Ubuntu 托管 runner 的普通用户无法删除。仅为原脚本提供完成这些宿主机清理所需的权限，不保留其余调用者环境。
- 如何验证：核对官方运行日志及产物；确认 `release-smoke.sh` 的 Git blob 与 v3.3.0 的 `121528d4a169d97a4a1e49b0cb15429d04138744` 完全相同，工作流只有这一行调用变化。执行根目录 CI 工具下载回归、actionlint、错误级 shellcheck、离线文档发布回归，以及文档构建和依赖审计；新的官方完整冒烟由维护者在修复引用上单独执行，此处不把原失败工作流记为成功。
