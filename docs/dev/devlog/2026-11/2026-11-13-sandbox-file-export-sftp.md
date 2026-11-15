# 2026-11-13（沙箱文件导出改用 SFTP，修复 send_file 总是失败）

- **日期**：2026-11-13
- **范围**：`server/internal/sandbox/file_export.go`、`server/internal/sandbox/file_export_test.go`、`server/go.mod`、`server/go.sum`。
- **做了什么**：远端沙箱的 `OpenArtifactFile` 不再通过 SSH 执行 `python3 -c <脚本> <工作区> <路径>`，改为在同一条 SSH 连接上打开 SFTP 子系统，按路径直接读文件。导出范围不变：路径经 `SSH_FXP_REALPATH` 解析（OpenSSH 的 sftp-server 会解析软链接）后必须落在工作区或 `/tmp` 内，打开后用 `fstat` 确认是普通文件。本地 worktree 模式（`os.OpenRoot`）不变。新增依赖 `github.com/pkg/sftp`。
- **为什么**：`send_file` 和 `upload_file_artifact` 都走 `OpenArtifactFile`。旧实现把整段 Python 脚本作为 `argv[2]` 交给 `newSafeCmd`，脚本含空格、分号、括号，永远过不了 `shellArgPattern`，两个工具在远端沙箱上每次都返回 `argv[2]: shell argument contains disallowed characters`。即使把脚本改走 stdin，路径本身仍要过同一白名单，含中文、空格、括号的文件名依然无法导出；SFTP 不经过远端 shell，两个问题一起消失，也不再依赖沙箱内有 Python。
- **如何验证**：新增 `TestOpenSFTPExportConfinesGuestPaths`，通过管道连接真实的 OpenSSH `sftp-server`（未安装时跳过），覆盖相对 / 绝对路径、中文空格括号文件名、工作区内软链接可读，以及绝对路径越界、`..` 越界、软链接越界、目录、不存在的文件被拒绝；去掉根目录检查后该测试失败。另在沙箱内用真实 sshd 走 `Sandbox.OpenArtifactFile` 读出 36 MB 的 `完整 压缩包(1).zip`，SHA-256 一致，`/etc/passwd`、`../../etc/passwd` 和目录被拒绝（临时测试，未提交）。`golangci-lint`（v2.12）`./internal/sandbox/...` 0 issues，`go vet`、`gen-configdoc -check`、`go test ./...` 通过。
