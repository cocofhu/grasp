# 2026-11-09 Kubernetes 保留环境的控制器观测与预览端口

- 日期：2026-11-09
- 范围：`sandbox-gateway/gateway/internal/driver/kubernetes/{cold_start,cold_controller,kubernetes,publish_surface}*.go`
- 做了什么：冷配置认领原 Deployment 后，等待控制器确认该停止代次；比较原 UID、generation、完整 Spec 与原冷配置意图，最终使用最新 resourceVersion 做启动 CAS。控制器正常的 status 更新不再被当作换环境。真正的 Spec、意图、资源身份变化仍拒绝；Spec 曾被改动再恢复也因 generation 变化而拒绝。观察不到停止或最后 CAS 冲突仍保留 unknown，GET 不会重放启动。端口整理保留该环境已经发布的应用端口，并从相同受管环境 Secret 的预览配置补回平台预览入口；内部端口仍只留在 ClusterIP，不返回 LoadBalancer。
- 为什么：实际保留环境验收复现两个问题。冷配置 annotation 更新推进 Deployment generation，控制器确认 observedGeneration 时又推进 resourceVersion，原来要求旧 RV 不变与等待新代次观测互相矛盾。Gateway 重启后的全局端口整理还会删除创建时加入的预览入口，导致应用和 injector 均正常而产品预览地址返回 502。
- 如何验证：client-go 回归显式模拟 annotation 推进 generation、延迟 status 确认及 RV 推进，覆盖 Spec 改动再恢复、未确认取消、最终 CAS 冲突与 unknown 只读；端口回归覆盖丢失入口恢复、动态应用端口跨 Get/Start 保留、内部端口不公开和不可信配置零修改。原始顺序覆盖的反证测试失败。实际 Kubernetes 正向复验仍由新分支 Gateway 部署后执行，未把 client-go 测试算作真实 provider 成功；原先 unknown 的环境和收据保留。
