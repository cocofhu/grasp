# 2026-11-12 E2B 模板直接使用环境镜像中的文件

- 日期：2026-11-12
- 范围：`sandbox-gateway/deploy/e2b/prepare-template.py`、`sandbox-gateway/deploy/e2b/test_prepare_template.py`、`sandbox-gateway/deploy/e2b/README.md`、`CHANGELOG.md`
- 做了什么：移除模板的两次 COPY，直接从固定 digest 的环境镜像继承 bootstrap 和 IMAGE_LEVEL；保留构建和私有 guest 探针中的摘要、镜像级别与依赖校验。补充镜像原文件不匹配时拒绝且不覆盖的执行回归，并更新操作说明。
- 为什么：环境 Dockerfile 已包含这两个文件，重复 COPY 会额外依赖 provider 的构建文件上传接口，并可能覆盖镜像原有的版本标记。模板准备曾在该上传环节以 FileUploadException 失败；使用镜像原文件即可消除这条额外上传路径。
- 如何验证：11 项离线模板准备测试通过；使用原生 E2B 2.49.1 SDK，将上传接口设置为失败，确认修改前在上传链接请求处中断，修改后零上传调用并到达构建触发步骤（provider 请求均由本地替身隔离）；CI 工具下载、actionlint、error-level shellcheck、文档发布传输与品牌检查通过；docs 安装、构建及 high/critical audit 通过。真实 provider 构建尚未重跑，须由包含修复的新流水线验证。
