# 开发指南

本文说明如何在本机构建、运行和测试 LiveTap。架构与规则以 [design.md](design.md) 为准，贡献流程见 [../CONTRIBUTING.md](../CONTRIBUTING.md)。

## 环境要求

| 工具 | 版本 |
|---|---|
| Go | 1.27 |
| Node.js | 24 |
| pnpm | 11.5.1 |
| Git | 任意较新版本 |
| Docker | 仅构建镜像时需要 |

## 目录结构

```text
server/                 Go 后端（采集引擎适配、任务管理、管理接口、北向接口）
  cmd/collector/        程序入口
  internal/             业务模块：engine、room、hub、api、store、observe、pool、lab ...
  internal/webui/dist/  前端打包产物（构建时生成，仓库只保留占位文件）
web/                    React 管理后台
sdk/go/                 独立的 Go 客户端，供接入方使用
third_party/douyinLive/ 固定版本的采集内核（MIT，带本项目补丁）
deploy/                 Dockerfile、compose、内核补丁与校验脚本、本机启动脚本
tests/                  集成、构建与界面回归脚本
docs/                   正式文档
bin/                    本机构建产物（不进仓库）
test-results/           测试输出与截图（不进仓库）
```

## 构建

```powershell
# 1. 校验内核补丁与 v2.3.2 基线一致（离线，不修改内核）
.\deploy\prepare-engine.ps1

# 2. 构建前端并嵌入后端
pnpm --dir web install --frozen-lockfile
pnpm --dir web build

# 3. 编译后端（前端已嵌入）
go -C server build -trimpath -ldflags='-s -w' -o ../bin/collector.exe ./cmd/collector
```

- 内核固定在 `third_party/douyinLive/`，基线 v2.3.2 / `3b0c44876c2a9221cb11d27041af83720b9210c5`。`server/go.mod` 用相对路径 `replace` 引用，不需要 `go.work` 或任何本机绝对路径。
- `pnpm --dir web build` 会把产物同步到 `server/internal/webui/dist/`，Go 编译时嵌入。未构建前端时后端仍可编译和测试，只是不带管理界面。
- 修改内核后必须重新生成补丁：以基线提交为底，与 `third_party/douyinLive/` 做差异，写入 `deploy/patches/douyinlive-v2.3.2-http-hook.patch`，并运行 `node deploy/verify-engine.mjs` 确认通过。

## 本机运行

在密码管理器中生成并保存一份 **32 字节随机值的 Base64 主密钥**，以后每次启动都使用同一份，否则无法解密已有配置。

```powershell
.\deploy\run-local.ps1
```

脚本以隐藏输入读取主密钥；首次运行再读取管理员初始密码（至少 12 位）。默认监听 `127.0.0.1:8090`，数据位于 `%LOCALAPPDATA%\LiveTap\data`。打开 http://127.0.0.1:8090 ，账号 `admin`，首次登录必须修改密码。

前端日常开发：先启动后端，再运行 `pnpm --dir web dev`（端口 5310，自动转发到本机 8090）。

> 后台添加直播间默认立即开始真实采集。家用宽带出口容易被抖音要求验证，批量测试请在专用测试服务器上进行。

## 测试

日常提交前至少运行：

```powershell
go -C server vet ./...
go -C server test ./...
go -C sdk/go test ./...
node deploy/verify-engine.mjs
pnpm --dir web build
```

更多检查：

```powershell
# 并发检查
go -C server test -race ./...

# 内核钩子与补丁行为
go -C server test github.com/jwwsjlm/douyinLive/v2 -run 'Test(RequestHook|Collector|RoomRefresh|Proxy)'

# 观察脚本与构建回归
node --test tests/integration/observation.test.mjs tests/build/engine.test.mjs

# 界面离线检查（本地静态文件 + 模拟管理接口，拦截外部网络）
node tests/ui/offline-core.mjs        # 总览、直播间、实时消息、设置
node tests/ui/offline-resources.mjs   # 代理池、小号池、测试实验室、告警与日志
node tests/ui/offline-clients.mjs     # 接入方与 API Key

# 重新生成 README 截图（内置演示数据，完全离线；输出到 docs/images/）
pnpm --dir web shots

# 容器检查（临时凭据、隔离网络、独立数据卷；不访问抖音）
docker build -f deploy/Dockerfile -t livetap:local .
node tests/integration/docker.mjs
```

### 北向压力测试

使用假引擎和临时数据库，200 个房间、4 个接入方（其中一个故意不消费），不访问抖音：

```powershell
$env:LIVETAP_STRESS = '1'
go -C server test ./internal/api/northbound -run '^TestNorthboundStress$' -count=1 -v -timeout 100s
Remove-Item Env:LIVETAP_STRESS
```

结果写入 `test-results/stress/stress.json`。CPU 100% 表示占满一个逻辑核心；结果包含同进程的测试客户端，不能直接换算成真实房间容量。

### 真实采集观察

`node tests/integration/live.mjs` 会启动本地实例并真实采集，默认启动期 120 秒、观察 10 分钟。可用进程环境变量 `LIVETAP_STARTUP_SECONDS`、`LIVETAP_LIVE_MINUTES`、`LIVETAP_SHARE_GUEST_IDENTITY`、`LIVETAP_TEST_ROOMS`、`LIVETAP_TEST_BINARY` 调整；`LIVETAP_FUNCTIONAL_ONLY=1` 为短时功能模式。报告按“模式 + 共享状态 + 开始时间”写入 `test-results/live/`，不会覆盖历史结果。

服务器上的容量与稳定性实测使用后台“测试实验室”，方法见 [design.md](design.md) 第 12 节。

## 提交规范

见 [../CONTRIBUTING.md](../CONTRIBUTING.md)。
