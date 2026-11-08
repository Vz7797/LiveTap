# 部署指南

LiveTap 以单个容器运行，前端已内嵌。一个容器就是一个采集节点；扩容时增加节点，由接入方负责在节点之间分配直播间。

## 镜像

[deploy/Dockerfile](../deploy/Dockerfile) 使用固定摘要的 Node / Go 多阶段构建，运行层为 distroless static：

- 以 UID/GID `65532:65532` 运行，非 root；
- 只读根文件系统，可写数据只在 `/data`；
- 健康检查执行 `/collector --healthcheck`，只请求本机 `/healthz`，不读取密码、不访问抖音；
- 默认 `TZ=Asia/Shanghai`，“今日”统计按北京时间 0 点切日。

## 启动

将主密钥 `LIVETAP_SECRET_KEY`（以及首次启动用的 `LIVETAP_ADMIN_INITIAL_PASSWORD`）从密码管理器注入**当前进程环境**，不要把明文写进 `.env`、YAML 或命令历史：

```powershell
docker compose -f deploy/compose.yaml up -d --build
docker compose -f deploy/compose.yaml ps
```

[deploy/compose.yaml](../deploy/compose.yaml) 已包含：只读根文件系统、去除全部特权、`no-new-privileges`、数据卷、健康检查、日志大小上限，以及只绑定 `127.0.0.1:8090`。

## 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `LIVETAP_SECRET_KEY` | 必填 | 32 字节随机值的 Base64；加密代理、小号、API Key 等敏感配置。丢失后已有数据无法解密 |
| `LIVETAP_ADMIN_INITIAL_PASSWORD` | 首次必填 | 至少 12 位；首次登录强制修改，之后可从环境中移除 |
| `LIVETAP_LISTEN` | `0.0.0.0:8090` | 主监听地址（北向接口；未单独配置时也承载管理后台） |
| `LIVETAP_ADMIN_LISTEN` | 空 | 管理后台单独监听地址，建议只绑定内网 |
| `LIVETAP_DATA_DIR` | `/data` | SQLite 数据库与日志目录 |
| `LIVETAP_NODE_NAME` | `collector-01` | 节点名称，显示在后台和北向 `/v1/node` |
| `LIVETAP_LOG_LEVEL` | `INFO` | 日志级别 |
| `LIVETAP_COOKIE_SECURE` | `false` | 位于 HTTPS 反向代理之后时设为 `true` |
| `LIVETAP_TRUSTED_PROXIES` | 空 | 实际反向代理的 CIDR，逗号分隔；为空时不信任任何转发头 |
| `LIVETAP_UPDATE_CHECK` | `true` | 是否每 6 小时向 `api.github.com` 查询采集内核作者（jwwsjlm/douyinLive）的最新发布，结果只显示在后台顶部的版本标识里，不会下载或改动任何东西。节点不能访问外网或不希望有这条出站请求时设为 `false` |
| `LIVETAP_LAB_ENABLED` | `false` | 是否开启“测试实验室”。实验室会真实连接抖音直播间做压测，正式环境保持关闭，需要压测时临时设为 `true` 并重启 |
| `LIVETAP_WS_PING_SECONDS` | `15` | 北向 WebSocket Ping 间隔（1～30） |
| `LIVETAP_STATUS_HEARTBEAT_SECONDS` | `60` | 订阅状态心跳间隔（1～3600） |
| `TZ` | `Asia/Shanghai` | 时区 |

完整说明见 [design.md](design.md) 第 13 节。

## 反向代理与访问控制

- 管理后台和北向接口都应位于可信内网，或经 HTTPS/WSS 暴露；不要在公网用明文 HTTP 传输密码或 API Key。
- 使用反向代理时，设置 `LIVETAP_COOKIE_SECURE=true`，并在 `LIVETAP_TRUSTED_PROXIES` 中**只**填写实际代理的网段。只有 TCP 对端属于可信网段时才读取 `X-Forwarded-For`（从右向左跳过可信代理，取第一个非可信地址），否则使用 TCP 对端地址；登录失败锁定按此来源计算。
- 反向代理需支持 WebSocket 升级和长连接，空闲超时应大于 Ping 间隔。

## 版本与发布

- 版本号只在 `server/internal/buildinfo/VERSION` 一处维护（语义化版本，如 `1.0.0`），后台“版本信息”、`/healthz` 与北向 `/v1/node` 都从这里读取；后台同时显示构建所用提交。
- 发布新版本：修改 `VERSION`，把 `CHANGELOG.md` 的“未发布”整理为新版本号与日期，合并后在主分支打 `vX.Y.Z` 标签。
- 镜像按版本号打标签（`LIVETAP_IMAGE_TAG=1.0.0 docker compose build`），不使用 `latest`；回滚时把 `LIVETAP_IMAGE_TAG` 改回上一个版本并重启。

## 数据与备份

- `/data` 保存 SQLite 数据库和轮转日志（`logs/collector.log`，10 MB × 5 份）。
- 新建命名卷会自动继承目录权限；使用已有的绑定目录时，须让 UID 65532 可写。
- 主密钥必须与数据库备份分开、安全地保存。
- 不要对正式环境执行 `docker compose down -v`，它会删除数据卷。

## 节点出口与抖音访问

- 节点以游客身份只读访问公开直播间，不发送弹幕、不点赞、不处理验证码。
- 新的机房 IP 若在短时间内大量启动房间，可能被要求验证。服务端有节点级受限熔断，并限制启动速度与请求预算；容量与稳定性以测试实验室的实测为准（[design.md](design.md) 第 5.5、12 节）。
- 测试与生产节点应分开，避免测试流量影响生产 IP。
