import { Bot, Globe2, RefreshCw, UserRound, Users } from "lucide-react";
import { memo } from "react";
import { Sparkline } from "@/components/charts";
import { Avatar, Num, StatusPill, Tag } from "@/components/kit";
import { SOURCE_META, STATUS_META, isFinal, isLive, type Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";
import type { Room } from "@/data/types";
import { DouyinLink } from "@/components/kit/douyin-link";

// 详情只在打开、关闭时进行短暂淡入与位移。
export const MORPH_SPRING = { type: "tween" as const, duration: 0.18, ease: "easeOut" as const };

// memo：打开详情时只重渲染被点的那张卡，其余卡片不动，保证形变动画流畅
export const RoomCard = memo(function RoomCard({ room, onOpen }: { room: Room; onOpen: (liveId: string) => void }) {
  const m = STATUS_META[room.status];
  const live = isLive(room.status);
  const dead = isFinal(room.status);
  return (
    <div
      data-room-card={room.liveId}
      // 屏幕外的卡片跳过排版与绘制：100 间时进入页面不再一次性布局 7000 多个节点
      style={{ borderRadius: 24, contentVisibility: "auto", containIntrinsicSize: "auto 268px" }}
      className="group relative cursor-pointer overflow-hidden border bg-card shadow-[var(--shadow-card)] transition-colors hover:border-border-strong"
      onClick={() => onOpen(room.liveId)}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onOpen(room.liveId); } }}
      aria-label={`${room.name}，${m.label}，打开详情`}
    >
      <div className="relative p-4">
        <div className="flex items-start gap-3">
          <Avatar name={room.name} src={room.avatar} seed={room.liveId} size={44} live={live} dim={dead} />
          <div className="min-w-0 flex-1">
            <div className="truncate text-[15px] font-semibold">{room.name}</div>
            <div className="mt-0.5 flex items-center gap-1.5 text-[11px] text-muted-foreground">
              <span className="num">{room.liveId}</span>
              {room.city && <span className="text-faint">·</span>}
              <span>{room.city}</span>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <DouyinLink liveId={room.liveId} />
            <StatusPill status={room.status} size="sm" />
          </div>
        </div>

        <p className="mt-3 truncate text-xs text-muted-foreground">{room.title}</p>

        <div className="relative mt-3 -mx-1" title="近 30 分钟在线人数变化">
          <span className="pointer-events-none absolute top-0 left-1 z-10 text-[10px] text-faint">在线 · 30 分钟</span>
          <Sparkline data={room.onlineSpark} tone={live ? "info" : m.tone} height={44} fit />
        </div>

        <div className="mt-3 grid grid-cols-3 gap-2">
          <Stat label="在线" icon={Users} tone="info">
            {live || room.status === "reconnecting" ? <Num value={room.online} /> : <span className="text-faint">—</span>}
          </Stat>
          <Stat label="条/分" icon={Bot} tone="ok">
            {live ? <Num value={room.msgPerMin} /> : <span className="text-faint">—</span>}
          </Stat>
          <Stat label="重连" icon={RefreshCw} warn={room.reconnects30m >= 3}>
            <Num value={room.reconnects30m} format={String} />
          </Stat>
        </div>

        <div className="mt-3 flex items-center gap-1.5 overflow-hidden">
          <Tag tone={SOURCE_META[room.source].tone}>{SOURCE_META[room.source].label}</Tag>
          {room.tags.slice(0, 2).map((t) => (
            <Tag key={t}>{t}</Tag>
          ))}
          <span className="ml-auto flex shrink-0 items-center gap-1.5 text-faint">
            {room.proxyId && <Globe2 className="size-3.5 text-ink-info" aria-label="已指定代理" />}
            {room.accountId && <UserRound className="size-3.5 text-ink-social" aria-label="已分配小号" />}
            {room.subscribers > 0 && (
              <span className="num max-w-[120px] truncate text-[10px]" title={room.subscriberClients?.join("、")}>
                {room.subscriberClients?.[0] ? `${room.subscriberClients[0]} · ` : ""}
                {room.subscribers} 订阅
              </span>
            )}
          </span>
        </div>
      </div>
    </div>
  );
});

function Stat({ label, icon: Icon, children, warn, tone = "idle" }: { label: string; icon: typeof Users; children: React.ReactNode; warn?: boolean; tone?: Tone }) {
  const color = warn ? "warn" : tone;
  return (
    <div className={cn("rounded-xl px-2.5 py-2", color === "idle" ? "bg-background/60" : TONE[color].soft)}>
      <div className="flex items-center gap-1 text-[10px] text-muted-foreground">
        <Icon className={cn("size-3", TONE[color].ink)} />
        {label}
      </div>
      <div className="mt-0.5 text-sm font-semibold">{children}</div>
    </div>
  );
}

