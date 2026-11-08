import { Gift, Heart, Pause } from "lucide-react";
import { memo, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { Avatar, TypeTag } from "@/components/kit";
import { EmptyState } from "@/components/illustrations";
import { fmtClock } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useProgressiveCount } from "@/lib/hooks/use-progressive-count";
import type { FeedMsg } from "@/data/types";

const MAX_FEED_MESSAGES = 100;
/** 屏幕外的消息行跳过排版与绘制（固定行高，预估尺寸准确，滚动不跳） */
const FEED_ROW_STYLE = { contentVisibility: "auto", containIntrinsicSize: "auto 37px" } as const;

/** 关键字高亮 */
export function Highlight({ text, kw }: { text: string; kw?: string }) {
  if (!kw) return <>{text}</>;
  const i = text.toLowerCase().indexOf(kw.toLowerCase());
  if (i < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, i)}
      <mark className="rounded bg-chat/30 px-0.5 text-foreground">{text.slice(i, i + kw.length)}</mark>
      {text.slice(i + kw.length)}
    </>
  );
}

export const FeedRow = memo(function FeedRow({ m, showRoom, kw, dense }: { m: FeedMsg; showRoom?: boolean; kw?: string; dense?: boolean }) {
  return (
    // 固定单行高度：新消息插入时其余行只整体下移，不会因换行忽高忽低造成抖动
    <div className={cn("flex items-center gap-2.5", dense ? "h-8" : "h-9")}>
      <span className="num hidden w-14 shrink-0 text-[11px] text-faint sm:inline">{fmtClock(m.ts)}</span>
      <TypeTag type={m.type} />
      <Avatar name={m.nickname} src={m.avatar} seed={m.nickname} size={22} className="hidden sm:inline-flex" />
      <div className="min-w-0 flex-1 truncate text-sm leading-[22px]" title={m.type === "chat" ? `${m.nickname}：${m.content}` : undefined}>
        <span className="mr-1.5 text-muted-foreground">{m.nickname}</span>
        {showRoom && (
          <span className="mr-1.5 inline-flex h-5 max-w-[11rem] items-center truncate rounded-md bg-muted px-1.5 align-[1px] text-[11px] text-muted-foreground">
            {m.roomName}
          </span>
        )}
        <MsgContent m={m} kw={kw} />
      </div>
    </div>
  );
});

function MsgContent({ m, kw }: { m: FeedMsg; kw?: string }) {
  switch (m.type) {
    case "chat":
      return (
        <span className="text-foreground">
          <Highlight text={m.content} kw={kw} />
        </span>
      );
    case "gift":
      return (
        <span className="text-foreground">
          送出 <Gift className="inline size-3.5 -translate-y-px text-ink-gift" /> <span className="font-medium text-ink-gift">{m.extra?.gift}</span>
          <span className="num ml-1 font-semibold text-ink-gift">×{m.extra?.count}</span>
        </span>
      );
    case "like":
      return (
        <span className="text-muted-foreground">
          为主播点赞 <Heart className="inline size-3.5 -translate-y-px fill-like text-ink-like" />
          <span className="num ml-1 font-semibold text-ink-like">×{m.extra?.count}</span>
        </span>
      );
    case "social":
      return <span className="text-ink-social">{m.content}</span>;
    case "member":
      return <span className="text-muted-foreground">{m.content}</span>;
  }
}

/** 每批新消息在这段时间内逐条放出（推送间隔 2.5 秒，留一点余量） */
const DRIP_WINDOW_MS = 2300;
/** 排队上限：积压超过这么多条时直接显示较早的，只把最新的这些逐条放出，不会追着旧消息飞速滚动 */
const MAX_BACKLOG = 20;

/**
 * 实时消息流（参考抖音直播间评论区）：
 * - 每次推送来的一批新消息不一次性出现，而是排队、在推送间隔内均匀地一条条放出；来得越多放得越快。
 * - 新消息从顶部滑入，其余各行整体平滑下移（整个列表做一次平移过渡，只需合成）。
 * - 筛选条件变化（resetKey 变化）时直接显示结果，不再排队；显式暂停时冻结当前列表。
 */
export function LiveFeed({
  messages,
  showRoom,
  kw,
  limit = 60,
  paused,
  resetKey,
  className,
  emptyTitle = "还没有消息",
  emptyDesc,
  header,
}: {
  /** 按时间倒序（最新在前） */
  messages: FeedMsg[];
  showRoom?: boolean;
  kw?: string;
  limit?: number;
  paused?: boolean;
  /** 筛选条件的标识；变化时立即显示新结果 */
  resetKey?: string;
  className?: string;
  emptyTitle?: string;
  emptyDesc?: string;
  header?: ReactNode;
}) {
  // hidden：最新的若干条还在排队、尚未显示
  const [hidden, setHidden] = useState(0);
  const prevTop = useRef<string | undefined>(undefined);
  const prevKey = useRef(resetKey);
  const [frozen, setFrozen] = useState<FeedMsg[] | null>(null);

  // 计算这次推送新增了几条（上一次最新的一条现在排在第几位）
  const top = messages[0]?.id;
  if (top !== prevTop.current || resetKey !== prevKey.current) {
    const keyChanged = resetKey !== prevKey.current;
    const at = prevTop.current === undefined ? -1 : messages.findIndex((m) => m.id === prevTop.current);
    prevTop.current = top;
    prevKey.current = resetKey;
    // 首次进入、换了筛选、或断档太久找不到上一条：直接显示；否则把新增的几条放进队列
    const next = keyChanged || at < 0 ? 0 : Math.min(messages.length, MAX_BACKLOG, hidden + at);
    if (next !== hidden) setHidden(next);
  }

  // 标签页回到前台：后台时浏览器会限制定时器，队列来不及放；直接从最新的开始显示
  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") setHidden(0);
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, []);

  // 逐条放出：间隔 = 剩余时间 / 排队条数，限制在 40～400 毫秒
  useEffect(() => {
    if (hidden <= 0 || paused) return;
    const t = window.setTimeout(() => setHidden((h) => Math.max(0, h - 1)), Math.min(400, Math.max(40, DRIP_WINDOW_MS / hidden)));
    return () => window.clearTimeout(t);
  }, [hidden, paused]);

  const live = messages.slice(hidden, hidden + MAX_FEED_MESSAGES);
  if (paused && !frozen) setFrozen(live);
  if (!paused && frozen) {
    setFrozen(null);
    if (hidden) setHidden(0);
  }
  const shown = paused && frozen ? frozen : live;
  const full = shown.slice(0, Math.max(0, Math.min(MAX_FEED_MESSAGES, Math.floor(limit))));
  // 首次进入先画 30 行，其余下一帧补上，避免一次挂载 80 行造成掉帧
  const list = full.slice(0, useProgressiveCount(full.length, 30));
  const pendingCount = paused && frozen ? Math.max(0, messages.findIndex((m) => m.id === frozen[0]?.id)) : 0;

  // 顶部插入一条时，列表先上移一行（视觉上不动），再平滑滑回原位 = 新消息把旧消息往下“顶”
  const listRef = useRef<HTMLUListElement>(null);
  const shownTop = list[0]?.id;
  const lastShownTop = useRef<string | undefined>(undefined);
  useLayoutEffect(() => {
    const ul = listRef.current;
    const before = lastShownTop.current;
    lastShownTop.current = shownTop;
    if (!ul || !before || !shownTop || before === shownTop) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const second = ul.children[1] as HTMLElement | undefined;
    // 只处理“顶上新增一条”的情况；一次出现多条（如筛选切换）直接显示
    if (second?.dataset.id !== before) return;
    const h = (ul.children[0] as HTMLElement).offsetHeight;
    const current = new DOMMatrix(getComputedStyle(ul).transform).f;
    ul.style.transition = "none";
    ul.style.transform = `translateY(${current - h}px)`;
    void ul.offsetHeight;
    ul.style.transition = "transform 260ms cubic-bezier(0.22, 1, 0.36, 1)";
    ul.style.transform = "translateY(0px)";
  }, [shownTop]);

  return (
    <div className={cn("relative", className)}>
      {header}
      {paused && (
        // 高度为 0 的吸顶层：提示浮在列表上方，不占位置
        <div className="pointer-events-none sticky top-2 z-10 flex h-0 justify-center">
          <div className="flex h-7 items-center gap-1.5 rounded-full border border-border-strong bg-popover px-3 text-xs shadow-lg">
            <Pause className="size-3 text-ink-chat" />
            已暂停
            {pendingCount > 0 && <span className="num text-muted-foreground">· {pendingCount} 条新消息</span>}
          </div>
        </div>
      )}
      {list.length === 0 ? (
        <EmptyState title={emptyTitle} desc={emptyDesc} />
      ) : (
        <ul ref={listRef} className="relative will-change-transform">
          {list.map((m) => (
            <li key={m.id} data-id={m.id} className="overflow-hidden border-b border-border/60 px-1 last:border-b-0" style={FEED_ROW_STYLE}>
              <FeedRow m={m} showRoom={showRoom} kw={kw} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
