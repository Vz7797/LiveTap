import type { LucideIcon } from "lucide-react";
import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { RollingNumber } from "@/components/motion/rolling-number";
import { fmtNum } from "@/lib/format";
import { STATUS_META, TYPE_META, type MsgType, type RoomStatus, type Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";

/* ---------------- 图标底块：按用途使用同色浅底，必要时可关闭点缀 ---------------- */

export function IconTile({
  icon: Icon,
  tone,
  accent = true,
  size = 36,
  className,
}: {
  icon: LucideIcon;
  tone: Tone;
  accent?: boolean;
  size?: number;
  className?: string;
}) {
  const swap = useSwapClass();
  return (
    <span
      className={cn(
        "t-tone inline-flex shrink-0 items-center justify-center rounded-xl",
        accent ? TONE[tone].tint : "bg-muted text-muted-foreground",
        className,
      )}
      style={{ width: size, height: size }}
    >
      <Icon key={iconKey(Icon)} className={swap} style={{ width: size * 0.5, height: size * 0.5 }} strokeWidth={2} />
    </span>
  );
}

/** 图标切换时淡入并轻微放大；首次渲染不播放（换图标会因 key 变化重建元素，从而触发动画）。 */
function useSwapClass() {
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
  }, []);
  return mounted.current ? "icon-swap" : undefined;
}
const iconKeys = new WeakMap<object, number>();
let nextIconKey = 0;
function iconKey(icon: object) {
  if (!iconKeys.has(icon)) iconKeys.set(icon, ++nextIconKey);
  return iconKeys.get(icon);
}

/* ---------------- 状态胶囊：彩色圆点 + 短标签 ---------------- */

export function StatusDot({ status, className }: { status: RoomStatus; className?: string }) {
  const m = STATUS_META[status];
  return (
    <span
      className={cn("t-tone inline-block size-2 shrink-0 rounded-full", TONE[m.tone].fill, className)}
    />
  );
}

export function StatusPill({
  status,
  size = "md",
  className,
}: {
  status: RoomStatus;
  size?: "sm" | "md";
  className?: string;
}) {
  const m = STATUS_META[status];
  return (
    <span
      className={cn(
        "t-tone inline-flex shrink-0 items-center gap-1.5 rounded-full font-medium whitespace-nowrap",
        TONE[m.tone].tint,
        size === "sm" ? "h-5 px-2 text-[11px]" : "h-6 px-2.5 text-xs",
        className,
      )}
    >
      <StatusDot status={status} />
      {m.label}
    </span>
  );
}

/* ---------------- 消息类型标签 ---------------- */

export function TypeTag({ type, className, withIcon = true }: { type: MsgType; className?: string; withIcon?: boolean }) {
  const m = TYPE_META[type];
  const Icon = m.icon;
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium whitespace-nowrap",
        TONE[m.tone].tint,
        className,
      )}
    >
      {withIcon && <Icon className="size-3" strokeWidth={2.2} />}
      {m.label}
    </span>
  );
}

export function Tag({ tone = "idle", children, className }: { tone?: Tone; children: ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center gap-1 rounded-md px-1.5 text-[11px] font-medium whitespace-nowrap",
        TONE[tone].tint,
        className,
      )}
    >
      {children}
    </span>
  );
}

/* ---------------- 头像占位：浅蓝 / 在线浅绿底色 + 文字 ---------------- */

/** 抖音头像缩略图：只接受 http(s)，统一走 https。 */
function avatarUrl(src?: string) {
  if (!src || !/^https?:\/\//i.test(src)) return undefined;
  return src.replace(/^http:/i, "https:");
}

export function Avatar({
  name,
  src,
  size = 40,
  live = false,
  dim = false,
  className,
}: {
  name: string;
  seed?: string;
  /** 头像缩略图；加载前与失败时显示首字 */
  src?: string;
  size?: number;
  live?: boolean;
  dim?: boolean;
  className?: string;
}) {
  const ch = Array.from(name.replace(/^[^\p{L}\p{N}]+/u, ""))[0] ?? "?";
  const url = avatarUrl(src);
  const [failed, setFailed] = useState<string>();
  const [loaded, setLoaded] = useState<string>();
  const showImage = url && failed !== url;
  return (
    <span
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full font-semibold select-none",
        dim ? "bg-muted text-muted-foreground" : TONE[live ? "ok" : "info"].tint,
        dim && "opacity-45",
        className,
      )}
      style={{ width: size, height: size, fontSize: size * 0.42 }}
    >
      {ch}
      {showImage && (
        <img
          src={url}
          alt=""
          width={size}
          height={size}
          loading="lazy"
          decoding="async"
          referrerPolicy="no-referrer"
          draggable={false}
          onLoad={() => setLoaded(url)}
          onError={() => setFailed(url)}
          className={cn("absolute inset-0 size-full object-cover transition-opacity duration-200", loaded === url ? "opacity-100" : "opacity-0", dim && "grayscale")}
        />
      )}
    </span>
  );
}

/* ---------------- 等宽数字：默认直接更新，关键数字可显式开启动效 ---------------- */

export function Num({
  value,
  format = (n: number) => fmtNum(n),
  className,
  suffix,
  animate = false,
}: {
  value: number;
  format?: (n: number) => string;
  className?: string;
  suffix?: ReactNode;
  animate?: boolean;
}) {
  const text = format(value);
  // 滚动数字是纯 CSS 的轻量过渡，精简模式下也保留；系统要求减少动态效果时由样式关闭
  return (
    <span className={cn("num inline-flex items-baseline", className)}>
      {animate ? <RollingNumber text={text} /> : text}
      {suffix && <span className="ml-1 font-sans text-[0.5em] font-medium tracking-normal text-muted-foreground">{suffix}</span>}
    </span>
  );
}

/* ---------------- Bento 卡片 ---------------- */

export function Card({
  className,
  children,
  interactive,
  ...rest
}: ComponentProps<"div"> & { interactive?: boolean }) {
  return (
    <div
      className={cn(
        "relative rounded-3xl border bg-card text-card-foreground shadow-[var(--shadow-card)]",
        interactive && "transition-[border-color,transform] duration-200 hover:border-border-strong",
        className,
      )}
      {...rest}
    >
      {children}
    </div>
  );
}

export function CardHeader({
  icon,
  tone = "chat",
  title,
  desc,
  action,
  className,
}: {
  icon?: LucideIcon;
  tone?: Tone;
  title: ReactNode;
  desc?: ReactNode;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-wrap items-start gap-3", className)}>
      {icon && <IconTile icon={icon} tone={tone} />}
      <div className="min-w-0 flex-1 basis-[calc(100%_-_3rem)] sm:basis-0">
        <h3 className="text-[15px] leading-5 font-semibold break-words">{title}</h3>
        {desc && <p className="mt-0.5 text-xs leading-5 break-words text-muted-foreground">{desc}</p>}
      </div>
      {action && <div className="flex w-full flex-wrap items-center gap-2 sm:ml-auto sm:w-auto">{action}</div>}
    </div>
  );
}

export function Kbd({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <kbd
      className={cn(
        "inline-flex h-5 min-w-5 items-center justify-center rounded-md border border-border-strong bg-background/60 px-1 font-mono text-[10px] font-medium text-muted-foreground",
        className,
      )}
    >
      {children}
    </kbd>
  );
}

/* ---------------- 页面标题 ---------------- */

export function PageHeader({
  title,
  desc,
  icon,
  tone = "chat",
  actions,
}: {
  title: string;
  desc?: ReactNode;
  icon: LucideIcon;
  tone?: Tone;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-5 flex flex-col gap-4 sm:mb-6 sm:flex-row sm:items-end sm:justify-between">
      <div className="flex min-w-0 items-center gap-3.5">
        <IconTile icon={icon} tone={tone} size={44} className="rounded-2xl" />
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight sm:text-2xl">{title}</h1>
          {desc && <p className="mt-0.5 text-sm text-muted-foreground">{desc}</p>}
        </div>
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

/* ---------------- 胶囊按钮 ---------------- */

export function PillButton({
  tone = "neutral",
  icon: Icon,
  children,
  className,
  ...rest
}: ComponentProps<"button"> & { tone?: "primary" | "neutral" | "ghost" | "danger"; icon?: LucideIcon }) {
  return (
    <button
      type="button"
      className={cn(
        "inline-flex h-9 shrink-0 cursor-pointer items-center justify-center gap-1.5 rounded-full px-4 text-sm font-medium whitespace-nowrap transition-[background-color,color,transform] duration-150 outline-none select-none focus-visible:ring-2 focus-visible:ring-ring/60 active:scale-[0.97] disabled:pointer-events-none disabled:opacity-45",
        tone === "primary" &&
          "bg-primary text-primary-foreground hover:brightness-105",
        tone === "neutral" && "border bg-card hover:bg-accent",
        tone === "ghost" && "text-muted-foreground hover:bg-accent hover:text-foreground",
        tone === "danger" && "tint-err hover:brightness-110",
        className,
      )}
      {...rest}
    >
      {Icon && <Icon className="size-4" strokeWidth={2.2} />}
      {children}
    </button>
  );
}

export function IconButton({
  icon: Icon,
  label,
  className,
  ...rest
}: ComponentProps<"button"> & { icon: LucideIcon; label: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className={cn(
        "inline-flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-full text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/60 active:scale-95",
        className,
      )}
      {...rest}
    >
      <Icon className="size-[18px]" strokeWidth={2} />
    </button>
  );
}

/* ---------------- 进度条 ---------------- */

export function Meter({
  value,
  max = 100,
  tone = "chat",
  className,
}: {
  value: number;
  max?: number;
  tone?: Tone;
  className?: string;
}) {
  const pct = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0;
  return (
    <div className={cn("h-1.5 w-full overflow-hidden rounded-full bg-muted", className)}>
      {/* 用平移代替改宽度：只需合成，不触发重排 */}
      <div
        className={cn("t-move t-tone h-full w-full rounded-full", TONE[tone].fill)}
        style={{ transform: `translateX(${pct - 100}%)` }}
      />
    </div>
  );
}

/* ---------------- 浮层挂到 body，避免被页面过渡的 transform/filter 影响 fixed 定位 ---------------- */

export function Portal({ children }: { children: ReactNode }) {
  return createPortal(children, document.body);
}
