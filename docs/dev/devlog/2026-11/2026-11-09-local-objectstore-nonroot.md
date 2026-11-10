# 2026-11-09 本地 SeaweedFS 默认使用非 root 身份

- 日期：2026-11-09
- 范围：`scripts/local-objectstore.sh`、`scripts/test-local-objectstore.py`、`docker-compose.yml`、`compose.release.yaml`、`.env.example`、中英文配置帮助及 `CHANGELOG.md`。
- 做了什么：两个 Compose 定义默认使用 `10001:10001`。启动脚本对普通用户沿用宿主 UID/GID，对 root 沿用已存在的非 root 数据属主，新数据及旧 root 布局使用非特权身份；只调整私有数据与 IAM 挂载属主，客户端凭据保持宿主用户私有。拒绝无效身份和目录软链接，迁移属主不跟随数据中的软链接。
- 为什么：对象存储不需要容器 root。原本沿用宿主身份会在 root 启动时继承不必要权限；单改容器 UID 又会使私有 IAM 配置不可读，因此必须同步处理挂载属主且保留原密钥和数据。
- 如何验证：11 项配置/权限回归通过，覆盖旧 root 数据迁移、并发首次启动、软链接目标不变和凭据不再生成。实际公开 SeaweedFS 镜像在默认非 root 身份下通过发布栈写入、源码栈重建后读取，两个对象 SHA256 一致，包含 11,050,000 字节分段上传，匿名及其他 bucket 访问均为 403；临时容器、网络和卷已清理。根门槛及文档构建/审计通过。
