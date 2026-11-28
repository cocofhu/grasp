# 2026-11-27（官网与帮助中心重做）

- **日期** 2026-11-27
- **范围** `docs/site/`、`docs/scripts/`、`docs/content/`、`docs/reference/`、`docs/README.md`、`CONFIGURATION.md`、`CHANGELOG.md`
- **做了什么** 统一中英文官网与帮助中心，首页用应用同款黑白幽灵标志、暖白与炭黑的版式，以及朱红的状态和进度标记。真实工作流画布支持跟随滚动、拖动或播放的镜头，步骤中的示例产物可打开查看。帮助中心补齐首次运行、Solo、工作流、模型组、仓库、常规运行设置和部署排查。配置默认值来源于仓库参考，查找可直达字段。构建校验站内链接、锚点、资产和语言对应关系，使用 HTML 属性解码器保留锚点中的字面实体。
- **为什么** 现有文档从安装摘要直接跳到源码，读者难以在产品操作和部署配置之间定位。旧摘要的沙箱回收规则已落后于主线。按操作流程和生效时机更新说明。
- **设计参考** 参考获奖案例将领域工件做成交互主体的办法，以及 Resend 对首页叙事顺序的说明。将主要动效集中在同一条示例工作流的画布取景和任务状态。 [Heron AI 获奖记录](https://www.cssdesignawards.com/sites/heron-ai/50037/) · [Resend 设计手册](https://resend.com/handbook/marketing/how-we-think-about-our-website) · [Carbon 动效指南](https://www.carbondesignsystem.com/building-blocks/foundations/motion/overview)
- **如何验证** 使用 Node 24 安装锁定依赖，运行文档构建、官方 npm 高危审计、服务端配置生成校验、品牌检查、actionlint、error 级 shellcheck 及 CI 工具下载与文档发布回归测试。两种发布路径均构建并校验 36 个页面，并回归校验包含 HTML 实体的锚点。Chromium 和 WebKit 走查 320–1920 像素布局、连续镜头、拖动、播放、产物查看、减少动态效果、键盘搜索及配置字段跳转。确认生成录屏和截图。
