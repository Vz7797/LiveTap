# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM node:24-alpine@sha256:a34ab6d7f6f4813f4d0c5469d74685604b962fbdbf5793a7bc7b4998ecb867c8 AS web
WORKDIR /build/web
RUN npm install --global pnpm@11.5.1
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS server
WORKDIR /build/server
COPY third_party/ /build/third_party/
COPY sdk/go/ /build/sdk/go/
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /build/server/internal/webui/dist/ ./internal/webui/dist/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /collector ./cmd/collector \
    && mkdir /data && chown 65532:65532 /data

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL org.opencontainers.image.title="LiveTap" \
      org.opencontainers.image.description="Douyin live-comment collector node" \
      org.opencontainers.image.source="https://github.com/Vz7797/LiveTap" \
      org.opencontainers.image.url="https://github.com/Vz7797/LiveTap"
COPY --from=server --chown=65532:65532 /collector /collector
COPY --from=server --chown=65532:65532 /data /data
USER 65532:65532
# distroless 没有 /etc/localtime，不设 TZ 时 Go 按 UTC 计时，“今日”统计会在北京时间 8 点切日。
ENV LIVETAP_DATA_DIR=/data LIVETAP_LISTEN=0.0.0.0:8090 TZ=Asia/Shanghai
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=4s --start-period=10s --retries=3 CMD ["/collector", "--healthcheck"]
ENTRYPOINT ["/collector"]
