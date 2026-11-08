# 第三方组件说明

本项目使用以下主要第三方组件。完整依赖清单以 `server/go.mod`、`sdk/go/go.mod`、`web/package.json` 及各依赖自带的许可文件为准。

## 随仓库分发（vendored）

| 组件 | 位置 | 许可 | 说明 |
|---|---|---|---|
| [douyinLive](https://github.com/jwwsjlm/douyinLive) v2.3.2（`3b0c448`） | `third_party/douyinLive/` | MIT | 采集内核。原始 `LICENSE` 保留在该目录；本项目的修改见 `ORIGIN.md` 与 `deploy/patches/` |
| [beUI](https://beui.dev)（starc007/ui-components） | `web/src/components/motion/` 等 | MIT | 动效组件，按本项目需要修改；来源与范围见该目录 `LICENSE-beui.txt` |

## Go 依赖

| 组件 | 许可 |
|---|---|
| github.com/jwwsjlm/douyinlive-proto | MIT |
| github.com/jwwsjlm/req/v3 | MIT |
| github.com/coder/websocket | ISC |
| modernc.org/sqlite | BSD-3-Clause |
| github.com/shirou/gopsutil/v4 | BSD-3-Clause |
| github.com/tidwall/gjson | MIT |
| golang.org/x/crypto、golang.org/x/time | BSD-3-Clause |
| google.golang.org/protobuf | BSD-3-Clause |

## 前端依赖

| 组件 | 许可 |
|---|---|
| React、React DOM | MIT |
| Tailwind CSS | MIT |
| Radix UI / shadcn/ui | MIT |
| Motion | MIT |
| TanStack Query、TanStack Table | MIT |
| lucide-react | ISC |
| Geist Mono 字体（@fontsource/geist-mono） | SIL Open Font License 1.1 |
