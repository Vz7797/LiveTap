import { AlertTriangle, BookOpen, Check, ChevronDown, Copy, Inbox, KeyRound, Link2, Plug, Plus, Radio, ShieldBan, Shield, Sparkles, Timer, Upload, X, Zap, type LucideIcon } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useMemo, useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { navigate, setParams, useRoute } from "@/app/router";
import { useToast } from "@/app/toast";
import { Avatar, Card, CardHeader, IconTile, Meter, Num, PageHeader, PillButton, Portal, StatusPill, Tag } from "@/components/kit";
import { useProgressiveCount } from "@/lib/hooks/use-progressive-count";
import { Field, SearchInput, Segmented, TextInput } from "@/components/kit/form";
import { Switch } from "@/components/motion/switch";
import { PRE_LIVE, isLive, type Tone } from "@/lib/meta";
import { fmtAgo } from "@/lib/format";
import { cn } from "@/lib/utils";
import { refresh, useSim } from "@/data/runtime";
import { api } from "@/data/api";
import type { Client } from "@/data/types";

const ENV_TONE = { 生产: "chat", 预发: "info", 测试: "idle" } as const;

export function ClientsPage() {
  const { params } = useRoute();
  const clients = useSim((s) => s.clients);
  const createOpen = params.get("create") === "1";

  return (
    <>
      <PageHeader
        icon={KeyRound}
        tone="gift"
        title="接入方"
        desc="各业务系统通过 API Key 调用北向接口，Key 只在创建时显示一次"
        actions={
          <PillButton tone="primary" icon={Plus} onClick={() => setParams({ create: "1" })}>
            创建 API Key
          </PillButton>
        }
      />

      <TrafficStrip />

      <div className="mt-4 space-y-4">
        {!clients.length && <EmptyClients />}
        {clients.map((c) => (
          <ClientCard key={c.id} c={c} defaultOpen={clients.length === 1} />
        ))}
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-2">
        <Card className="p-4 sm:p-5">
          <CardHeader icon={Plug} tone="member" title="接入地址" desc="北向接口 /v1，请求头 Authorization: Bearer" />
          <div className="mt-4 grid gap-2 font-mono text-xs sm:grid-cols-2">
            {[
              ["GET", "/v1/node", "info"],
              ["POST", "/v1/subscriptions", "ok"],
              ["DELETE", "/v1/subscriptions/{id}", "err"],
              ["POST", "/v1/rooms/status:batch", "ok"],
              ["WS", "/v1/stream?since_seq=", "social"],
            ].map(([m, p, t]) => (
              <div key={p} className="flex min-w-0 items-center gap-2 rounded-xl bg-muted/60 px-2.5 py-2">
                <span className={cn("w-14 shrink-0 rounded-md px-1.5 py-0.5 text-center text-[10px] font-semibold", `tint-${t}`)}>{m}</span>
                <span className="truncate">{p}</span>
              </div>
            ))}
          </div>
          <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
            事件按统一格式推送，<span className="num">id</span> 用于去重，<span className="num">seq</span> 用于断线补拉（最近 60 秒）。订阅带 120 秒租约，推送连接在线时自动续期；直播结束 10 分钟后节点以 <span className="num">live_ended</span> 结束订阅，下次开播重新订阅。
          </p>
        </Card>
        <Card className="p-4 sm:p-5">
          <CardHeader icon={BookOpen} tone="info" title="背压策略" desc="每个连接发送队列上限 2000 条或 4 MB，满了按顺序丢弃" />
          <ol className="mt-4 grid gap-2 text-sm sm:grid-cols-2">
            {[
              ["点赞、在线人数", "like", "最先丢弃"],
              ["进场", "member", "其次"],
              ["礼物、关注", "gift", "再次"],
              ["弹幕", "chat", "最后才丢"],
            ].map(([label, tone, hint], i) => (
              <li key={label} className="flex items-center gap-2.5 rounded-xl bg-muted/40 px-2.5 py-2">
                <span className={cn("num inline-flex size-6 items-center justify-center rounded-lg text-xs font-semibold", `tint-${tone}`)}>{i + 1}</span>
                <span>{label}</span>
                <span className="ml-auto text-xs text-muted-foreground">{hint}</span>
              </li>
            ))}
          </ol>
          <p className="mt-3 text-xs leading-relaxed text-muted-foreground">状态消息不参与丢弃；发生丢弃时推送 backpressure 通知，接入方按缺口处理。</p>
        </Card>
      </div>

      <Portal>
        <AnimatePresence>{createOpen && <CreateKeyDialog onClose={() => setParams({ create: undefined })} />}</AnimatePresence>
      </Portal>
    </>
  );
}

/* ---------------- 顶部：全节点北向流量 ---------------- */

function lagPair(p50: number, p95: number) {
  return p95 < 1000 ? `${p50} / ${p95} ms` : `${(p50 / 1000).toFixed(1)} / ${(p95 / 1000).toFixed(1)} 秒`;
}

function TrafficStrip() {
  const traffic = useSim((s) => s.node.northbound);
  const push = useSim((s) => s.node.latency?.push);
  const subs = useSim((s) => s.clients.reduce((n, c) => n + c.used, 0));
  const dropped = traffic?.dropped ?? 0;
  const tiles: { icon: LucideIcon; tone: Tone; label: string; value: ReactNode; hint: string }[] = [
    { icon: Upload, tone: "gift", label: "北向出站", value: <>{((traffic?.bytes_per_sec ?? 0) / 1024).toFixed(1)}<small> KB/s</small></>, hint: "最近 60 秒平均" },
    { icon: Zap, tone: "chat", label: "推送速率", value: <>{(traffic?.push_per_sec ?? 0).toFixed(1)}<small> 条/秒</small></>, hint: "全部接入方合计" },
    { icon: Radio, tone: "info", label: "推送连接", value: traffic?.connections ?? 0, hint: "在线的 WebSocket" },
    { icon: Link2, tone: "member", label: "有效订阅", value: subs, hint: "未过期的订阅数" },
    { icon: Timer, tone: "social", label: "推送延迟", value: push?.samples ? lagPair(push.p50_ms, push.p95_ms) : "—", hint: "中位 / 95%，本节点 → 接入方" },
    { icon: dropped ? AlertTriangle : Inbox, tone: dropped ? "warn" : "ok", label: "排队 / 丢弃", value: <>{traffic?.queue ?? 0}<small> / </small><span className={dropped ? "text-ink-warn" : undefined}>{dropped}</span></>, hint: dropped ? "发生过丢弃，接入方会收到缺口通知" : "没有积压" },
  ];
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
      {tiles.map((t) => (
        <Card key={t.label} className="flex min-w-0 items-start gap-3 p-4">
          <IconTile icon={t.icon} tone={t.tone} size={34} />
          <div className="min-w-0">
            <div className="text-xs text-muted-foreground">{t.label}</div>
            <div className="num mt-0.5 truncate text-lg font-semibold [&_small]:text-xs [&_small]:font-normal [&_small]:text-muted-foreground">{t.value}</div>
            <div className="mt-0.5 truncate text-[11px] text-faint" title={t.hint}>{t.hint}</div>
          </div>
        </Card>
      ))}
    </div>
  );
}

function EmptyClients() {
  return (
    <Card className="flex flex-col items-center gap-3 border-dashed px-6 py-12 text-center">
      <IconTile icon={KeyRound} tone="gift" size={48} />
      <div className="text-base font-semibold">还没有接入方</div>
      <p className="max-w-md text-sm text-muted-foreground">为每个业务系统实例创建一个 API Key，它就能订阅商家的直播间、实时接收弹幕和进场等消息。</p>
      <PillButton tone="primary" icon={Plus} onClick={() => setParams({ create: "1" })}>
        创建 API Key
      </PillButton>
    </Card>
  );
}

/* ---------------- 接入方：一行概览，可展开订阅明细 ---------------- */

function ClientCard({ c, defaultOpen }: { c: Client; defaultOpen: boolean }) {
  const toast = useToast();
  const [confirm, setConfirm] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [open, setOpen] = useState(defaultOpen);
  const mutate = async (remove: boolean, enabled = c.enabled) => {
    setPending(true); setError("");
    try {
      await api(`/clients/${encodeURIComponent(c.id)}`, remove ? "DELETE" : "PUT", remove ? undefined : { name: c.name, env: c.env, quota: c.quota, whitelist: c.whitelist, enabled });
      await refresh();
      toast.show({ title: remove ? "接入方已删除，Key 已失效" : enabled ? "接入方已启用" : "接入方已停用，订阅和连接已撤销", status: "success" });
    } catch (e) { setError(e instanceof Error ? e.message : "操作失败，请重试"); }
    finally { setPending(false); }
  };
  const online = c.connections > 0;
  const nearFull = c.used >= c.quota * 0.85;
  return (
    <Card className={cn("overflow-hidden", !c.enabled && "opacity-75")}>
      <div className="grid gap-4 p-4 sm:p-5 lg:grid-cols-[minmax(240px,1.2fr)_minmax(0,2fr)_auto] lg:items-center">
        <div className="flex min-w-0 items-center gap-3">
          <span className="relative">
            <IconTile icon={KeyRound} tone="gift" size={44} />
            <span className={cn("absolute -right-0.5 -bottom-0.5 size-3 rounded-full ring-2 ring-card", online ? "bg-ok" : "bg-idle")} />
          </span>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate font-semibold">{c.name}</span>
              <Tag tone={ENV_TONE[c.env]}>{c.env}</Tag>
            </div>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
              <span className={online ? "text-ink-ok" : undefined}>{online ? `在线 · ${c.connections} 条推送连接` : c.enabled ? "离线" : "已停用"}</span>
              <span className="text-faint">·</span>
              <span className="num">{c.keyPrefix}</span>
              <span className="text-faint">·</span>
              <span>{c.lastSeen ? `最后使用 ${fmtAgo(c.lastSeen)}` : "尚未使用"}</span>
            </div>
          </div>
        </div>

        <div className="grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-4">
          <Metric label="推送" value={<><Num value={c.pushPerSec} format={(n) => n.toFixed(1)} /><small> 条/秒</small></>} />
          <Metric label="出站" value={<>{((c.bytesPerSec ?? 0) / 1024).toFixed(1)}<small> KB/s</small></>} />
          <Metric label="排队 / 丢弃" value={<>{c.queue}<small> / </small><span className={c.dropped ? "text-ink-warn" : undefined}>{c.dropped}</span></>} />
          <div className="min-w-0">
            <div className="flex items-baseline justify-between text-xs text-muted-foreground">
              <span>订阅额度</span>
              <span className="num"><span className={cn("font-semibold text-foreground", nearFull && "text-ink-warn")}>{c.used}</span> / {c.quota}</span>
            </div>
            <Meter value={c.used} max={c.quota} tone={nearFull ? "warn" : "info"} className="mt-2" />
          </div>
        </div>

        <div className="flex items-center gap-2 lg:justify-end">
          <Switch checked={c.enabled} disabled={pending} onCheckedChange={(v) => { if (!pending) void mutate(false, v); }} ariaLabel={`启用 ${c.name}`} />
          <button
            type="button"
            onClick={() => {
              if (!confirm) {
                setConfirm(true);
                window.setTimeout(() => setConfirm(false), 3000);
                return;
              }
              void mutate(true);
            }}
            className={cn("inline-flex h-8 cursor-pointer items-center gap-1 rounded-full px-3 text-xs font-medium", confirm ? "bg-err text-white" : "tint-err")}
            disabled={pending}
          >
            <ShieldBan className="size-3.5" />
            {confirm ? "确认吊销" : "吊销"}
          </button>
        </div>
      </div>
      {error && <p role="alert" className="-mt-2 px-5 pb-3 text-sm text-ink-err">{error}</p>}

      <div className="flex flex-wrap items-center gap-2 border-t bg-muted/20 px-4 py-2.5 sm:px-5">
        <Shield className="size-3.5 text-muted-foreground" />
        {c.whitelist.length ? (
          c.whitelist.map((ip) => (
            <span key={ip} className="num rounded-md bg-muted px-1.5 py-0.5 text-[11px]">{ip}</span>
          ))
        ) : (
          <span className="text-[11px] text-ink-warn">未限制来源 IP，建议填写业务系统服务器的出口地址</span>
        )}
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className="ml-auto inline-flex cursor-pointer items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          订阅明细 <span className="num">{c.used}</span>
          <ChevronDown className={cn("size-3.5 transition-transform duration-150", open && "rotate-180")} />
        </button>
      </div>
      {open && <Subscriptions client={c} />}
    </Card>
  );
}

function Metric({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="num mt-0.5 truncate text-base font-semibold [&_small]:text-xs [&_small]:font-normal [&_small]:text-muted-foreground">{value}</div>
    </div>
  );
}

interface SubscriptionRow { id: string; live_id: string; external_ref: string; created_at: string; expires_at: string }

/** 这个接入方订阅了哪些直播间、对应哪个商家（external_ref）、现在是什么状态 */
function Subscriptions({ client }: { client: Client }) {
  const subs = useQuery({
    queryKey: ["client-subscriptions", client.id],
    queryFn: () => api<SubscriptionRow[]>(`/clients/${encodeURIComponent(client.id)}/subscriptions`),
    refetchInterval: 5000,
  });
  const rooms = useSim((s) => s.rooms);
  const byId = useMemo(() => new Map(rooms.map((r) => [r.liveId, r])), [rooms]);
  const [q, setQ] = useState("");
  const rows = useMemo(() => {
    const kw = q.trim().toLowerCase();
    return (Array.isArray(subs.data) ? subs.data : [])
      .map((s) => ({ s, room: byId.get(s.live_id) }))
      .filter(({ s, room }) => !kw || s.live_id.includes(kw) || s.external_ref.toLowerCase().includes(kw) || (room?.name ?? "").toLowerCase().includes(kw));
  }, [subs.data, byId, q]);
  const states = useMemo(() => {
    const c = { collecting: 0, waiting: 0, other: 0 };
    for (const { room } of rows) {
      if (room && isLive(room.status)) c.collecting++;
      else if (room && PRE_LIVE.includes(room.status)) c.waiting++;
      else c.other++;
    }
    return c;
  }, [rows]);
  const shown = rows.slice(0, useProgressiveCount(rows.length, 40));

  return (
    <div className="border-t">
      <div className="flex flex-wrap items-center gap-2 px-4 py-3 sm:px-5">
        <Tag tone="ok">采集中 {states.collecting}</Tag>
        <Tag tone="member">等待 / 探测 {states.waiting}</Tag>
        {states.other > 0 && <Tag tone="warn">其他 {states.other}</Tag>}
        <SearchInput value={q} onChange={setQ} placeholder="直播间、商家标识" className="ml-auto w-full sm:w-64" />
      </div>
      {subs.isError && <p className="px-5 pb-3 text-sm text-ink-warn">订阅明细暂时无法读取，正在重试</p>}
      {!subs.isPending && rows.length === 0 && <p className="px-5 pb-5 text-sm text-muted-foreground">{q ? "没有匹配的订阅" : "这个接入方当前没有订阅；业务系统在开播时创建订阅，直播结束后自动结束。"}</p>}
      {rows.length > 0 && (
        <div className="scroll-area max-h-[440px] pb-2">
          <table className="w-full min-w-[640px] text-sm">
            <thead className="sticky top-0 z-[1] bg-card text-left text-xs text-muted-foreground">
              <tr className="border-b">
                <th className="px-5 py-2 font-medium">直播间</th>
                <th className="px-3 py-2 font-medium">业务标识</th>
                <th className="px-3 py-2 font-medium">状态</th>
                <th className="px-3 py-2 font-medium">订阅于</th>
                <th className="px-5 py-2 text-right font-medium">租约</th>
              </tr>
            </thead>
            <tbody>
              {shown.map(({ s, room }) => {
                const left = Math.max(0, Math.round((Date.parse(s.expires_at) - Date.now()) / 1000));
                return (
                  <tr key={s.id} className="cursor-pointer border-b last:border-0 hover:bg-accent/50" onClick={() => navigate("rooms", { room: s.live_id })} style={ROW_STYLE}>
                    <td className="px-5 py-2">
                      <div className="flex min-w-0 items-center gap-2.5">
                        <Avatar name={room?.name || s.live_id} src={room?.avatar} seed={s.live_id} size={28} live={!!room && isLive(room.status)} />
                        <div className="min-w-0">
                          <div className="max-w-[220px] truncate">{room?.name || "—"}</div>
                          <div className="num text-[11px] text-muted-foreground">{s.live_id}</div>
                        </div>
                      </div>
                    </td>
                    <td className="num max-w-[220px] truncate px-3 py-2 text-xs" title={s.external_ref}>{s.external_ref || <span className="text-faint">未填写</span>}</td>
                    <td className="px-3 py-2">{room ? <StatusPill status={room.status} size="sm" /> : <span className="text-xs text-faint">—</span>}</td>
                    <td className="px-3 py-2 text-xs text-muted-foreground">{fmtAgo(Date.parse(s.created_at))}</td>
                    <td className={cn("num px-5 py-2 text-right text-xs", left < 30 ? "text-ink-warn" : "text-muted-foreground")}>{left} 秒</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

const ROW_STYLE = { contentVisibility: "auto", containIntrinsicSize: "auto 45px" } as const;

function CreateKeyDialog({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const [name, setName] = useState("");
  const [env, setEnv] = useState<Client["env"]>("生产");
  const [quota, setQuota] = useState("50");
  const [ips, setIps] = useState("");
  const [key, setKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");

  const create = async () => {
    setPending(true); setError("");
    try {
      const value = await api<{ api_key: string }>("/clients", "POST", { name, env, quota: Number(quota), whitelist: ips.split(/[\s,，]+/).filter(Boolean), enabled: true });
      setKey(value.api_key); // Component state only: never cache or persist the key.
      await refresh();
    } catch (e) { setError(e instanceof Error ? e.message : "创建失败，请重试"); }
    finally { setPending(false); }
  };

  return (
    <>
      <motion.div className="fixed inset-0 z-[70] bg-black/30 dark:bg-black/50" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={key || pending ? undefined : onClose} />
      <div className="pointer-events-none fixed inset-0 z-[71] flex items-start justify-center p-3 pt-[10vh]">
        <motion.div
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, y: 8 }}
          transition={{ type: "tween", duration: 0.18, ease: "easeOut" }}
          style={{ borderRadius: 28 }}
          className="scroll-area overscroll-contain pointer-events-auto max-h-[80dvh] w-full max-w-lg border border-border-strong bg-popover shadow-2xl"
        >
          <AnimatePresence mode="popLayout" initial={false}>
            {!key ? (
              <motion.div key="form" initial={{ opacity: 0, x: -20 }} animate={{ opacity: 1, x: 0 }} exit={{ opacity: 0, x: -20 }} className="p-5 sm:p-6">
                <div className="flex items-start gap-3">
                  <IconTile icon={KeyRound} tone="gift" size={44} className="rounded-2xl" />
                  <div className="flex-1">
                    <h2 className="text-lg font-semibold">创建 API Key</h2>
                    <p className="text-sm text-muted-foreground">库中只保存 SHA-256 摘要</p>
                  </div>
                  <button type="button" onClick={onClose} disabled={pending} className="cursor-pointer rounded-full p-2 text-muted-foreground hover:bg-accent" aria-label="关闭">
                    <X className="size-4" />
                  </button>
                </div>
                <div className="mt-5 space-y-4">
                  <Field label="接入方名称">
                    <TextInput aria-label="接入方名称" value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />
                  </Field>
                  <Field label="环境">
                    <Segmented<Client["env"]>
                      value={env}
                      onChange={setEnv}
                      size="sm"
                      options={[
                        { value: "生产", label: "生产" },
                        { value: "预发", label: "预发" },
                        { value: "测试", label: "测试" },
                      ]}
                    />
                  </Field>
                  <div className="grid gap-4 sm:grid-cols-2">
                    <Field label="直播间额度" hint="可订阅的直播间数上限">
                      <TextInput aria-label="直播间额度" value={quota} onChange={(e) => setQuota(e.target.value.replace(/\D/g, ""))} className="num" />
                    </Field>
                    <Field label="来源 IP 白名单" hint="留空不限制；多个 CIDR 用空格或逗号分隔">
                      <TextInput aria-label="来源 IP 白名单" placeholder="例如 10.0.12.10/32" value={ips} onChange={(e) => setIps(e.target.value)} className="num" />
                    </Field>
                  </div>
                </div>
                {error && <p role="alert" className="mt-3 text-sm text-ink-err">{error}</p>}
                <div className="mt-6 flex justify-end gap-2">
                  <PillButton tone="ghost" onClick={onClose} disabled={pending}>
                    取消
                  </PillButton>
                  <PillButton tone="primary" icon={Sparkles} onClick={create} disabled={pending || !name.trim() || Number(quota) < 1 || Number(quota) > 2000}>
                    {pending ? "正在创建…" : "生成 Key"}
                  </PillButton>
                </div>
              </motion.div>
            ) : (
              <motion.div key="key" initial={{ opacity: 0, x: 20 }} animate={{ opacity: 1, x: 0 }} className="p-5 sm:p-6">
                <div className="flex flex-col items-center text-center">
                  <motion.span
                    initial={{ opacity: 0, y: 8 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ type: "tween", duration: 0.18, ease: "easeOut" }}
                    className="inline-flex size-14 items-center justify-center rounded-2xl tint-ok"
                  >
                    <Check className="size-7" strokeWidth={2.6} />
                  </motion.span>
                  <h2 className="mt-3 text-lg font-semibold">Key 已生成</h2>
                  <p className="text-sm text-muted-foreground">{name}</p>
                </div>
                <div className="mt-5 flex items-center gap-2 rounded-2xl border bg-background/60 p-2 pl-3">
                  <code data-private="api-key" className="num min-w-0 flex-1 break-all text-sm">{key}</code>
                  <button
                    type="button"
                    onClick={async () => {
                      try { await navigator.clipboard.writeText(key); setCopied(true); toast.show({ title: "已复制到剪贴板", status: "success" }); }
                      catch { setError("复制失败，请手动选中并保存密钥"); }
                    }}
                    className={cn("inline-flex h-8 shrink-0 cursor-pointer items-center gap-1 rounded-full px-3 text-xs font-medium", copied ? "tint-ok" : "bg-primary text-primary-foreground")}
                  >
                    {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
                    {copied ? "已复制" : "复制"}
                  </button>
                </div>
                <div className="mt-3 flex items-start gap-2 rounded-2xl tint-warn p-3 text-xs leading-relaxed">
                  <AlertTriangle className="mt-0.5 size-4 shrink-0" />
                  <span>这个 Key 只显示这一次，关闭后无法再查看。请立即保存到业务系统的配置中；丢失只能吊销后重新创建。</span>
                </div>
                <div className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
                  <Radio className="size-3.5" /> 仅保存在当前弹窗，关闭后无法再次查看
                </div>
                {error && <p role="alert" className="mt-3 text-sm text-ink-err">{error}</p>}
                <div className="mt-6 flex justify-end">
                  <PillButton tone="primary" onClick={onClose}>
                    我已保存
                  </PillButton>
                </div>
              </motion.div>
            )}
          </AnimatePresence>
        </motion.div>
      </div>
    </>
  );
}
