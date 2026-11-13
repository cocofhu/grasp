# 2026-11-13（E2B 模板构建支持显式磁盘可用空间申请）

- 日期：2026-11-13
- 范围：`sandbox-gateway/deploy/e2b/prepare-template.py`、同目录测试和 README、`CHANGELOG.md`。
- 做了什么：新增可选的 `--min-free-disk-mb`，将显式申请原样传给固定版本 SDK，并在 dry-run 与构建报告中记录。未指定时保持 provider 默认策略；允许显式零，不允许负数或超出 API int32 范围。保留 `--minimum-workspace-gib` 的实际容量探针与失败清理。
- 为什么：更大的工作区要求此前只能被检查，没有显式参数可向 provider 申请构建后的空间增长。CPU/内存配置正确也不能保证工作区容量满足要求；申请成功不能替代实际 guest 验收。
- 如何验证：13 项离线模板测试通过，覆盖 8 核/8192 MiB 构建参数、磁盘申请透传、范围校验、provider 未提供所需容量时失败并删除探针。always-on 四项 CI 检查、文档构建及高/严重级依赖审计通过。离线测试不代表 provider 已支持扩容或真机构建成功。
