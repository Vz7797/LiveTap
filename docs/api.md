# LiveTap 管理与北向接口

本文对应接口版本 `v1`。前半部分是后台 `/admin/api/*`，后半部分是接入方使用的 `/v1/*`。两套鉴权独立；采集节点独立部署、节点之间不通信，房间如何在节点间分配由接入方决定。接入开发建议见 [integration-guide.md](integration-guide.md)。

## 登录与保护

- 前缀 `/admin/api`；JSON 请求/响应，错误为 `{ok:false,error:{code,message},request_id}`。
- `POST /login`：`{username:"admin",password:"<用户输入>"}`。返回登录状态、`must_change_password`、`csrf`，设置 HttpOnly / SameSite=Strict 会话：每次使用顺延到 7 天后（最多每小时写库一次），连续 7 天未使用或登录满 30 天失效；TLS 连接或 LIVETAP_COOKIE_SECURE=true 时设置 Secure。
- `GET /session`：返回是否已登录、是否须改密和 CSRF 令牌；未登录也返回 200。
- 首次登录后只有改密、退出可用。写操作须带 `X-CSRF-Token`；跨站来源拒绝。登录失败与错误旧密码按来源 IP 合计，5 次失败锁该 IP 15 分钟；其他 IP 不受该锁定影响。默认来源为 TCP 对端地址；仅对端属于 `LIVETAP_TRUSTED_PROXIES` 配置的 CIDR 时读取 XFF（从右向左跳过可信代理）或无 XFF 时的单个 X-Real-IP，非法转发头退回 TCP 对端。过期会话每 15 分钟清理。
- `POST /password`：`{current_password,new_password}`，新密码至少 12 字节；成功后撤销其他会话。
- `POST /logout`：撤销当前会话。

凭据只通过 HTTPS 或本机管理连接发送；文档中的占位符不是可用密码。API 不回显 Cookie、代理地址、TikHub token，返回空值和相应的 `*_configured` 布尔值。

## 直播间

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/rooms` | 实际任务、状态、分钟统计、有效配置、覆盖字段名 |
| POST | `/rooms` | `{live_ids:["直播间号或完整链接"],tags:[],enabled:true}（enabled 可选，false 创建暂停房间）`，一次最多 50 项；逐项返回创建、已存在或失败，部分失败不会报全部成功 |
| PATCH | `/rooms/{live_id}` | 可选 `note`、`tags`、`overrides`；只合并传入的配置项，`inherit_all:true` 清除所有房间覆盖 |
| DELETE | `/rooms/{live_id}` | 取消并等待任务退出，再删除任务；历史分钟统计按保留期清理 |
| POST | `/rooms/{live_id}/actions` | `{action:"pause"}`、`resume`、`reconnect`；操作仅影响该直播间 |
| GET | `/rooms/{live_id}/events` | 最近最多 200 条房间内存事件，按 `seq` 倒序；已按房间允许的事件类型过滤 |

接受 `live.douyin.com` 完整直播链接、web_rid 和可从页面确认的短号；无法可靠解析时返回错误，不生成随机 ID。`v.douyin.com` 分享短链请先打开并复制完整直播地址。

任务状态：`probing`、`waiting`、`connecting`、`collecting`、`reconnecting`、`risk`、`notfound`、`stopped`、`ended`（含义见[状态与原因码](#状态与原因码)）；`reason` 为当前原因，`reasonCode` 为机器可读的原因码，`noData` 表示静默旁证均无数据。抖音对节点所在地区不开放的直播间（页面显示“当前直播间在本区域未开放”，接口返回 `status_code=4003073`）记为 `notfound`、停止检测并留下 `region_blocked` 告警，不当作风控退避重试。直播刚结束时接口常返回 `status_code=30003`（24 小时测试中 62 间里 61 间约 10 分钟内被确认下播），按已下播处理、进入正常的下播宽限，不再显示为 `risk`。

`ended`（已下播，停止检测）：任务仍启用，但不再向抖音发任何请求，不占容量，直到 `resume`/`reconnect` 或有新订阅。进入条件：

- 本次启动后采集过，下播后 **10 分钟**内没有重新开播（覆盖断播重开）。观察期内的检查节奏：前 2 分钟每 30 秒一次，之后每 2 分钟一次（每间约 8 次请求）；重复确认“仍未开播”的检查不写时间线、不推送状态；
- 手动/测试任务启动后 **1 小时**内从未开播（订阅任务改用订阅的 `idle_release_seconds`）。

房间字段补充：`avatar` 主播头像缩略图；`endedAt`（Unix 毫秒，0 为未停止）与 `endedReason`（`live_ended` 已下播 / `never_live` 长时间未开播）；`lastLiveAt` 最近一次开始采集的时间；`subscribers`、`subscriberClients`（订阅的接入方名称）；详情接口另有 `subscriptions`（`{client, externalRef}` 列表）。

手动任务处于 `ended` 且距最近一次开播（从未开播则为创建时间）超过 **30 天**，每小时维护时自动删除，并写入 `pruned` 告警与审计。订阅任务在最后一条订阅结束时删除，测试任务由实验室清理。

`/rooms` 和 `/snapshot` 中每间房的 `diagnostics` 提供当前进程内的采集诊断：

| 字段 | 含义 |
|---|---|
| `disconnects` | 离开采集中状态的异常断线次数；同一次重连的多次状态变化不重复计数，正常下播和手动暂停不计 |
| `reconnectDurationsMs` / `reconnectRecoveryCount` | 最近 200 次恢复耗时（毫秒）及累计恢复次数；从断线到再次进入采集中计算 |
| `disconnectedAt` | 尚未恢复的断线时间（Unix 毫秒）；无待恢复断线时为 0 |
| `silenceFalsePositives` | 已发起静默旁证，但旁证期间主连接自行收到消息的次数；无法确认的情况不算误判 |
| `requestsPerMinute` / `requestsTotal` | 该房间过去 60 秒实际获准发出的请求数及当前进程累计请求数；不含仍在限速或冷却中等待的请求 |

诊断不跨进程保存；稳定性观察应同时核对 `node.bootId`，不能将重启前后的累计值直接相减。

房间的 `dropped` 是上游消息进入节点入口队列时被丢弃的累计条数（不含北向推送队列）；`drops` 把它拆开：`{total, lastAt, peakItems, peakBytes, reason: {oversize, full}, type: {chat, gift, social, member, other}}`，`lastAt` 是最近一次丢弃的 Unix 毫秒（没有丢弃为 0），`peakItems` / `peakBytes` 是入口队列积压过的最高条数和字节数。`oversize` 是单条原始消息超过 256 KiB，`full` 是队列达到 4000 条或 1 MiB 时淘汰；队列满时先淘汰点赞等低价值消息，弹幕最后才丢。`type` 按被丢弃那条消息的类型计数。同样不跨进程保存。

`overrides` 可用字段：`engine`（目前仅 `douyinlive`）、`proxy_url`、`cookie`、`sign_provider`（`local` / `tikhub`）、`tikhub_token`、`protocol_mode`（`web` / `pc`）、`poll_seconds`（15～120）、`silence_seconds`（0 表示自适应，或 90～300）、`event_types`。

事件采用设计第 7.3 节的公共字段：`id` / `seq` / `type` / `live_id` / `room_id` / `ts` / `received_at` / `user` / `content` / `extra`。抖音 ID 为字符串，避免 JavaScript 数字精度丢失；管理员事件不含北向订阅字段。正文仅在内存保留。

## 节点与配置

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/snapshot` | 精简快照（与 `/stream` 相同）：任务卡片字段、每间最近 3 条事件、全局最近 50 条消息、真实进程 CPU/RSS、请求预算与滚动进度；`?view=full` 返回完整快照（含逐分钟统计、诊断、覆盖配置与全站分钟历史） |
| GET | `/history` | 全站逐分钟历史（总览“1 小时 / 24 小时”图表按需获取） |
| GET | `/rooms/{live_id}/detail` | 单间完整数据：逐分钟统计、有效配置、覆盖字段、诊断、各类计数、全部事件；房间不存在返回 404 |
| WS | `/stream` | 使用同源管理会话，每 2.5 秒发送精简快照，启用 permessage-deflate；会话失效断开；慢连接写入超时 5 秒 |
| GET | `/settings` | 当前全局设置，敏感值不回显；附带 `db_bytes`（数据库实际占用字节） |
| PUT | `/settings` | `{settings:{要修改的字段},apply:"new"或"rolling"}`；未传项保持不变 |
| POST | `/settings/rollout` | `{action:"pause"或"resume"或"cancel"}`；进度表示重建指令已提交，实际结果以房间状态为准 |
| GET | `/audit?limit=` | 最近的后台操作（新到旧，默认 100 条，最多 500 条），不含敏感值 |

全局 `settings` 字段：`defaults`（同房间配置）、`max_rooms`、`starts_per_second`、`requests_per_second`、`rolling_seconds`、`stats_days`、`event_days`、`share_guest_identity`。默认容量 200、启动 2 次/秒、请求 5 次/秒、滚动间隔 10 秒。只有连接参数改变才重建；消息类型、静默阈值、容量及请求限速立即生效。

`share_guest_identity` 默认 `false`。开启后，相同代理出口和协议的游客任务共享内存中的有效 `ttwid`；显式 Cookie 不参与共享。开关供下一次探测或新建、重建连接使用，不主动断开已有采集连接。

`/snapshot` 和 `/stream` 的 `node` 新增字段：

- `cooldown`：`{active, until, remainingSeconds, strikes}`；`until` 为 Unix 毫秒，`remainingSeconds` 为剩余秒数，`strikes` 为当前退避档位。受限时依次冷却 2、4、8、16、30 分钟，后续封顶 30 分钟；冷却期间并发返回的受限响应合并为同一轮。到期后再有 30 分钟未受限才重置档位。
- `homepageRequestsPerMinute`：过去 60 秒访问 `live.douyin.com/` 的次数，用于比较游客身份共享前后的开销。
- `shareGuestIdentity`：当前共享开关状态（快照采用 camelCase，设置接口采用 snake_case）。
- `entryDrops`：节点启动以来所有房间的入口丢弃，结构同房间 `drops`，包括已删除房间的计数。
- `engine`：采集内核版本与作者在 GitHub 上的最新发布 `{enabled, repo, version, latest, latestUrl, publishedAt, checkedAt, newer, error}`。节点启动约 30 秒后查一次，之后每 6 小时一次；`newer=true` 表示作者发布了比当前固定版本更高的版本，只作提示，升级仍需重新核对补丁并测试。`LIVETAP_UPDATE_CHECK=false` 时 `enabled=false` 且不发请求。查询失败时保留上次结果并在 `error` 说明。
- `latency`：最近 1 分钟的消息延迟 `{upstream, push}`，各为 `{p50_ms, p95_ms, samples, precise}`。`upstream` 是抖音消息自带时间到本节点收到；上游时间只精确到秒时按半秒居中并 `precise=false`（误差约 ±0.5 秒）。`push` 是本节点收到到成功写入接入方 WebSocket，只统计实时推送，不含补拉。

节点冷却只阻止新的探测、旁证和建连；现有连接继续采集。触发时写入 `node_cooldown` 告警，当前告警列表也显示剩余时间。冷却到期后仍遵守请求和启动限速，不集中放行等待中的请求。

额外入口：`GET /healthz` 不需登录；`GET /metrics` 需管理会话。设置 `LIVETAP_ADMIN_LISTEN` 时，主监听地址开放 `/v1/*` 和健康检查；管理接口、界面和指标放在管理监听地址，该地址拒绝 `/v1/*`。不分开监听时仍按路径独立鉴权。

## 代理池与小号池

路径 `/proxies`、`/accounts`，共用会话鉴权和写入 CSRF 保护。资源对象包含 `id,kind,name,tags,enabled,limit,state,checked_at,latency_ms,detail,mask,version,rooms`；`secret` 不序列化到响应。状态为 `unknown`、`healthy`、`invalid`、`restricted`。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/proxies`、`/accounts` | 脱敏列表与当前分配的房间号 |
| POST | 同上 | `{name,tags,enabled,limit,secret}`；新增小号须 `small_account:true` |
| PUT | `/{proxies或accounts}/{id}` | 同创建字段；secret 为空表示保留原凭据，填新值替换并重置检测状态 |
| DELETE | `/{proxies或accounts}/{id}` | 删除资源并解除分配；代理相关房间暂停，小号相关房间退回游客 |
| POST | `/{proxies或accounts}/{id}/check` | `{proxy_id:""}`；可为小号检查指定代理，返回脱敏结果。会访问上游，冷却时拒绝 |
| POST | `/rooms/{live_id}/assignment` | `{kind:"proxy"或"account",resource_id,policy}`；policy 可为 `least_used` / `round_robin`，空值按 resource_id 指定；两者为空解除分配 |

房间 `overrides` 增加 `proxy_id`、`account_id`、`share_guest_identity`（bool，实验对照覆盖）。资源 ID 与共享覆盖不得设为全局默认；资源优先于同一字段的原始配置，分配接口会清除该房间原有的对应原始覆盖。未知状态允许管理员分配，受限/失效或停用资源不可分配；真实有效性以检查和实测为准。

小号检测仅由后台手动调用，没有定时任务；代理仍每 30 分钟自动检测。检测使用内核同款 Windows Chrome 133 浏览器 UA / TLS / HTTP 画像，请求当前自用户接口 `/aweme/v1/web/user/profile/self/`；参考 [上游项目 API 说明](https://github.com/Johnserf-Seed/TikTokDownload/wiki/APIv1.0)。接口可能随上游变化；无法明确确认时返回 unknown，不伪造有效性结论。

## 告警与日志

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/logs?level=&live_id=&q=` | 内存最近 1000 条中的最多 200 条匹配日志；level 为 DEBUG/INFO/WARN/ERROR |
| GET | `/feed?types=&limit=` | 各直播间内存中最近的消息（每间保留最近 200 条），按时间倒序，默认 300 条、最多 500；`types` 逗号分隔（chat/member/like/social/gift），留空为全部；不含 room_stats，只含各房间允许采集的类型 |
| GET | `/logs/stats` | 日志文件列表（名称、大小、修改时间）、总大小、单文件上限、备份数、内存条数与按级别计数 |
| GET | `/logs/export` | 下载全部日志文件（从旧到新拼接的纯文本，写入时已脱敏） |
| POST | `/logs/purge` | 删除轮转备份、清空当前日志文件和内存日志，返回 `{freed}`（字节）；写入审计 |
| GET | `/alerts?level=&rule=&live_id=&acked=&limit=` | 最近匹配告警，默认 200 条、最多 500；`acked` 为 `0` 未处理、`1` 已确认，留空为全部；level 为 critical/warning/info |
| GET | `/alerts/summary` | `{open:{critical,warning,info}, today, total, oldest, top_rules:[{rule,count}]}`：未处理数、今日新增、总数、最早一条的时间、最近 24 小时最多的 3 条规则 |
| POST | `/alerts/{id}/ack` | 确认单条告警 |
| POST | `/alerts/ack` | 批量确认：`{ids:[...]}`（最多 500）或按 `{level, rule, live_id}` 确认全部匹配的未处理告警，返回 `{acked}` |
| POST | `/alerts/purge` | 清理：`{mode:"acked"}` 删除全部已确认告警；`{mode:"before", days:N}` 删除 N 天前的全部告警（`days:0` 清空）。返回 `{deleted}`，写入审计；告警也按“事件与审计保留天数”每小时自动清理 |
| GET | `/alert-rules` | enabled 规则映射、mass_percent、capacity_percent；webhook 永远为空，另有 webhook_configured |
| PUT | `/alert-rules` | 完整规则映射和两项阈值；webhook 留空保留、填写替换，clear_webhook:true 清除 |

规则键：`mass_failure`、`silence`、`risk`、`capacity`、`proxy_invalid`、`account_invalid`、`node_cooldown`。Webhook POST 通用 JSON `{id,at,rule,level,live_id,detail,acked}`；接收端需自行适配企业微信等渠道格式。日志环形缓冲只覆盖当前进程，历史文件按轮转策略保留。

## 测试实验室

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/lab/runs` | 已保留报告（最多 20 份）的摘要与各档结果；只有最新一份带最近 120 个节点采样 `recent`，其余为 `[]` |
| POST | `/lab/runs` | 提交计划，202 返回；同一节点只允许一份实验在运行 |
| POST | `/lab/runs/{id}/stop` | 取消后清理实验自有房间，保留已观察数据 |
| GET | `/lab/runs/{id}/export` | 下载 JSON，包含报告与全部已存采样（节点指标每 5 秒一条，每房间明细每分钟及每档结束各一条）；沿用管理会话鉴权 |
| DELETE | `/lab/runs/{id}` | 删除一份已结束的报告及其采样；运行中的实验需先停止 |
| POST | `/lab/discover` | `{categories:["1_1"]}`，最多 10 个分类；会真实访问分类页，遵守预算与冷却 |

计划字段：`rooms` 数字房间号列表、`stages` 严格递增档位数组、`startup_seconds`（0～600）、`duration_seconds`（1～86400）、`sharing`（如 `[false,true]`）、`modes`（`anonymous` / `proxy` / `account` / `proxy_account`）、可选 `proxy_id` / `account_id`（为空时按最少占用分配）。计划总时长上限 7 天；样本数量应覆盖最大档位，禁止覆盖已有房间。

报告状态 `running/completed/cancelled/interrupted/failed`；completed 仅表示计划观察完成。每档包含开始/结束时间、最低在线数、CPU/内存峰值、按房间诊断、启动未连通原因、按末次已知在线人数的分组统计。导出采样包含异常时间线、每分钟请求、节点冷却；`recovery_incomplete:true` 表示恢复耗时存在缺口。采样的 `down_kb_s` / `up_kb_s` 和各档的 `max_down_kb_s` / `max_up_kb_s` 为节点容器上下行带宽（KB/s，含北向推送与后台页面流量）；`bandwidth_available:false` 表示未能计量，不能据此判断带宽为零。真实服务器验收另行进行。

## 接入方管理（后台）

前缀 `/admin/api`，只接受管理员会话；写操作须带 CSRF。API Key 不能管理接入方。

| 方法 | 路径 | 请求与响应 |
|---|---|---|
| GET | `/clients` | 接入方及实时指标数组，不含完整密钥和摘要 |
| POST | `/clients` | `{name,env,enabled,quota,whitelist}`；201 返回 `{client,api_key}`，完整密钥只在这次响应中出现 |
| GET | `/clients/{id}/subscriptions` | 该接入方的有效订阅（新的在前）：`id`、`live_id`、`external_ref`、`event_types`、`options`、`created_at`、`expires_at`；接入方不存在返回 404 |
| PUT | `/clients/{id}` | 完整提供上述五个字段；可修改额度、网段、启停，返回 `{ok:true}` |
| DELETE | `/clients/{id}` | 撤销密钥、关闭连接、取消该接入方所有订阅，返回 `{ok:true}` |

`name` 为 1～100 字节；`env` 为 `生产` / `预发` / `测试`；`quota` 为 1～2000 且不能低于现有订阅数；`whitelist` 为最多 32 个 IPv4/IPv6 CIDR，空数组表示不限制。示例网段使用 `192.0.2.0/24`，填写时应换成真实接入出口。变更网段会断开既有连接，重新握手检查；停用会取消全部订阅，重新启用后需要重新订阅。

API Key 使用 32 字节随机数的 43 位 Base62 编码，带 `lt_live_` 前缀，共 51 个字符；数据库只存 SHA-256。查询列表只返回固定脱敏标记。创建窗口关闭后不能找回密钥，需要另建接入方替换。不要把创建响应写入日志、截图、浏览器存储或工单。

列表字段：`id,name,env,enabled,quota,whitelist,createdAt,lastSeen,keyPrefix,used,connections,pushPerSec,bytesPerSec,queue,dropped,spark`。`createdAt/lastSeen` 为 Unix 毫秒；`lastSeen=0` 表示未使用；`spark` 当前为空。`used` 为订阅数，`queue` 为所有连接待发送条数。推送速率按最近 60 秒平均，丢弃数为进程内累计队列淘汰次数。被断开时尚未发送的队列内容清除，需重连补拉，不作为逐条淘汰计数。

`GET /snapshot` 及管理 WS 的 `clients` 同上；`node.northbound` 包含 `connections,queue,queue_bytes,push_per_sec,bytes_per_sec,sent,bytes,dropped`。流量仅计成功发送的 WebSocket JSON（含补拉和控制帧），不含 HTTP、TLS 或网卡其他流量；指标随重启清零。接入方删除后不再计入当前接入方汇总。房间 `subscribers` 是该房间的有效订阅引用数。

---

## 北向接口：鉴权和通用约定

所有 `/v1/*` 请求（含 WS 握手）都必须携带：

```http
Authorization: Bearer <API_KEY>
```

`<API_KEY>` 是占位符，不是可用密钥；不要放到 URL、查询参数或文档中。后台 Cookie 不能替代它。IP 限制采用 TCP 来源；仅可信反向代理才读取转发头，规则见前文 `LIVETAP_TRUSTED_PROXIES`。业务服务应使用内网连接或 HTTPS。

JSON 请求最大 64 KiB，拒绝未知字段和多段 JSON。ID 都是字符串；时间使用 RFC3339。成功返回业务对象，不强制包 `data`。订阅、房间状态、事件仅对当前接入方可见；不知道与不属于自己的资源统一返回 404。节点容量是聚合信息，可以用于负载分配。

### 节点信息与容量

`GET /v1/node`，200 示例（全部是示意数据）：

```json
{
  "name": "collector-01",
  "version": "0.3.2-m3.2",
  "api_version": "v1",
  "engine": "douyinlive",
  "engine_version": "v2.3.2 + request-hook",
  "boot_id": "node-start-id",
  "seq": 210,
  "capacity": {"max": 200, "used": 20, "live": 15},
  "health": 100,
  "load_score": 0.1,
  "cooldown": {"active": false, "until": 0, "remainingSeconds": 0, "strikes": 0}
}
```

- `capacity.used` 是启用任务数（含等待、连接、重连），共享订阅只占一间；暂停任务不占用，`live` 是当前采集中数量。
- `health` 为 0～100，按 `risk/reconnecting/notfound` 占启用任务的比例扣分，节点冷却时不高于 50。它不是上游可用性保证。
- `load_score` 为已用/容量，冷却时返回 1；接入方还应显式检查 `cooldown.active`，停止分配新房间。采集中连接继续工作。
- `boot_id` 在重启时变化，`seq` 不跨重启比较。节点不返回其他接入方或直播间清单。
- `api_version` 是接口契约版本，独立于软件发布版本；接入方启动及巡检时校验，未知版本不能照常分配。

### 创建、查询与删除订阅

`POST /v1/subscriptions`：

```json
{
  "live_id": "https://live.douyin.com/123456789",
  "external_ref": "merchant:session",
  "event_types": ["chat", "gift", "status"],
  "options": {"lease_seconds": 120, "idle_release_seconds": 3600}
}
```

- `live_id` 必填，接受 web_rid、完整直播链接和可确认的短号。任务键统一为 web_rid；短号解析会受节点预算和冷却约束。分享短链 `v.douyin.com` 不直接支持，先获取完整直播地址。
- `external_ref` 最长 256 字节，原样附在订阅事件里；不解析业务含义，请勿放凭据。
- `event_types` 省略表示全部六类上游消息；显式空数组只收状态。类型为 `chat/member/like/social/gift/room_stats/status`，状态总是推送。管理员对采集任务关闭的消息类型不会因为订阅而重新开启。
- `options` 可省略；租约默认 120 秒，范围 1～3600；等待开播释放默认 3600 秒，范围 1～86400。正常使用建议保留默认值。这里不能设置 Cookie、代理或签名参数，采集配置仍由节点管理员维护。

200 响应：

```json
{
  "id": "sub_example",
  "live_id": "123456789",
  "external_ref": "merchant:session",
  "event_types": ["chat", "gift", "status"],
  "options": {"lease_seconds": 120, "idle_release_seconds": 3600},
  "created_at": "2026-10-16T12:00:00Z",
  "expires_at": "2026-10-16T12:02:00Z",
  "status": {"live_id": "123456789", "room_id": "", "state": "probing", "reason": "探测中", "reason_code": "probing", "last_message_at": null}
}
```

同一接入方对规范化房间号重复提交相同参数：返回同一 ID 并续租。参数不同返回 409 `subscription_conflict`，需先退订。多个接入方订阅同一房间各有自己的订阅 ID、过滤器与 `external_ref`，共用一条上游连接。最后一条引用消失后立即清理自动任务，手动任务保留。管理员暂停的手动任务不会被订阅自动启动。

| 方法 | 路径 | 响应 |
|---|---|---|
| GET | `/v1/subscriptions` | 200 `{subscriptions:[上述对象]}`，只含本接入方未过期订阅 |
| GET | `/v1/subscriptions/{id}` | 200 单个对象 |
| DELETE | `/v1/subscriptions/{id}` | 200 `{ok:true}`；重复退订返回 404 |

后台手动删除房间、停用接入方、租约到期或持续未开播都可能使订阅结束。租约每秒清理，到期后不能续活，需要重新创建。订阅结束时在线的推送连接立即收到 `subscription_released`；当时不在线的，带游标重连时补发（见下文“连接控制帧”）。节点重启恢复仍有效的订阅，过期和无引用自动任务清除。

### 续租

`POST /v1/subscriptions:renew`：

```json
{"subscription_ids":["sub_example"]}
```

一次 1～2000 条，重复 ID 只算一次。仍有效的照常顺延，已结束的列在 `missing` 里，不影响其余：200 `{subscriptions:[...],missing:[{subscription_id,live_id,reason}]}`，每条 `expires_at` 从当前时间按该订阅的租约长度顺延；该响应不含 `status`。`reason` 与 `subscription_released` 相同；节点已不认识的 ID（超过 24 小时、节点重启后或不属于本接入方）为 `not_found`，此时没有 `live_id`。一条都续不上时返回 404 `not_found`，响应同样带 `missing`。`lease_expired` 而业务仍需要该房间时重新订阅；`live_ended` 表示本场直播已结束。

WS 完成 Pong 确认时自动续租，只有单订阅的流只续该条；全量流续本接入方全部有效订阅。Pong 只在剩余租约不足一半时顺延，所以断流那一刻订阅最少只剩约一半租约（默认约 60 秒）。断流后希望继续占用任务时使用 HTTP 续租（SDK `AutoRenew`），或调大 `lease_seconds`。显式退订优先，不会因仍有 WS 而复活。持续未开播的闲置释放独立于续租，即使还在续租，也会在到达闲置阈值后结束。

Ping 默认每 15 秒一次，`LIVETAP_WS_PING_SECONDS` 可配置为 1～30 秒。短租约测试同时调短 Ping 或使用 HTTP 续租。成功 HTTP 鉴权与 WS Pong 共用最后使用时间，最多 30 秒写数据库一次。

### 批量房间状态

`POST /v1/rooms/status:batch`：

```json
{"live_ids":["123456789","https://live.douyin.com/234567890"]}
```

一次 1～50 个，使用订阅响应中的规范化 `live_id` 或其完整链接，不重新解析短号、不发起上游请求。任一房间未订阅或不存在则整批 404；标识非法为 400。200 返回：

```json
{"rooms":[{"live_id":"123456789","room_id":"upstream-room-id","state":"collecting","reason":"已连接","reason_code":"collecting"}]}
```

状态枚举沿用管理接口；`collecting` 代表正在收集，`waiting` 代表等待开播；其他状态不能直接当成离线或不存在。

订阅查询和批量状态中的 `last_message_at` 是本次房间任务最近收到上游消息的时间（RFC3339）；从未收到或任务重新创建/进程重启后尚未收到时为 `null`。不因开始连接、状态变化或本地心跳而更新。

### WebSocket 事件流

`GET /v1/stream`，WS 握手使用同一个 Bearer 请求头。默认多路推送该接入方全部订阅，新增订阅会动态生效；`?subscription=sub_example` 只收指定订阅。每接入方最多 8 条连接。

- 首次只收后续事件：不带 `since_seq`。
- 需要补回订阅到建连的间隙：先查节点 `boot_id`，携带 `?boot_id=node-start-id&since_seq=0`。
- 重连：使用上次 `boot_id` 和最后成功处理的订阅事件序号 `since_seq`。不要把 API Key 放进这个 URL。

业务事件：

```json
{
  "id":"7701234567890123456", "seq":211, "type":"chat",
  "subscription_id":"sub_example", "external_ref":"merchant:session",
  "live_id":"123456789", "room_id":"7696945466596018971",
  "ts":"2026-10-16T12:00:01.234Z", "received_at":"2026-10-16T12:00:01.310Z",
  "user":{"id":"user-id","sec_uid":"user-sec-id","nickname":"示例用户","avatar":""},
  "content":"示例消息", "extra":{}
}
```

`id` 是原始抖音消息 ID（字符串）；本地状态没有上游 ID，值为空，不能凭空生成抖音 ID。`seq` 为本节点递增序号；接收端不能凭跳号判定缺口，因为其他房间、过滤或控制帧也占用序号。

| type | extra 或正文 |
|---|---|
| chat | user、content |
| member | user |
| like | count、total |
| social | action |
| gift | gift_name、repeat_count（匿名模式可能缺失） |
| room_stats | online、total_pv |
| status（房间） | state、reason；带顶层 subscription_id，正常推进游标 |

#### 订阅状态心跳

每条有效订阅默认每 **60 秒**收到一条 `status` 心跳，配置项为 `LIVETAP_STATUS_HEARTBEAT_SECONDS`（整数 1～3600 秒，重启生效）。每秒检查一次到期时间，调度可能有约一秒偏差；生产建议保留 60 秒，接入方的静默阈值应随配置放宽。

同一房间的多个订阅共用一份状态采样和节点 seq，各自收到自己的 `subscription_id` / `external_ref`。新订阅加入已有房间的心跳周期，首次等待不超过一个间隔；幂等订阅和续租不重置该周期。即使 `event_types` 不包含 `status` 或是空数组，仍推送心跳。无连接时也正常进入内存补拉缓存。

```json
{
  "id": "", "seq": 212, "type": "status",
  "subscription_id": "sub_example", "external_ref": "merchant:session",
  "live_id": "123456789", "room_id": "",
  "ts": "2026-10-16T12:01:00Z", "received_at": "2026-10-16T12:01:00Z",
  "user": {"id":"", "sec_uid":"", "nickname":"", "avatar":""}, "content": "",
  "extra": {
    "code": "heartbeat", "state": "waiting", "reason": "未开播，等待下一次探测", "reason_code": "not_live",
    "last_message_at": null, "heartbeat_interval_seconds": 60,
    "node_cooldown": false, "cooldown_remaining_seconds": 0
  }
}
```

| 字段 | 含义 |
|---|---|
| `extra.code=heartbeat` | 本服务生成的订阅状态心跳；因为有顶层 `subscription_id`，它是可补拉的订阅事件，不是连接控制帧 |
| `extra.state` | 当前状态：`collecting` 采集中、`waiting` 等待开播、`probing` 探测中、`connecting/reconnecting` 连接/重连中、`risk` 状态异常、`notfound` 不存在或在节点所在地区不开放（`reason` 区分，地区限制见下）、`stopped` 已停止、`ended` 已下播并停止检测；另有下面的 `cooling` |
| `extra.reason` / `extra.reason_code` | 当前原因：`reason` 是给人看的中文，可能调整措辞；`reason_code` 是稳定的机器可读代码，程序请按它判断，取值见[状态与原因码](#状态与原因码) |
| `extra.state=cooling` | 节点正在冷却，此启用任务尚未处于采集中；暂停新的探测/建连。`extra.base_state` / `extra.base_reason_code` 同时保留任务原状态和原因码，例如 `waiting`、`probing`、`risk`；此时 `reason_code` 为 `node_cooldown` |
| `extra.node_cooldown` / `cooldown_remaining_seconds` | 采样时节点是否冷却以及剩余秒数。已经 `collecting` 的连接保持该状态；停止/不存在任务也保持原状态，不强行改成冷却 |
| `extra.last_message_at` | 最近收到上游消息的时间，含上游统计等消息；格式 RFC3339。未收到为 `null`，本地心跳不更新它，不跨任务重建/进程重启持久化 |
| `ts` / `received_at` | 本次心跳生成时间；不是最近弹幕时间。补拉时保留原时间 |
| `heartbeat_interval_seconds` | 当前配置的心跳间隔，方便接入方调整静默阈值 |

含义是“这个房间仍由本节点管理”，不保证正在开播或上游有新消息。心跳不进入 AI 回复、不计入上游采集量、不发起抖音请求、不重置上游静默检测，也不续租或重置未开播闲置释放。它与用于确认连接/续租的 WS Ping/Pong 不同。

心跳沿用正常递增 seq、订阅隔离和补拉窗口；SDK 通过 `OnEvent` 交付，成功处理后推进游标。状态属于最高优先级，不参加队列淘汰；全是状态且无法容纳新状态，或状态本身超过队列字节上限时立即断开慢连接，重连后按游标补拉。**不丢弃指发送队列策略，不是永久存储承诺**：断线超过缓存窗口、缓存容量或节点重启仍可能无法补齐，必须处理 `gap`。

#### 状态与原因码

任务状态变化时推送的 `status` 事件（`extra.state`、`extra.reason`、`extra.reason_code`）、上面的状态心跳和 `/v1/rooms/status:batch` 使用同一套取值。状态和原因码**只增不改**：已有的值不会改名或改变含义，新增时写进[更新日志](../CHANGELOG.md)。程序判断请用 `state` 和 `reason_code`，不要解析 `reason` 文字。

| `state` | 含义 | 分类 |
|---|---|---|
| `probing` | 正在向抖音确认是否开播 | 开播前 |
| `waiting` | 未开播，或刚下播、正在等它重开（10 分钟内重开会自动接回） | 开播前 |
| `connecting` | 已开播，正在建立上游连接 | 开播前 |
| `collecting` | 正在接收消息 | 在播 |
| `reconnecting` | 连接中断，正在自动重连 | 异常 |
| `risk` | 暂时无法确认：节点受限、探测超时或连续失败，退避重试 | 异常 |
| `notfound` | 直播间不存在，或在节点所在地区不开放；任务终止 | 异常、结束 |
| `stopped` | 被管理员暂停 | 结束 |
| `ended` | 已下播（或手动任务长时间未开播），停止检测，不占容量 | 结束 |
| `cooling` | 只出现在状态心跳里：节点冷却中，任务暂停探测和建连 | — |

“结束”的任务不再探测或采集，也不占节点容量；其余状态都占容量。

| `reason_code` | 对应状态 | 含义 |
|---|---|---|
| `queued` | `probing` | 排队等待启动限速 |
| `probing` | `probing` | 正在确认直播状态 |
| `not_live` | `waiting` | 未开播，稍后再查 |
| `ended_grace` | `waiting` | 刚下播，等待可能的重开 |
| `upstream_offline` | `waiting` | 采集中途上游确认已下播 |
| `connecting` | `connecting` | 正在建立上游连接 |
| `collecting` | `collecting` | 上游连接已就绪 |
| `no_data` | `collecting` | 已连接，但主连接和旁证连接都收不到消息 |
| `connection_dropped` | `reconnecting` | 采集连接结束，稍后重新连接 |
| `engine_reconnecting` | `reconnecting` | 采集内核正在自动重连 |
| `upstream_failed` | `risk` | 采集内核多次连接失败 |
| `probe_failed` | `risk` | 探测失败且没有明确结论 |
| `probe_timeout` | `risk` | 探测超时 |
| `access_restricted` | `risk` | 遇到验证页或 403/429，节点受限 |
| `node_cooldown` | `risk`、`cooling` | 节点冷却中，暂停新的探测和建连 |
| `resources_unavailable` | `risk` | 分配的代理或小号不可用 |
| `rebuild_limited` | `risk` | 30 分钟内静默重建已达 3 次 |
| `room_not_found` | `notfound` | 上游确认直播间不存在 |
| `region_blocked` | `notfound` | 直播间在节点所在地区不开放 |
| `stopped` | `stopped` | 管理员暂停 |
| `ended_after_live` | `ended` | 下播后 10 分钟内没有重开 |
| `ended_never_live` | `ended` | 手动或测试任务长时间未开播 |

#### 连接控制帧与补拉

控制帧也使用上述 Event 对象，`type=status`，没有顶层 `subscription_id`，由 `extra.code` 区分：

| code | extra 字段 | 接收端处理 |
|---|---|---|
| hello | boot_id、current_seq、subscriptions | 建连第一条；启动标识变化时重置保存的游标。`subscriptions` 是本连接当前承载的订阅数，为 0 而业务仍在等消息时应查询并重新订阅 |
| gap | reason、boot_id、since_seq、reset_cursor | 明确存在无法完整恢复的区间，触发缺口处理；reset_cursor 为 true 时把游标重置为 0 |
| backpressure | dropped、has_gap:true | 当前连接队列发生淘汰；dropped 为本次合并通知的数量，不能静默忽略 |
| subscription_released | subscription_id、live_id、reason | 此处的订阅 ID 在 extra 内；停止该租约的续期或按业务需要重新订阅。断线期间结束的订阅在重连时补发 |

控制帧的 `seq` **不得推进补拉游标**，因为 hello/gap 可能先于较早的缓存事件送达。`subscription_released.reason` 包括 `live_ended/lease_expired/idle_release/unsubscribed/room_removed/client_disabled/client_deleted`。`live_ended` 表示本次直播已结束（下播 10 分钟内未重开），节点已停止检测该房间；下次开播需重新订阅。房间停止检测且不是下播（例如手动房间长时间未开播）时为 `idle_release`；停用/删除会先关闭流，不保证收到关闭通知，应以鉴权失败和订阅查询为准。

带非 0 游标、启动标识不变的全订阅流重连时，节点在 hello 之后补发游标之后结束的订阅的 `subscription_released`。节点为每个接入方保留最近 24 小时、最多 2000 条结束记录，重启后不保留（重启时会收到 `node_restarted` 缺口，应查询订阅）。补发可能与在线时收到的重复，按 `subscription_id` 去重。不带游标或游标为 0 的新连接不补发，应先查询订阅。

补拉规则：

1. 只补自己的、仍有效且在订阅生效之后的事件，按 seq 排序发完后切实时队列。退订后重新订阅不补旧订阅的数据。
2. 缓存每房最多 60 秒、1500 条和 1 MiB；静默时也清理。单连接本次补拉总量最多 20000 条或 32 MiB（约够 100 个热闹直播间断线 60 秒）。正文不落盘。
3. `gap.reason` 为 `buffer_exceeded`（已淘汰）、`replay_limit`（本次补拉量超过上限）、`node_restarted`（启动标识不同或缺失）、`invalid_cursor`（序号大于节点当前值）。多个原因同时出现时返回其中一个，`reset_cursor` 独立表明游标是否必须清零。缺口后仍会发送可恢复部分，不能据此认为全部补齐。
4. 同一个消息在重连或节点迁移时可能再次出现，按 `(live_id,id)` 做幂等；seq 只在同一个 boot_id 内有效。
5. 每个连接独立有界发送队列（2000 条/4 MiB）。先淘汰 like/room_stats，再 member，再 gift/social，最后 chat；status 不参与淘汰，无法容纳时立即断开。普通事件连续 100 次溢出或一次写入超过 5 秒也会断开，重连可能补回；超过缓存窗口仍会发 gap。

### 统一错误

所有北向 HTTP 错误和未升级成功的握手错误使用设计 7.4 的格式：

```json
{"ok":false,"error":{"code":"capacity_exceeded","message":"节点容量已满","suggestion":"请分配到其他节点"},"request_id":"request-example"}
```

| HTTP | code | 说明 |
|---|---|---|
| 400 | invalid_request | JSON、参数、事件类型、租约时长、WS 握手或游标格式有误 |
| 400 | invalid_live_id | 直播间标识无效 |
| 401 | unauthorized | 缺少 API Key、错误、已停用或已删除 |
| 403 | forbidden | 来源 IP 不允许，或跨站 WS 握手 |
| 404 | not_found | 不存在、租约过期或不属于当前接入方；续租全部失败时附 `missing` 说明每个 ID |
| 409 | capacity_exceeded | 节点容量或接入方配额用满 |
| 409 | subscription_conflict | 同房间已有不同参数的订阅 |
| 422 | upstream_unverified | 无法可靠确认短号对应的唯一直播间 |
| 429 | rate_limited | 当前接入方已达 8 条推送连接 |
| 500 | internal_error | 内部失败；不返回底层错误和敏感内容 |

WS 升级后的缺口、队列淘汰使用 status 控制帧；连接关闭后由客户端重新鉴权。`request_id` 仅用于请求定位，不含凭据。

### Go SDK 与最小示例

独立模块 `sdk/go`，导入路径 `github.com/Vz7797/LiveTap/sdk/go`，包名 `collector`。尚未发布为远程模块。接入方可将其随源码复制到自己的仓库（文件头标明来源、软件版本与接口版本），或发布为内部模块；构建不应依赖本机绝对路径或 go.work。下面的相对 replace 仅适用于单独开发 SDK：

```gomod
require github.com/Vz7797/LiveTap/sdk/go v0.0.0
replace github.com/Vz7797/LiveTap/sdk/go => ../LiveTap/sdk/go
```

相对路径按实际 checkout 调整。SDK 提供 `New`、`Node`、`Subscribe`、`Subscriptions`、`Subscription`、`Renew`、`AutoRenew`、`Unsubscribe`、`StatusBatch` 和 `Stream`。

```go
client, err := collector.New(os.Getenv("LIVETAP_NODE_URL"), os.Getenv("LIVETAP_API_KEY"))
if err != nil { return err }
sub, err := client.Subscribe(ctx, collector.SubscribeRequest{
    LiveID: os.Getenv("LIVETAP_LIVE_ID"), ExternalRef: "merchant:session",
    EventTypes: []string{"chat", "status"},
})
if err != nil { return err }
// 完整的取消、自动续租和退订处理见 examples/minimal/main.go。
return client.Stream(ctx, collector.StreamOptions{
    OnEvent: func(e collector.Event) error { return handle(e) },
    OnGap: func(e collector.Event) { reportGap(e.StringField("reason")) },
})
```

- `Renew(ctx, ids)` 返回续上的订阅和已结束的 `[]Released`（订阅 ID、直播间、原因）；一条都续不上时返回 404 的 `*APIError`，`Released` 同样给出原因。
- `AutoRenew(ctx, subs, onReleased)` 阻塞运行，每最短租约的 1/3 周期续租；可放协程。节点报告已结束的订阅从续租集合中去掉并交给 `onReleased`（可为 nil），其余照常续；网络失败、429 和 5xx 在租约内每隔几秒重试。全部结束时返回 nil，其他 API 错误（如密钥停用）返回该错误。过期 ID 不会自动复活，需要时重新订阅并另起续租。
- `StreamOptions.OnReleased` 接收 `subscription_released`，包括重连时补发的断线期间结束的订阅；不设置时照旧作为 status 事件交给 `OnEvent`。
- `Stream` 自动重连，退避 100ms 到 10s，携带游标；鉴权/权限/订阅不存在停止重试。`OnEvent` 成功返回后才确认序号；返回错误则终止，游标不前进。
- 默认每条 Stream 保留每房最近 10 分钟、2000 个消息 ID。跨进程或多节点长期幂等需业务持久化；同一 `Cursor` 不得同时用于多个流。可保存非秘密的 Cursor 以便下次恢复。
- `OnGap` 必须明确处理有缺口的情况；缺少回调时返回 `*GapError`，不会静默忽略。慢回调会让该流进入背压，业务应及时消费。
- 最小程序位于 `sdk/go/examples/minimal/main.go`，只从进程环境读取节点地址、API Key 和房间号；`go -C sdk/go run ./examples/minimal`。对真实节点运行会开始采集。
