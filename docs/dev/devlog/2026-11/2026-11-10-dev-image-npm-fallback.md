# 开发镜像 npm 安装回退官方源

- **日期**：2026-11-10
- **范围**：`web/npm-ci.sh`、`web/Dockerfile.dev`、`web/dev-entrypoint.sh`、`CHANGELOG.md`。
- **做了什么**：新增 `web/npm-ci.sh`，开发镜像构建和 `dev-entrypoint.sh` 的锁文件变更补装都改用它。先用 `web/.npmrc` 的镜像源，失败后以同一份锁文件和 integrity 加 `--registry=https://registry.npmjs.org` 再执行一次 `npm ci`。两次都带 `--replace-registry-host=always` 和 `server/Dockerfile` 相同的有界超时与重试，覆盖 `.npmrc` 中 600 秒 × 5 次的设置。
- **为什么**：#1156。`server/Dockerfile` 的 web-builder 已有回退，开发镜像只执行一次 `npm ci`，镜像站对单个 tarball 返回 504 就让 `./start.sh dev` 整体失败；容器启动时的补装路径同样没有回退。
- **如何验证**：`sh -n web/npm-ci.sh`；复制 `package*.json` 与脚本到临时目录，把 `.npmrc` 的源改为不可达地址并使用空 npm 缓存执行，第一次安装失败后自动回退官方源，457 个包安装成功。
