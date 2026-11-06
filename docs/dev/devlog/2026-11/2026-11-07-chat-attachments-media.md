# 2026-11-07 聊天附件拖拽与多媒体预览

- 日期：2026-11-07
- 范围：`web/src/components/{chat,run,pm,agent,solo,ui}/`、`web/src/views/`、`web/src/lib/{shared,composables,inbox,pm,run,icons}/`、`web/src/locales/`、`web/src/styles/icon-motion.css`、`web/e2e/`、`web/package.json`、`server/internal/{blob,gateshare,handlers,router}/`、`docs/content/{en/,}guide/quick-start.md`、`CHANGELOG.md`。
- 做了什么：统一文件拖入区域，支持多文件、高亮反馈、离开及取消恢复，保留选择和粘贴入口；统一文件读取状态、失败提示和移除行为，显示名称、类型、大小，读取完成后才能发送。图片提供下载，音视频使用原生控件播放并保留下载和失败兜底；为图片、音频、视频、其他文件展示类型图标。文件读取支持 Range/HEAD 和固定 MIME 回退，分享及嵌入继续使用原有凭据和对话附件范围；下载文件使用受限 CSP。分享元数据保留不透明索引，不输出内部引用或文件内容。
- 为什么：完成 #1117 与 #1116，减少添加附件的操作步骤，并使历史和待发送附件可直接识别、查看或播放。
- 如何验证：运行仓库通用检查、server lint/vet/配置文档校验/全量测试/90% 覆盖率与 runtime 回归、web lint/类型检查/全量覆盖率/构建、CI 浏览器关键路径及附件专项回归、docs 构建与审计；浏览器使用有效 WAV 和 WebM 验证播放、跳转、下载、失败兜底、各聊天入口和移动端布局，接口测试验证 Range 及鉴权和对话范围。
