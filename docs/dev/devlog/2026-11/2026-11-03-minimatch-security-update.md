# 2026-11-03（升级 Web 开发工具的 minimatch）

- 日期：2026-11-03
- 范围：`web/package.json`、`web/package-lock.json`。
- 做了什么：将 minimatch 的 npm override 放宽到包含 11.0.1 修复版本的范围，按 npm 官方注册表重新生成锁文件。
- 为什么：Dependabot 的安全更新仍受 10.x override 限制，无法升级到修复 glob 展开导致拒绝服务的版本。
- 如何验证：检查 npm 依赖树和安全审计，并运行 Web lint、类型检查、单测、构建和关键路径浏览器测试。
