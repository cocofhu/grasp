# grasp 镜像(构建上下文 = 仓库根,因为要把 web/ 前端一起打进来)。
#
# 硬性约定(deployment docs):**配置不打进镜像**。这里只构建并
# 承载业务二进制 + 前端静态产物;运行时配置由环境变量 + ConfigMap 挂载的
# CONFIG_PATH 提供,镜像内不 COPY 任何配置 / 密钥。
#
# 需要 CGO(go-sqlite3)。运行阶段用 node:20-bookworm-slim:既有 glibc/SQLite
# 运行时依赖,也提供 node 供 set_plan 做 Mermaid 11.x 语法校验。

# --- Web (Vite) 构建 ---
# Node 24 与 ci-web 对齐。commander 15 要求 >=22.12；js-beautify 2 带上的
# nopt 10 要求 ^24.15，浮动标签 node:24 当前满足。运行阶段仍是 Node 20。
FROM node:24-bookworm-slim AS web-builder
# Keep repo-relative layout: web build runs ../scripts/assert-no-approving-brand.mjs
# which walks its parent as the project root. WORKDIR /web would put that parent
# at /, and the guard would scan the whole image (OOM).
WORKDIR /src/web
# .npmrc 把 registry 指向 registry.npmmirror.com(见 web/.npmrc):国内网络直连
# registry.npmjs.org 拉大包(monaco-editor)易 ECONNRESET,换镜像源规避。
COPY web/package*.json web/.npmrc ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
COPY scripts/assert-no-approving-brand.mjs /src/scripts/assert-no-approving-brand.mjs
RUN npm run build

# --- 沙箱运行时包 ---
# backend / preview-inject 与 startup.sh 等脚本打成 sandbox-runtime.tgz，随服务端下发给
# 沙箱（沙箱镜像只含环境 + grasp-bootstrap.sh）。见 scripts/build-sandbox-runtime.sh。
FROM golang:1.26-bookworm AS sandbox-runtime
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn
COPY sandbox-gateway/sandbox/go.mod sandbox-gateway/sandbox/go.sum sandbox-gateway/sandbox/
RUN cd sandbox-gateway/sandbox && go mod download
COPY sandbox-gateway/sandbox/ sandbox-gateway/sandbox/
COPY scripts/build-sandbox-runtime.sh scripts/
RUN scripts/build-sandbox-runtime.sh /out

# `docker build --target sandbox-runtime-export --output type=local,dest=<dir>` 导出运行时包（CI 用）。
FROM scratch AS sandbox-runtime-export
COPY --from=sandbox-runtime /out/sandbox-runtime.tgz /

# --- Go 构建 ---
FROM golang:1.26-bookworm AS build
WORKDIR /src
# 国内直连 proxy.golang.org / sum.golang.org 易超时(与 Dockerfile.dev 一致),
# 走七牛 goproxy.cn + Google 中国区校验和镜像。
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
# 前端产物随二进制由 Go 进程从文件系统服务(非 go:embed)。
COPY --from=web-builder /src/web/dist ./web/dist
# Optional revision stamp for the overview-page service commit badge.
# Production images have no .git (root .dockerignore); pass GIT_COMMIT or
# GITHUB_SHA as build-arg. Empty args still produce a working binary.
ARG GIT_COMMIT=""
ARG GITHUB_SHA=""
RUN COMMIT_SHA="${GIT_COMMIT:-$GITHUB_SHA}" && \
    CGO_ENABLED=1 go build -trimpath \
      -ldflags="-s -w -X github.com/cocofhu/grasp/internal/version.commit=${COMMIT_SHA}" \
      -o /out/grasp-server ./cmd/server
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/grasp ./cmd/doctor

# --- 运行 ---
# 使用 node bookworm-slim 作为运行底包,使 set_plan 的 Mermaid 11.x 语法校验
# (node + 内嵌 bundle.mjs)在镜像内可用,并与 web-builder 同 glibc。
FROM node:20-bookworm-slim
# 后端不再直连 Docker:沙箱由 sandbox-gateway 经 REST 创建、数据面走 SSH,
# 因此运行镜像无需 docker CLI / dockerd,只需 CA 证书(HTTPS 连网关 / git)。
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
WORKDIR /app
COPY --from=build /out/grasp-server /app/grasp-server
COPY --from=build /out/grasp /app/grasp
COPY --from=build /src/web/dist /app/web/dist
COPY --from=sandbox-runtime /out/sandbox-runtime.tgz /app/sandbox-runtime/sandbox-runtime.tgz

# 数据落 SQLite,挂到持久卷;端口与默认 DB 路径可被部署的 env 覆盖。
ENV GRASP_PORT=8080 \
    GRASP_DB=/data/grasp.db
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/grasp-server"]
