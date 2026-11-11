# 2026-11-11 本地沙箱镜像 Docker CE 源回退

- 日期：2026-11-11（写下这条的当天，不是排期）
- 范围：`sandbox-gateway/sandbox/Dockerfile`、`start.sh`、`.env.example`、`CHANGELOG.md`。
- 做了什么：沙箱镜像安装 Docker CE 时不再只拉清华 GPG/apt。国内源失败（含 403）后依次尝试阿里云、中科大，再回退 `download.docker.com`；同 URL 先默认 curl，失败再强制 IPv4。`./start.sh dev` 与 `./start.sh sandbox` 把 `USE_CN_MIRROR` 传进 build-arg，国内源整次构建失败时再以 `false` 重试一次。
- 为什么：清华 docker-ce 对部分云厂商 IP 和 Ubuntu 22.04 默认 curl 返回 403，GPG 下载没有重试也没有备用源，`./start.sh dev` 第一次构建本地沙箱镜像就会直接失败。
- 如何验证：在与失败构建相同的 `--network=host` Ubuntu 22.04 容器里跑新的密钥/仓库选择逻辑，清华默认 curl 403 后 IPv4 或下一源拿到 3817 字节密钥；`bash -n start.sh` 与 error-level shellcheck 通过。
