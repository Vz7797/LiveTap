import { RoomSettings } from "./RoomSettings";
import { useQuery } from "@tanstack/react-query";
import { api } from "@/data/api";
import type { FeedMsg } from "@/data/types";
import { describeDrops } from "@/lib/drops";
import {
  Activity,
  BarChart3,
  Check,
  CircleDot,
  Copy,
  Hourglass,
  Pause,
  Play,
  PlugZap,
  RadioTower,
  RefreshCw,
  Settings2,
  ShieldAlert,
  Trash2,
  TriangleAlert,
  Users,
  Wifi,
  WifiOff,
  X,
  Zap,
  MessagesSquare,
  Unplug,
  Search,
} from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useToast } from "@/app/toast";
import { Legend, MinuteBars, type SeriesDef } from "@/components/charts";
import { LiveFeed } from "@/components/feed";
import { Avatar, IconButton, IconTile, Num, PillButton, StatusPill, Tag } from "@/components/kit";
import { Segmented, ToggleChip } from "@/components/kit/form";
import { fmtAgo, fmtClock, fmtNum } from "@/lib/format";
import { MSG_TYPES, SOURCE_META, STATUS_META, TYPE_META, withFallback, type MsgType, type Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";
import { sim } from "@/data/runtime";
import { useRoomDetail } from "@/data/detail";
import type { Room, RoomEvent } from "@/data/types";
import { MORPH_SPRING } from "./RoomCard";
import { DouyinLink } from "@/components/kit/douyin-link";

const SERIES: SeriesDef[] = MSG_TYPES.map((t) => ({ key: t, label: TYPE_META[t].label, tone: TYPE_META[t].tone }));

type TabKey = "feed" | "stats" | "timeline" | "settings";

export function RoomPanel({ room: liveRoom, onClose }: { room: Room; onClose: () => void; fromCard: boolean }) {
  // 推送只带精简字段；分钟统计、配置、完整时间线在面板打开时按需获取
  const room = useRoomDetail(liveRoom);
  const [tab, setTab] = useState<TabKey>("feed");
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [onClose]);

  return (
    <>
      <motion.button
        type="button"
        aria-label="关闭详情"
        className="fixed inset-0 z-[60] cursor-default bg-black/25 dark:bg-black/55"
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        exit={{ opacity: 0 }}
        transition={{ duration: 0.2 }}
        onClick={onClose}
      />
      <motion.aside
        transition={MORPH_SPRING}
        initial={{ opacity: 0, x: 20 }}
        animate={{ opacity: 1, x: 0 }}
        exit={{ opacity: 0, x: 20 }}
        style={{ borderRadius: 28 }}
        role="dialog"
        aria-modal="true"
        aria-label={`${room.name} 详情`}
        className="fixed inset-2 z-[61] flex flex-col overflow-hidden border border-border-strong bg-card shadow-2xl sm:inset-y-3 sm:right-3 sm:left-auto sm:w-[620px]"
      >
        <motion.div
          className="flex min-h-0 flex-1 flex-col"
          initial={{ opacity: 0 }}
          animate={{ opacity: 1, transition: { duration: 0.18 } }}
          exit={{ opacity: 0, transition: { duration: 0.08 } }}
        >
          <PanelHeader room={room} onClose={onClose} />
          <div className="px-5 pb-3">
            <Segmented<TabKey>
              value={tab}
              onChange={setTab}
              size="sm"
              options={[
                { value: "feed", label: "实时消息", icon: MessagesSquare },
                { value: "stats", label: "分钟统计", icon: BarChart3 },
                { value: "timeline", label: "重连时间线", icon: Activity },
                { value: "settings", label: "单独设置", icon: Settings2 },
              ]}
            />
          </div>
          <div className="scroll-area min-h-0 flex-1 overscroll-contain border-t px-5 pt-4 pb-8">
            <AnimatePresence mode="wait" initial={false}>
              <motion.div
                key={tab}
                initial={{ opacity: 0, y: 8 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -6 }}
                transition={{ duration: 0.18 }}
              >
                {tab === "feed" && <RoomFeed room={room} />}
                {tab === "stats" && <RoomStats room={room} />}
                {tab === "timeline" && <RoomTimeline room={room} />}
                {tab === "settings" && <RoomSettings room={room} />}
              </motion.div>
            </AnimatePresence>
          </div>
        </motion.div>
      </motion.aside>
    </>
  );
}

/* ---------------- 头部：身份 + 操作 + 关键指标 ---------------- */

function PanelHeader({ room, onClose }: { room: Room; onClose: () => void }) {
  const toast = useToast();
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [copied, setCopied] = useState(false);
  const m = STATUS_META[room.status];
  const live = room.status === "collecting";
  const stopped = room.status === "stopped";
  const ended = room.status === "ended";
  const silentFor = Math.round((Date.now() - room.lastMsgAt) / 1000);

  return (
    <div className="relative shrink-0 overflow-hidden px-5 pt-5 pb-4">
      <div className="relative flex items-start gap-3.5">
        <Avatar name={room.name} src={room.avatar} seed={room.liveId} size={56} live={live} dim={room.status === "notfound" || stopped || ended} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-lg font-semibold">{room.name}</h2>
            <StatusPill status={room.status} />
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <button
              type="button"
              className="num inline-flex cursor-pointer items-center gap-1 hover:text-foreground"
              onClick={() => {
                navigator.clipboard?.writeText(room.liveId).catch(() => {});
                setCopied(true);
                window.setTimeout(() => setCopied(false), 1400);
              }}
              title="复制直播间号"
            >
              web_rid {room.liveId}
              {copied ? <Check className="size-3 text-ink-ok" /> : <Copy className="size-3" />}
            </button>
            <DouyinLink liveId={room.liveId} variant="pill" />
            {room.city && (
              <>
                <span className="text-faint">·</span>
                <span>{room.city}</span>
              </>
            )}
            <Tag tone={SOURCE_META[room.source].tone}>{SOURCE_META[room.source].label}</Tag>
          </div>
          <p className="mt-1.5 truncate text-xs text-muted-foreground">{room.note ?? (ended ? room.reason || m.hint : m.hint)}</p>
          {(room.subscriptions?.length ?? 0) > 0 && (
            <div className="mt-2 flex flex-wrap gap-1.5">
              {room.subscriptions!.slice(0, 4).map((s, i) => (
                <span key={i} className="min-w-0" title={`${s.client}${s.externalRef ? " · " + s.externalRef : ""}`}>
                  <Tag tone="info" className="max-w-[220px] truncate">
                    {s.client}
                    {s.externalRef ? ` · ${s.externalRef}` : ""}
                  </Tag>
                </span>
              ))}
              {room.subscriptions!.length > 4 && <Tag>+{room.subscriptions!.length - 4}</Tag>}
            </div>
          )}
          {stopped && room.subscribers > 0 && (
            <p className="mt-2 rounded-xl tint-warn px-2.5 py-1.5 text-xs">有 {room.subscribers} 个接入方订阅在等这个直播间，暂停期间它们收不到消息</p>
          )}
        </div>
        <IconButton icon={X} label="关闭" onClick={onClose} className="-mt-1 -mr-2" />
      </div>

      <div className="relative mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <Kpi icon={Users} tone="member" label="在线人数">
          {room.online > 0 ? <Num value={room.online} /> : <span className="text-faint">—</span>}
        </Kpi>
        <Kpi icon={Zap} tone="chat" label="消息/分钟">
          {live ? <Num value={room.msgPerMin} /> : <span className="text-faint">—</span>}
        </Kpi>
        <Kpi icon={RefreshCw} tone={room.reconnects30m >= 3 ? "warn" : "info"} label="30 分钟重连">
          <Num value={room.reconnects30m} format={String} />
        </Kpi>
        <Kpi icon={Hourglass} tone="social" label="最近消息">
          {live ? <span className="num" title={`静默阈值 ${room.silenceThreshold} 秒`}>{!room.lastMsgAt ? "等待首条消息" : silentFor < 2 ? "刚刚" : `${silentFor}s 前`}</span> : <span className="text-faint">—</span>}
        </Kpi>
      </div>
      {!!room.drops?.total && (
        <p className="relative mt-2 rounded-xl tint-warn px-2.5 py-1.5 text-xs">
          入口丢弃 <span className="num">{room.drops.total}</span> 条（{describeDrops(room.drops)}）。消息涌入太快时先丢点赞等低价值消息，弹幕最后才丢
        </p>
      )}

      <div className="relative mt-4 flex flex-wrap items-center gap-2">
        <PillButton
          tone="neutral"
          icon={RefreshCw}
          disabled={stopped || room.status === "notfound"}
          onClick={async () => {
            if (await sim.reconnect(room.liveId)) toast.show({ title: "重连任务已提交", description: "请查看直播间状态确认连接结果", status: "info" });
          }}
        >
          重连
        </PillButton>
        {stopped || ended ? (
          <PillButton
            tone="primary"
            icon={Play}
            onClick={async () => {
              if (await sim.resume(room.liveId)) toast.show({ title: ended ? "已开始检测" : "恢复任务已提交", description: "按全局启动限速排队探测", status: "success" });
            }}
          >
            {ended ? "开始检测" : "恢复"}
          </PillButton>
        ) : (
          <PillButton
            tone="neutral"
            icon={Pause}
            disabled={room.status === "notfound"}
            onClick={async () => {
              if (await sim.pause(room.liveId)) toast.show({ title: `暂停任务已提交`, status: "info" });
            }}
          >
            暂停
          </PillButton>
        )}
        <motion.button
          layout
          type="button"
          onClick={async () => {
            if (!confirmRemove) {
              setConfirmRemove(true);
              window.setTimeout(() => setConfirmRemove(false), 3000);
              return;
            }
            if (!await sim.remove([room.liveId])) return;
            onClose();
            toast.show({ title: `已移除「${room.name}」`, description: "采集任务已释放", status: "success" });
          }}
          transition={{ type: "tween", duration: 0.18 }}
          className={cn(
            "ml-auto inline-flex h-9 cursor-pointer items-center gap-1.5 rounded-full px-4 text-sm font-medium",
            confirmRemove ? "bg-err text-white" : "tint-err",
          )}
        >
          {confirmRemove ? <TriangleAlert className="size-4" /> : <Trash2 className="size-4" />}
          <AnimatePresence mode="popLayout" initial={false}>
            <motion.span key={String(confirmRemove)} initial={{ opacity: 0, y: 6 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -6 }}>
              {confirmRemove ? "再点一次确认移除" : "移除"}
            </motion.span>
          </AnimatePresence>
        </motion.button>
      </div>
    </div>
  );
}

function Kpi({ icon, tone, label, children }: { icon: typeof Users; tone: Tone; label: string; children: React.ReactNode }) {
  return (
    <div className="rounded-2xl border bg-background/50 p-3">
      <div className="flex items-center gap-2">
        <IconTile icon={icon} tone={tone} size={24} className="rounded-lg" />
        <span className="truncate text-[11px] text-muted-foreground">{label}</span>
      </div>
      <div className="mt-1.5 text-lg font-semibold">{children}</div>
    </div>
  );
}

/* ---------------- 实时消息 ---------------- */

function RoomFeed({ room }: { room: Room }) {
  const events = useQuery({queryKey:["room-events",room.liveId],queryFn:()=>api<Array<{id:string;seq:number;type:string;live_id:string;content:string;received_at:string;user:{nickname:string;avatar?:string};extra:Record<string,unknown>}>>(`/rooms/${room.liveId}/events`),refetchInterval:2500});
  const msgs = useMemo<FeedMsg[]>(() => (events.data ?? []).filter(e=>MSG_TYPES.includes(e.type as MsgType)).map(e=>({id:e.id,seq:e.seq,type:e.type as MsgType,liveId:room.liveId,roomName:room.name,nickname:e.user.nickname,avatar:e.user.avatar,content:e.content,ts:Date.parse(e.received_at),extra:{gift:e.extra?.gift_name as string,count:(e.extra?.repeat_count ?? e.extra?.count) as number,action:e.extra?.action===1?"关注":"分享"}})), [events.data, room.liveId, room.name]);
  const [types, setTypes] = useState<MsgType[]>([...MSG_TYPES]);
  const shown = useMemo(() => msgs.filter((m) => types.includes(m.type)), [msgs, types]);
  return (
    <div>
      {events.isError && <p className="mb-2 text-xs text-warn">房间消息暂时无法读取，正在重试</p>}
      <div className="mb-2 flex flex-wrap gap-1.5">
        {MSG_TYPES.map((t) => {
          const meta = TYPE_META[t];
          return (
            <ToggleChip
              key={t}
              tone={meta.tone}
              icon={meta.icon}
              active={types.includes(t)}
              onClick={() => setTypes((cur) => (cur.includes(t) ? cur.filter((x) => x !== t) : [...cur, t]))}
              className="h-7"
            >
              {meta.label}
            </ToggleChip>
          );
        })}
      </div>
      <LiveFeed
        messages={shown}
        limit={40}
        resetKey={`${room.liveId}|${types.join()}`}
        emptyTitle={room.status === "collecting" ? "正在等待新消息…" : "当前未在采集"}
        emptyDesc={room.status === "collecting" ? "冷清直播间的弹幕可能很少，在线人数等系统消息仍在持续推送" : "直播间开播并连接成功后，这里会实时显示消息"}
      />
    </div>
  );
}

/* ---------------- 分钟统计 ---------------- */

function RoomStats({ room }: { room: Room }) {
  const sums = MSG_TYPES.map((t) => ({ t, v: room.minutes.reduce((a, m) => a + m[t], 0) }));
  return (
    <div className="space-y-5">
      <div>
        <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
          <div className="text-sm font-medium">最近 30 分钟 · 每分钟消息数</div>
          <Legend items={SERIES.map((s) => ({ label: s.label, tone: s.tone }))} />
        </div>
        <MinuteBars data={room.minutes} series={SERIES} height={170} />
      </div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
        {sums.map(({ t, v }) => {
          const meta = TYPE_META[t];
          const Icon = meta.icon;
          return (
            <div key={t} className="rounded-2xl border bg-background/50 p-3">
              <div className={cn("inline-flex size-7 items-center justify-center rounded-lg", TONE[meta.tone].tint)}>
                <Icon className="size-3.5" />
              </div>
              <div className="mt-2 text-[11px] text-muted-foreground">{meta.label}</div>
              <div className="num text-base font-semibold">{fmtNum(v)}</div>
            </div>
          );
        })}
      </div>
      <div className="rounded-2xl border bg-background/50 p-4 text-xs leading-relaxed text-muted-foreground">
        <div className="mb-1 flex items-center gap-1.5 text-sm font-medium text-foreground">
          <CircleDot className="size-4 text-ink-social" /> 静默检测
        </div>
        自适应阈值 = max(90 秒, 最近 30 分钟消息间隔 P99 × 4)，上限 300 秒。当前阈值
        <span className="num mx-1 font-medium text-foreground">{room.silenceThreshold}s</span>
        ，只看“任何消息”，弹幕为零不算异常。
      </div>
    </div>
  );
}

/* ---------------- 重连时间线 ---------------- */

const EVENT_META: Record<RoomEvent["kind"], { icon: typeof Wifi; tone: Tone; label: string }> = withFallback<RoomEvent["kind"], { icon: typeof Wifi; tone: Tone; label: string }>({
  ready: { icon: Wifi, tone: "ok", label: "已就绪" },
  online: { icon: RadioTower, tone: "info", label: "开播" },
  reconnect: { icon: RefreshCw, tone: "chat", label: "重连" },
  handshake_fail: { icon: WifiOff, tone: "err", label: "握手失败" },
  risk: { icon: ShieldAlert, tone: "warn", label: "状态待确认" },
  offline: { icon: Unplug, tone: "idle", label: "下播" },
  rebuild: { icon: PlugZap, tone: "social", label: "重建" },
  probe: { icon: Search, tone: "info", label: "探测" },
}, (key) => ({ icon: Search, tone: "idle", label: key || "状态变化" }));

function RoomTimeline({ room }: { room: Room }) {
  return (
    <ol className="relative space-y-1 before:absolute before:top-3 before:bottom-3 before:left-[17px] before:w-px before:bg-border">
      <AnimatePresence initial={false}>
        {room.events.map((e, i) => {
          const meta = EVENT_META[e.kind];
          const Icon = meta.icon;
          return (
            <li
              key={`${e.at}-${i}`}
              className="relative flex gap-3 rounded-xl p-1.5 hover:bg-accent/50"
            >
              <span className={cn("relative z-10 inline-flex size-[26px] shrink-0 items-center justify-center rounded-lg ring-4 ring-card", TONE[meta.tone].tint)}>
                <Icon className="size-3.5" />
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className={cn("text-xs font-medium", TONE[meta.tone].ink)}>{meta.label}</span>
                  <span className="num ml-auto text-[11px] text-faint">
                    {fmtClock(e.at)} · {fmtAgo(e.at)}
                  </span>
                </div>
                <p className="mt-0.5 text-sm">{e.text}</p>
              </div>
            </li>
          );
        })}
      </AnimatePresence>
    </ol>
  );
}


