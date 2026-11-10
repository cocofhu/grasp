# 2026-11-09 镜像构建中的全部锁定包跟随所选 registry

- 日期：2026-11-09
- 范围：`server/Dockerfile`
- 做了什么：官方源与备用源两次 `npm ci` 均显式设置 `replace-registry-host=always`，使 lockfile 中锁定到另一 registry 的下载地址也跟随本次选择；版本与 integrity 继续使用原 lockfile。
- 为什么：现有 lockfile 同时包含官方源与镜像源，单独设置 registry 不会替换其中固定镜像地址，仍可能命中已故障的镜像服务。
- 如何验证：用真实 npm、独立空缓存、写死镜像地址及 integrity 的最小包夹具完成安装，实际观察本地受控 registry 的 tarball GET 和安装后版本，原 lockfile 未改；完整镜像以固定提交构建另行核验。
