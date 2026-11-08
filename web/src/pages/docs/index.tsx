import {
  ArrowRight,
  BookOpen,
  Boxes,
  Check,
  Copy,
  Gauge,
  KeyRound,
  Layers,
  ListChecks,
  RadioTower,
  RefreshCcw,
  Rocket,
  Route,
  ShieldCheck,
  Siren,
  TimerReset,
  Waypoints,
  Workflow,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { navigate } from "@/app/router";
import { Card, IconTile, PageHeader, PillButton, Tag } from "@/components/kit";
import type { Tone } from "@/lib/meta";
import { cn } from "@/lib/utils";
import { C, CodeBlock } from "./code";
import { examples } from "./examples";

/* 接入文档：给接入方的开发者看的北向接口说明。内容与 docs/api.md、docs/integration-guide.md 一致。 */

interface Section {
  id: string;
  title: string;
  icon: LucideIcon;
  tone: Tone;
  summary: string;
  method?: "GET" | "POST" | "DELETE" | "WS";
  path?: string;
  body: (base: string) => ReactNode;
}

const METHOD_TONE: Record<string, Tone> = { GET: "ok", POST: "info", DELETE: "err", WS: "social" };

/* ---------------- 小组件 ---------------- */

function Method({ m }: { m: string }) {
  return <Tag tone={METHOD_TONE[m] ?? "idle"} className="num w-14 justify-center font-semibold">{m}</Tag>;
}

function Note({ tone = "info", title, children }: { tone?: Tone; title?: string; children: ReactNode }) {
  return (
    <div className={cn("rounded-2xl px-4 py-3 text-sm leading-relaxed", `tint-${tone}`)}>
      {title && <div className="mb-0.5 font-medium">{title}</div>}
      <div className="text-foreground/85">{children}</div>
    </div>
  );
}

function Table({ head, rows }: { head: string[]; rows: ReactNode[][] }) {
  return (
    <div className="scroll-area overflow-y-hidden rounded-2xl border">
      <table className="w-full min-w-[560px] text-sm">
        <thead className="bg-muted/40 text-left text-xs text-muted-foreground">
          <tr>
            {head.map((h) => (
              <th key={h} className="px-4 py-2.5 font-medium">{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-t align-top">
              {r.map((c, j) => (
                <td key={j} className={cn("px-4 py-2.5", j === 0 && "whitespace-nowrap")}>{c}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function P({ children }: { children: ReactNode }) {
  return <p className="max-w-[860px] text-sm leading-relaxed text-foreground/85">{children}</p>;
}

function Steps({ items }: { items: { title: string; body: ReactNode }[] }) {
  return (
    <ol className="grid gap-3 md:grid-cols-2 2xl:grid-cols-3">
      {items.map((s, i) => (
        <li key={s.title} className="flex gap-3 rounded-2xl border bg-background/40 p-4">
          <span className="num inline-flex size-7 shrink-0 items-center justify-center rounded-full bg-primary text-xs font-bold text-primary-foreground">{i + 1}</span>
          <div className="min-w-0">
            <div className="text-sm font-medium">{s.title}</div>
            <div className="mt-1 text-xs leading-relaxed text-muted-foreground">{s.body}</div>
          </div>
        </li>
      ))}
    </ol>
  );
}

/* ---------------- 内容 ---------------- */

const GROUPS: { title: string; sections: Section[] }[] = [
  {
    title: "接入基础",
    sections: [
      {
        id: "start",
        title: "快速开始",
        icon: Rocket,
        tone: "chat",
        summary: "五步接通：建 Key、查节点、订阅、收消息、按场次结束。",
        body: (base) => (
          <>
            <Steps
              items={[
                { title: "创建 API Key", body: <>在后台“接入方”页面为每个业务系统实例创建一个 Key。Key 只显示一次，存进服务端的密钥配置，不要放进浏览器或代码仓库。</> },
                { title: "读取节点信息", body: <>启动时调用 <C>GET /v1/node</C>，确认 <C>api_version</C> 为 <C>v1</C>，并查看容量和是否冷却。</> },
                { title: "商家开播时订阅", body: <>调用 <C>POST /v1/subscriptions</C>，传直播间号和你们自己的场次标识 <C>external_ref</C>。</> },
                { title: "连接推送，接收消息", body: <>每个节点保持一条 <C>WS /v1/stream</C>，按订阅分发给各商家。连接在线时订阅会自动续期。</> },
                { title: "按场次结束", body: <>收到 <C>subscription_released</C> 且原因为 <C>live_ended</C>，表示这场已下播；商家主动结束时直接退订。下次开播重新订阅。</> },
              ]}
            />
            <CodeBlock title="最小示例" tabs={examples.quickstart(base)} />
          </>
        ),
      },
      {
        id: "auth",
        title: "鉴权与限制",
        icon: ShieldCheck,
        tone: "ok",
        summary: "所有请求都带 Bearer Key；了解来源限制、额度和请求格式。",
        body: () => (
          <>
            <CodeBlock tabs={[{ label: "请求头", code: "Authorization: Bearer <API_KEY>" }]} />
            <Table
              head={["项目", "约定"]}
              rows={[
                ["API Key", <>以 <C>lt_live_</C> 开头，共 51 个字符；节点只保存摘要，丢失后只能新建接入方替换</>],
                ["适用范围", <>所有 <C>/v1/*</C> 请求，包括 WebSocket 握手；不要把 Key 放进 URL 或查询参数</>],
                ["来源限制", "接入方可设置允许的 IP 网段，不在范围内返回 403；建议填写业务系统服务器的出口地址"],
                ["订阅额度", "每个接入方可同时订阅的直播间数量，超过返回 409 capacity_exceeded"],
                ["推送连接", "每个接入方最多 8 条 WebSocket，超过返回 429"],
                ["请求格式", "JSON 请求最大 64 KiB，拒绝未知字段；ID 一律是字符串，时间为 RFC3339"],
                ["可见范围", "只能看到自己的订阅和事件；不存在或不属于自己的资源统一返回 404"],
              ]}
            />
            <Note tone="warn" title="传输安全">节点与接入方之间请走内网或 HTTPS / WSS，不要在公网用明文 HTTP 传 Key。</Note>
          </>
        ),
      },
      {
        id: "lifecycle",
        title: "一场直播一次订阅",
        icon: Workflow,
        tone: "member",
        summary: "订阅跟着商家的直播场次走：开播订阅，下播由节点确认后结束。",
        body: () => (
          <>
            <div className="grid gap-3 md:grid-cols-4">
              {[
                { t: "订阅", d: "商家开播时创建；可以先订阅再开播", tone: "chat" as Tone },
                { t: "等待开播", d: "最长 idle_release_seconds（默认 60 分钟），仍未开播则结束订阅", tone: "member" as Tone },
                { t: "采集中", d: "持续推送消息；断线会自动重连", tone: "ok" as Tone },
                { t: "下播确认", d: "10 分钟内没有重新开播，推送 live_ended 并停止检测", tone: "warn" as Tone },
              ].map((s, i) => (
                <div key={s.t} className="relative rounded-2xl border bg-background/40 p-4">
                  <div className="flex items-center gap-2">
                    <span className={cn("num inline-flex size-6 items-center justify-center rounded-full text-[11px] font-bold", `tint-${s.tone}`)}>{i + 1}</span>
                    <span className="text-sm font-medium">{s.t}</span>
                  </div>
                  <div className="mt-2 text-xs leading-relaxed text-muted-foreground">{s.d}</div>
                  {i < 3 && <ArrowRight className="absolute top-1/2 -right-3 hidden size-4 -translate-y-1/2 text-faint md:block" />}
                </div>
              ))}
            </div>
            <Table
              head={["情况", "节点怎么做", "接入方怎么做"]}
              rows={[
                ["收到一次 waiting", "继续检查：下播后前 2 分钟每 30 秒一次，之后每 2 分钟一次", "可以提示“疑似下播”，不要马上结束助播"],
                ["10 分钟内重新开播", "自动接上，订阅不变", "照常处理消息"],
                [<>收到 <C>live_ended</C></>, "已停止检测该直播间，订阅结束", "这场助播结束，停止续租；下次开播重新订阅"],
                [<>收到 <C>idle_release</C></>, "长时间未开播，订阅结束", "提示商家未开播；需要时重新订阅"],
                ["商家点击结束助播", "—", <>直接 <C>DELETE</C> 退订，不必等节点确认</>],
                ["商家不再合作", "租约到期（默认 120 秒）自动清理", "退订即可；忘了退订也不会一直占用"],
              ]}
            />
            <Note>probing、cooling、risk 不是下播信号，不要据此结束场次。</Note>
          </>
        ),
      },
    ],
  },
  {
    title: "接口参考",
    sections: [
      {
        id: "node",
        title: "节点信息",
        icon: Gauge,
        tone: "info",
        summary: "检查接口版本、容量、健康度和冷却状态，用于选节点。",
        method: "GET",
        path: "/v1/node",
        body: (base) => (
          <>
            <Table
              head={["字段", "说明"]}
              rows={[
                [<C>api_version</C>, "接口契约版本，当前为 v1；未知版本不要分配新房间"],
                [<C>capacity</C>, <><C>max</C> 容量、<C>used</C> 已占用、<C>live</C> 正在采集；已下播停止检测的不占容量</>],
                [<C>health</C>, "0～100，按异常任务比例扣分；冷却时不高于 50"],
                [<C>load_score</C>, "已用 / 容量，冷却时为 1；选节点时取剩余容量多的"],
                [<C>cooldown</C>, <><C>active</C> 为 true 时不要再分配新房间；已有连接继续采集</>],
                [<C>boot_id</C>, <>每次重启都会变化，补拉游标要与它一起保存</>],
              ]}
            />
            <CodeBlock title="请求与响应" tabs={examples.node(base)} />
          </>
        ),
      },
      {
        id: "subscribe",
        title: "创建订阅",
        icon: Waypoints,
        tone: "chat",
        summary: "订阅一个直播间；同一接入方对同一房间重复提交相同参数会返回同一个订阅并续期。",
        method: "POST",
        path: "/v1/subscriptions",
        body: (base) => (
          <>
            <Table
              head={["参数", "必填", "说明"]}
              rows={[
                [<C>live_id</C>, "是", <>直播间号（web_rid）或完整直播链接 <C>live.douyin.com/…</C>；分享短链请先换成完整地址</>],
                [<C>external_ref</C>, "否", "你们的商家 / 场次标识，最长 256 字节，原样附在每条事件里；不要放凭据"],
                [<C>event_types</C>, "否", <>要接收的类型：<C>chat</C> <C>member</C> <C>like</C> <C>social</C> <C>gift</C> <C>room_stats</C> <C>status</C>；省略为全部，状态总会推送</>],
                [<C>options.lease_seconds</C>, "否", "租约秒数，默认 120（1～3600）"],
                [<C>options.idle_release_seconds</C>, "否", "等待开播最长秒数，默认 3600（1～86400）"],
              ]}
            />
            <CodeBlock title="请求与响应" tabs={examples.subscribe(base)} />
            <Note>同一房间参数不同（例如 external_ref 不同）返回 409 <C>subscription_conflict</C>，需要先退订。多个接入方订阅同一房间会共用一条上游连接。</Note>
          </>
        ),
      },
      {
        id: "manage",
        title: "查询与退订",
        icon: ListChecks,
        tone: "member",
        summary: "列出本接入方的订阅、查看单个订阅、退订。",
        method: "GET",
        path: "/v1/subscriptions",
        body: (base) => (
          <>
            <Table
              head={["方法", "路径", "说明"]}
              rows={[
                [<Method m="GET" />, <C>/v1/subscriptions</C>, "本接入方未过期的全部订阅"],
                [<Method m="GET" />, <C>/v1/subscriptions/{"{id}"}</C>, "单个订阅，含当前房间状态"],
                [<Method m="DELETE" />, <C>/v1/subscriptions/{"{id}"}</C>, "退订；重复退订返回 404"],
              ]}
            />
            <CodeBlock tabs={examples.manage(base)} />
          </>
        ),
      },
      {
        id: "renew",
        title: "续租",
        icon: RefreshCcw,
        tone: "gift",
        summary: "推送连接在线时会自动续期；断流期间想继续占用订阅，用 HTTP 续租。",
        method: "POST",
        path: "/v1/subscriptions:renew",
        body: (base) => (
          <>
            <P>一次 1～2000 个订阅 ID，任一无效（过期或不属于自己）整批返回 404。成功后每个订阅的 <C>expires_at</C> 从当前时间按各自的租约顺延。过期的订阅不能续活，需要重新创建。</P>
            <CodeBlock tabs={examples.renew(base)} />
            <Note>全量推送连接的 Pong 会续期本接入方的全部订阅；退订某个房间时请调用 DELETE，只停止续租并不能让它立即结束。</Note>
          </>
        ),
      },
      {
        id: "status",
        title: "批量房间状态",
        icon: Layers,
        tone: "ok",
        summary: "一次查询最多 50 个已订阅房间的当前状态，不会向抖音发请求。",
        method: "POST",
        path: "/v1/rooms/status:batch",
        body: (base) => (
          <>
            <Table
              head={["状态", "含义"]}
              rows={[
                [<C>collecting</C>, "正在采集"],
                [<C>waiting</C>, "等待开播，或刚下播在观察期内"],
                [<C>probing</C>, "正在确认直播状态（包括排队等待启动）"],
                [<><C>connecting</C> / <C>reconnecting</C></>, "建立或恢复连接中"],
                [<C>risk</C>, "状态暂时无法确认，节点在退避重试"],
                [<C>ended</C>, "已下播并停止检测（订阅随之结束）"],
                [<><C>stopped</C> / <C>notfound</C></>, "管理员暂停 / 直播间不存在"],
              ]}
            />
            <CodeBlock tabs={examples.status(base)} />
          </>
        ),
      },
      {
        id: "stream",
        title: "实时推送",
        icon: RadioTower,
        tone: "social",
        summary: "一条 WebSocket 收本接入方全部订阅的消息、状态心跳和控制通知。",
        method: "WS",
        path: "/v1/stream",
        body: () => (
          <>
            <Table
              head={["查询参数", "说明"]}
              rows={[
                ["（不带参数）", "只接收之后的新事件"],
                [<C>subscription</C>, "只接收某一个订阅的事件"],
                [<><C>boot_id</C> + <C>since_seq</C></>, "断线重连时补回缺失的事件；首次连接可带 since_seq=0 补回订阅到连接之间的事件"],
              ]}
            />
            <P>握手同样使用 <C>Authorization</C> 请求头。节点每 15 秒发一次 Ping，回 Pong 即自动续租。每条订阅默认每 60 秒收到一条 <C>status</C> 心跳（<C>extra.code=heartbeat</C>），表示“房间仍由节点管理”。</P>
            <CodeBlock title="事件格式" tabs={examples.events()} />
            <Table
              head={["控制通知（extra.code）", "含义", "怎么处理"]}
              rows={[
                [<C>hello</C>, "连接建立，带 boot_id 与当前 seq", "boot_id 变了就把保存的游标清零"],
                [<C>gap</C>, "有一段事件无法补齐", "记录缺口，必要时提示“期间可能漏消息”"],
                [<C>backpressure</C>, "发送太慢，队列丢弃了部分低优先级事件", "加快处理速度，按缺口处理"],
                [<C>subscription_released</C>, <>订阅结束，<C>extra.reason</C> 说明原因</>, <><C>live_ended</C> 为本场结束；其他原因见“错误与重试”</>],
              ]}
            />
            <Note>控制通知没有顶层 subscription_id，不要用它们推进补拉游标。</Note>
          </>
        ),
      },
    ],
  },
  {
    title: "开发与运维",
    sections: [
      {
        id: "events",
        title: "消息类型",
        icon: Boxes,
        tone: "chat",
        summary: "每类消息带哪些字段，哪些适合进 AI 回复队列。",
        body: () => (
          <Table
            head={["type", "内容", "常用字段", "建议"]}
            rows={[
              [<C>chat</C>, "弹幕", <><C>user.nickname</C> <C>content</C></>, "进入回复队列，提问优先"],
              [<C>member</C>, "进入直播间", <C>user.nickname</C>, "30 秒窗口合并欢迎，同一用户 5 分钟内不重复"],
              [<C>like</C>, "点赞", <><C>extra.count</C> <C>extra.total</C></>, "一般只更新统计"],
              [<C>social</C>, "关注 / 分享", <C>extra.action</C>, "30 秒窗口合并致谢；按 action 区分关注和分享"],
              [<C>gift</C>, "礼物", <><C>extra.gift_name</C> <C>extra.repeat_count</C></>, "游客身份收不到礼物"],
              [<C>room_stats</C>, "在线人数", <><C>extra.online</C> <C>extra.total_pv</C></>, "只更新统计，不进回复队列"],
              [<C>status</C>, "房间状态 / 心跳", <><C>extra.state</C> <C>extra.reason</C></>, "用于活性判断，不进回复队列"],
            ]}
          />
        ),
      },
      {
        id: "replay",
        title: "补拉、缺口与去重",
        icon: TimerReset,
        tone: "info",
        summary: "断线重连不丢消息的做法，以及补不回来时怎么处理。",
        body: () => (
          <>
            <Steps
              items={[
                { title: "按消息去重", body: <>用 <C>live_id + id</C> 做持久去重键。seq 只在同一个 boot_id 内有效，换节点后会不同。</> },
                { title: "处理成功再推进游标", body: <>只有带顶层 <C>subscription_id</C> 的事件推进游标；按节点和 boot_id 保存。</> },
                { title: "重连带上游标", body: <><C>?boot_id=…&since_seq=…</C>；节点每个房间缓存最近 60 秒、200 条，单次最多补 2000 条。</> },
                { title: "收到 gap 就记录", body: <>节点重启、断线太久或流量太大都可能补不齐，按缺口处理，不要假设已经补全。</> },
              ]}
            />
            <Note>使用 Go SDK 时，自动重连、续租、去重和缺口回调都已内置，见“客户端示例”。</Note>
          </>
        ),
      },
      {
        id: "errors",
        title: "错误与重试",
        icon: Siren,
        tone: "err",
        summary: "统一的错误格式、常见错误码，以及订阅结束的各种原因。",
        body: () => (
          <>
            <CodeBlock tabs={[{ label: "错误格式", code: `{"ok":false,"error":{"code":"capacity_exceeded","message":"节点容量已满","suggestion":"请分配到其他节点"},"request_id":"request-example"}` }]} />
            <Table
              head={["HTTP", "code", "说明", "建议"]}
              rows={[
                ["400", <C>invalid_request</C>, "参数、JSON 或游标格式有误", "修正后再试，不要重试原请求"],
                ["400", <C>invalid_live_id</C>, "直播间标识无效", "核对直播间号或链接"],
                ["401", <C>unauthorized</C>, "缺少 Key、Key 错误或接入方已停用", "检查配置；停用后需要重新启用并重新订阅"],
                ["403", <C>forbidden</C>, "来源 IP 不在允许范围", "核对出口 IP 与接入方网段"],
                ["404", <C>not_found</C>, "不存在、已过期或不属于自己", "重新订阅"],
                ["409", <C>capacity_exceeded</C>, "节点容量或接入方额度已满", "换节点，或调整额度"],
                ["409", <C>subscription_conflict</C>, "同房间已有不同参数的订阅", "先退订再订阅"],
                ["422", <C>upstream_unverified</C>, "无法可靠确认短号对应的直播间", "改用直播间号或完整链接"],
                ["429", <C>rate_limited</C>, "推送连接已达 8 条", "复用已有连接"],
                ["500", <C>internal_error</C>, "节点内部错误", "稍后重试，带 request_id 反馈"],
              ]}
            />
            <Table
              head={["subscription_released.reason", "含义"]}
              rows={[
                [<C>live_ended</C>, "本场直播已结束（下播 10 分钟内未重开）"],
                [<C>idle_release</C>, "长时间未开播"],
                [<C>lease_expired</C>, "租约到期未续"],
                [<C>unsubscribed</C>, "已退订"],
                [<C>room_removed</C>, "管理员删除了这个直播间"],
                [<><C>client_disabled</C> / <C>client_deleted</C></>, "接入方被停用或删除"],
              ]}
            />
          </>
        ),
      },
      {
        id: "clients",
        title: "客户端示例",
        icon: Route,
        tone: "gift",
        summary: "完整的订阅 + 收消息示例，可直接改成自己的服务。",
        body: (base) => (
          <>
            <CodeBlock title="订阅并接收消息" tabs={examples.full(base)} />
            <Note>Go SDK 在仓库的 <C>sdk/go</C> 目录，自带自动续租、断线重连、按消息 ID 去重和缺口回调；其他语言按上面的示例自行处理重连与去重。</Note>
          </>
        ),
      },
      {
        id: "failover",
        title: "什么时候换节点",
        icon: KeyRound,
        tone: "warn",
        summary: "只在“采集中却长时间没有任何事件”时迁移，频繁换节点反而增加风控风险。",
        body: () => (
          <>
            <Table
              head={["最新状态", "建议"]}
              rows={[
                [<C>collecting</C>, "超过 210 秒没有该房间的任何事件（含心跳），再考虑换节点"],
                [<C>waiting</C>, "正在等开播或下播观察期，不要迁移"],
                [<C>probing</C>, "等确认结果，反复换节点会重复探测"],
                [<>节点 <C>cooldown</C></>, "不分配新房间；已在采集的不必搬走"],
                [<><C>risk</C> / <C>reconnecting</C></>, "观察原节点恢复，长期异常再告警"],
              ]}
            />
            <Note tone="warn">迁移时先在新节点订阅，切换本地归属，再退订旧订阅；切换期间的重复消息由去重挡住。同一房间迁移后至少观察 5 分钟再考虑下一次。</Note>
          </>
        ),
      },
    ],
  },
];

const ALL = GROUPS.flatMap((g) => g.sections);

/* ---------------- 页面 ---------------- */

export function DocsPage() {
  const base = useMemo(() => `${window.location.origin}/v1`, []);
  const [active, setActive] = useState(ALL[0].id);
  const [copied, setCopied] = useState(false);

  // 目录高亮当前阅读的章节
  useEffect(() => {
    const els = ALL.map((s) => document.getElementById(`doc-${s.id}`)).filter((e): e is HTMLElement => !!e);
    const io = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]) setActive(visible[0].target.id.slice(4));
      },
      { rootMargin: "-90px 0px -65% 0px" },
    );
    els.forEach((e) => io.observe(e));
    return () => io.disconnect();
  }, []);

  const jump = (id: string) => {
    document.getElementById(`doc-${id}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
    setActive(id);
  };

  return (
    <>
      <PageHeader
        icon={BookOpen}
        tone="info"
        title="接入文档"
        desc="给接入方开发者：订阅直播间、接收实时消息、按场次结束"
        actions={
          <PillButton tone="primary" icon={KeyRound} onClick={() => navigate("clients")}>
            管理 API Key
          </PillButton>
        }
      />

      {/* 关键信息一行看全 */}
      <div className="mb-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Card className="flex min-w-0 items-center gap-3 p-4 xl:col-span-2">
          <IconTile icon={RadioTower} tone="chat" size={36} />
          <div className="min-w-0 flex-1">
            <div className="text-xs text-muted-foreground">接口地址</div>
            <div className="num mt-0.5 truncate text-sm font-semibold">{base}</div>
          </div>
          <button
            type="button"
            onClick={async () => {
              try {
                await navigator.clipboard.writeText(base);
                setCopied(true);
                window.setTimeout(() => setCopied(false), 1500);
              } catch {
                setCopied(false);
              }
            }}
            className={cn("inline-flex shrink-0 cursor-pointer items-center gap-1 rounded-full px-3 py-1.5 text-xs font-medium", copied ? "tint-ok" : "border hover:bg-accent")}
          >
            {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
            {copied ? "已复制" : "复制"}
          </button>
        </Card>
        {[
          { icon: ShieldCheck, tone: "ok" as Tone, label: "鉴权", value: "Bearer API Key" },
          { icon: Layers, tone: "member" as Tone, label: "接口版本", value: "v1 · JSON + WebSocket" },
        ].map((x) => (
          <Card key={x.label} className="flex min-w-0 items-center gap-3 p-4">
            <IconTile icon={x.icon} tone={x.tone} size={36} />
            <div className="min-w-0">
              <div className="text-xs text-muted-foreground">{x.label}</div>
              <div className="mt-0.5 truncate text-sm font-semibold">{x.value}</div>
            </div>
          </Card>
        ))}
      </div>

      {/* 窄屏：目录改成横向滚动的章节标签 */}
      <div className="scrollbar-none -mx-1 mb-3 flex gap-1.5 overflow-x-auto px-1 lg:hidden">
        {ALL.map((s) => (
          <button
            key={s.id}
            type="button"
            onClick={() => jump(s.id)}
            className={cn("shrink-0 cursor-pointer rounded-full border px-3 py-1.5 text-xs", active === s.id ? "bg-primary font-medium text-primary-foreground" : "bg-card text-muted-foreground")}
          >
            {s.title}
          </button>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-[220px_minmax(0,1fr)]">
        {/* 目录 */}
        <nav aria-label="文档目录" className="hidden lg:block">
          <div className="sticky top-20 space-y-4 rounded-3xl border bg-card p-3">
            {GROUPS.map((g) => (
              <div key={g.title}>
                <div className="px-2 pb-1.5 text-[11px] font-medium text-faint">{g.title}</div>
                <div className="space-y-0.5">
                  {g.sections.map((s) => (
                    <button
                      key={s.id}
                      type="button"
                      onClick={() => jump(s.id)}
                      className={cn(
                        "flex w-full cursor-pointer items-center gap-2 rounded-xl px-2 py-1.5 text-left text-sm transition-colors",
                        active === s.id ? "bg-accent font-medium text-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
                      )}
                    >
                      {s.method ? (
                        <span className={cn("num w-10 shrink-0 text-[10px] font-bold", `text-ink-${METHOD_TONE[s.method]}`)}>{s.method}</span>
                      ) : (
                        <s.icon className="size-3.5 shrink-0" />
                      )}
                      <span className="truncate">{s.title}</span>
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </nav>

        {/* 正文 */}
        <div className="min-w-0 space-y-4">
          {GROUPS.map((g) => (
            <div key={g.title} className="space-y-4">
              <div className="px-1 pt-2 text-xs font-medium tracking-wide text-faint">{g.title}</div>
              {g.sections.map((s) => (
                <Card key={s.id} id={`doc-${s.id}`} className="scroll-mt-20 p-4 sm:p-6">
                  <div className="flex items-start gap-3">
                    <IconTile icon={s.icon} tone={s.tone} size={40} />
                    <div className="min-w-0 flex-1">
                      <h2 className="text-lg font-semibold">{s.title}</h2>
                      <p className="mt-0.5 text-sm text-muted-foreground">{s.summary}</p>
                    </div>
                  </div>
                  {s.method && s.path && (
                    <div className="mt-4 flex flex-wrap items-center gap-2 rounded-2xl border bg-background/40 px-3 py-2">
                      <Method m={s.method} />
                      <code className="num text-sm font-medium">{s.path}</code>
                      <span className="ml-auto text-xs text-muted-foreground">Authorization: Bearer</span>
                    </div>
                  )}
                  <div className="mt-4 space-y-4">{s.body(base)}</div>
                </Card>
              ))}
            </div>
          ))}
          <p className="px-1 pb-4 text-xs text-faint">完整字段说明见仓库 docs/api.md 与 docs/integration-guide.md，本页与之保持一致。</p>
        </div>
      </div>
    </>
  );
}
