# 2026-11-09 部署前只读核验执行身份

- 日期：2026-11-09
- 范围：`server/cmd/storage-admin/`、`server/internal/executionlineage/`、`server/internal/objectstore/control_reader.go`、`server/STORAGE.md`、`CHANGELOG.md`
- 做了什么：新增 `execution-status`，GET 独立控制对象、SELECT 既有 SQL 元数据并再次读取控制身份，返回版本化状态；只有完整匹配的 active 状态退出成功。缺少 SQL 和控制位置不创建目录或数据库；已有 SQL 绑定但控制标记丢失仍明确保留存在证据。LocalControl 增加无写锁、无目录创建的原子信封读取接口；SQLite 使用只读连接。文档补齐显式初始化和先开始、后导入、再完成的恢复顺序。
- 为什么：普通部署与预检查不能因使用维护入口而隐式迁移、写探针或初始化；恢复后的 SQL 也不能自行使旧执行生效。机器结果需要区分空安装、控制数据丢失、恢复中及身份不匹配，供部署自动化保守处理。
- 如何验证：storage-admin、executionlineage、objectstore 全包 race 两轮通过；覆盖已初始化、空安装、旧 schema 无迁移、丢失 marker、缺失 SQL、并发 marker 轮换、只允许 GET、拒绝前缀重叠及 SQLite 拒写；三包 lint 通过。固定源码副本另执行完整服务端及仓库基础检查，结果保存在任务验证记录。
