# Sandbox Gateway control-plane image (thin lifecycle API).
# Build: docker build -t sandbox-gateway:local .
# The universal sandbox image is built separately from sandbox/Dockerfile.

FROM golang:1.26-bookworm AS build
WORKDIR /src

# 国内 Go 模块代理
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=sum.golang.google.cn

COPY gateway/go.mod gateway/go.sum ./
RUN go mod download

COPY gateway/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/sandbox-gateway ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# docker driver 需要 CLI（通过挂载的 docker.sock 操作宿主机 Docker）
COPY --from=docker:27-cli /usr/local/bin/docker /usr/local/bin/docker

COPY --from=build /out/sandbox-gateway /usr/local/bin/sandbox-gateway

ENV SBGW_CONFIG=/etc/sandbox-gateway/config.yaml
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/sandbox-gateway"]
