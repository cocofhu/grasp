# 2026-11-09 原 Solo 环境在派发前核对冷配置能力

- 日期：2026-11-09
- 范围：`server/internal/services/solo_environment_review_resume.go`、`solo_environment_review_resume_test.go`
- 做了什么：对已离线核对的原 Solo 环境，在记录冷启动派发身份和准备配置前调用只读能力检查。能力不支持返回明确的 `environment_cold_configuration_unsupported`；检查读取失败直接保留原错误，均不产生派发收据，也不授予原环境新的授权指纹。
- 为什么：管理器具有通用协议方法，不表示每个底层 provider 能安全替换旧授权。明确尚未派发的情况不应被记录为“结果未知”。
- 如何验证：服务定向回归在 race 下重复三次通过，核对拒绝时原资源与文件保留、授权指纹为空、冷启动调用为零，且数据库派发字段保持为空。真实 provider 能力分类由管理器协议用例覆盖。
