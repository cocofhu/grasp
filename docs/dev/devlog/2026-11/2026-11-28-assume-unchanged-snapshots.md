# 2026-11-28（工作区快照读取标记为 assume-unchanged 的文件）

- 日期：2026-11-28
- 范围：`server/internal/sandbox/workspace_changes.py`、`server/internal/sandbox/workspace_changes_test.go`、`CHANGELOG.md`
- 做了什么：从临时 Git 索引读取标记为 `assume-unchanged` 的路径，并在写入快照前清除临时索引里的该标记。
- 为什么：Solo 和工作流审阅需要显示磁盘上的真实改动，同时保留用户的 Git 索引。
- 如何验证：工作区快照回归包含文件名带制表符和换行的等长修改，并比较捕获前后的真实索引字节。另用普通索引、拆分索引和稀疏索引复核。
