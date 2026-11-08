import { useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { useMotionSettings } from "@/app/motion";
import { fmtNum } from "@/lib/format";
import { toneVar, type Tone } from "@/lib/meta";
import { cn } from "@/lib/utils";

/* 图表规范（dataviz）：细线 2px、圆角数据端、网格与坐标轴弱化、
   ≥2 个系列必有图例、文字用文字色而非系列色、默认带悬停提示。 */

const MAX_CHART_POINTS = 120;
/** 实时推送间隔；数据过渡时长与它对齐，动画在两次推送之间连贯进行，不会“动一下停一下” */
const STREAM_MS = 2500;

function recentPoints<T>(data: T[]) {
  return data.length > MAX_CHART_POINTS ? data.slice(-MAX_CHART_POINTS) : data;
}

export function useSize<T extends HTMLElement>() {
  const ref = useRef<T>(null);
  const [size, setSize] = useState({ w: 0, h: 0 });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => {
      const { width, height } = e.contentRect;
      setSize((s) => (Math.abs(s.w - width) < 0.5 && Math.abs(s.h - height) < 0.5 ? s : { w: width, h: height }));
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, size] as const;
}

function smoothPath(pts: [number, number][]) {
  if (pts.length === 0) return "";
  if (pts.length === 1) return `M${pts[0][0]},${pts[0][1]}`;
  let d = `M${pts[0][0].toFixed(2)},${pts[0][1].toFixed(2)}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const [x0, y0] = pts[i - 1] ?? pts[i];
    const [x1, y1] = pts[i];
    const [x2, y2] = pts[i + 1];
    const [x3, y3] = pts[i + 2] ?? pts[i + 1];
    const t = 0.18;
    const c1x = x1 + (x2 - x0) * t;
    const c1y = y1 + (y2 - y0) * t;
    const c2x = x2 - (x3 - x1) * t;
    const c2y = y2 - (y3 - y1) * t;
    d += ` C${c1x.toFixed(2)},${c1y.toFixed(2)} ${c2x.toFixed(2)},${c2y.toFixed(2)} ${x2.toFixed(2)},${y2.toFixed(2)}`;
  }
  return d;
}

/* ---------------- 实时滚动 ----------------
 * 实时序列每次推送多一个新点、丢掉最老的点。直接重画会让曲线每 2.5 秒“跳”一格；
 * 这里改为：新数据先整体右移一格画出（看起来和上一帧一致），再用 CSS transform 在一个推送间隔内
 * 匀速左移回原位，于是曲线持续平滑地向左流动。左边保留刚离开的那个点，滑动时左缘不会空一块。
 * 只动 transform，由合成线程完成；系统要求减少动态效果时直接切换。 */

const prefersReducedMotion = () => typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

/** 刚离开窗口的那个点：数据整体前移一格时返回上一帧的第一个点，否则为 undefined */
function useLeadPoint<T>(data: T[], same: (a: T, b: T) => boolean, enabled: boolean) {
  const prev = useRef<T[]>([]);
  const lead = useMemo(() => {
    const p = prev.current;
    if (!enabled || data.length < 2 || p.length < 2) return undefined;
    // 新数据的前 n-1 个点等于旧数据的后 n-1 个点：说明窗口前移了一格
    const k = data.length - 1;
    if (p.length !== data.length || !same(p[1], data[0]) || !same(p[k], data[k - 1])) return undefined;
    return p[0];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, enabled]);
  useEffect(() => {
    prev.current = data;
  }, [data]);
  return lead;
}

/**
 * 在 duration 毫秒内把图层从右移 step 像素匀速滑回原位；key 变化（且发生了前移）时触发。
 * 必须作用在 HTML 元素上：Chrome 不会把 SVG 内部元素的 transform 交给合成线程，
 * 那样每一帧都要重绘整张图（实测主线程占用 90%）；HTML 图层只做合成，几乎不占主线程。
 */
function useStreamScroll(key: unknown, step: number, duration: number) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || key === undefined || !(step > 0) || prefersReducedMotion()) return;
    el.style.transition = "none";
    el.style.transform = `translateX(${step}px)`;
    void el.getBoundingClientRect();
    el.style.transition = `transform ${duration}ms linear`;
    el.style.transform = "translateX(0px)";
  }, [key, step, duration]);
  return ref;
}

/* ---------------- 迷你曲线 ---------------- */

export function Sparkline({
  data: allData,
  tone = "chat",
  height = 36,
  className,
  strokeWidth = 2,
  dotTone,
  animate = false,
  fit = false,
}: {
  data: number[];
  /** 纵轴贴合数据范围而不是从 0 起，适合在线人数这类基数大、起伏小的量；数据里有 0（断流）时自动回到 0 起 */
  fit?: boolean;
  /** 数值变化时平滑过渡；列表里大量出现的小曲线（如直播间卡片）保持关闭，避免每次推送同时动画上百条路径 */
  animate?: boolean;
  tone?: Tone;
  /** 末端圆点单独着色，例如采集中的直播间用绿色点表示“在线” */
  dotTone?: Tone;
  height?: number;
  className?: string;
  strokeWidth?: number;
}) {
  const data = useMemo(() => recentPoints(allData), [allData]);
  const [ref, { w }] = useSize<HTMLDivElement>();
  const lead = useLeadPoint(data, (a, b) => a === b, animate);
  const plot = lead === undefined ? data : [lead, ...data];
  const max = Math.max(1, ...plot);
  // fit：下沿比最小值再低 10% 的起伏；起伏不足最大值 15% 时按 15% 撑开，避免几个人进出被放大成大起大落
  const span = Math.max(max - Math.min(...plot), max * 0.15, 4);
  const lo = fit ? Math.max(0, max - span * 1.1) : 0;
  const pad = strokeWidth + 1;
  const step = Math.max(1, w) / Math.max(1, data.length - 1);
  const pts: [number, number][] = plot.map((v, i) => [
    (i - (lead === undefined ? 0 : 1)) * step,
    pad + (1 - (v - lo) / Math.max(1, max - lo)) * (height - pad * 2),
  ]);
  const line = smoothPath(pts);
  const area = w > 0 && pts.length > 1 ? `${line} L${pts[pts.length - 1][0]},${height} L${pts[0][0]},${height} Z` : "";
  const last = pts[pts.length - 1];
  const scroll = useStreamScroll(lead === undefined ? undefined : data, step, STREAM_MS);
  return (
    <div ref={ref} className={cn("relative w-full", className)} style={{ height }}>
      {w > 0 && (
        <>
          {/* 曲线放在单独的 HTML 图层里滑动（合成线程完成），上下各留 4px 不裁掉线宽 */}
          {/* 末端圆点也在滑动图层里，随曲线一起移入；图层右侧多留 6px 不裁掉圆点 */}
          <div className="absolute -top-1 -right-1.5 -bottom-1 left-0 overflow-hidden">
            <div ref={scroll} style={animate ? { willChange: "transform" } : undefined}>
              <svg width={w} height={height} className="mt-1 block overflow-visible">
                <path d={area} fill={toneVar(tone)} fillOpacity={0.08} />
                <path d={line} fill="none" stroke={toneVar(tone)} strokeWidth={strokeWidth} strokeLinecap="round" strokeLinejoin="round" />
                {last && max > 1 && <circle r={3} fill={toneVar(dotTone ?? tone)} stroke="var(--card)" strokeWidth={2} cx={last[0]} cy={last[1]} />}
              </svg>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

/* ---------------- 堆叠面积图（带十字线提示） ---------------- */

export interface SeriesDef {
  key: string;
  label: string;
  tone: Tone;
}

export function StackedArea({
  series,
  data: allData,
  height = 220,
  formatX,
  unit = "条/分",
  highlight: hoverKey = null,
  className,
  stream = false,
  slots,
  fill = false,
}: {
  /** 高度跟随容器（至少 height），与旁边卡片等高时不留空白 */
  fill?: boolean;
  /** 实时模式：新点从右侧持续滑入（见“实时滚动”） */
  stream?: boolean;
  /** 固定的点位数：数据不足时靠右排列，预热阶段曲线不会被横向压缩 */
  slots?: number;
  /** 高亮某一层（图例悬停时），其余层变淡 */
  highlight?: string | null;
  series: SeriesDef[];
  data: { at: number; v: Record<string, number> }[];
  height?: number;
  formatX: (at: number) => string;
  unit?: string;
  className?: string;
}) {
  const data = useMemo(() => recentPoints(allData), [allData]);
  const [ref, { w, h: boxH }] = useSize<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const minHeight = height;
  height = fill ? Math.max(minHeight, Math.floor(boxH)) : minHeight;
  const padL = 40;
  const padB = 22;
  const padT = 8;
  const innerW = Math.max(1, w - padL - 4);
  const innerH = height - padB - padT;

  const lead = useLeadPoint(data, (a, b) => a.at === b.at, stream);
  const li = lead ? 1 : 0;
  const plot = useMemo(() => (lead ? [lead, ...data] : data), [lead, data]);
  const stacks = useMemo(() => {
    const acc = plot.map(() => 0);
    return series.map((s) => {
      const lower = [...acc];
      plot.forEach((d, i) => (acc[i] += d.v[s.key] ?? 0));
      return { s, lower, upper: [...acc] };
    });
  }, [plot, series]);

  const total = Math.max(slots ?? 0, data.length);
  const offset = total - data.length;
  const step = innerW / Math.max(1, total - 1);
  const totalMax = Math.max(1, ...plot.map((_, i) => stacks[stacks.length - 1]?.upper[i] ?? 0));
  const niceMax = niceCeil(totalMax * 1.08);
  /** 数据点 i（0 为窗口内最老的点，-1 为刚离开的点）的横坐标 */
  const x = (i: number) => padL + (i + offset) * step;
  const xp = (j: number) => x(j - li);
  const y = (v: number) => padT + (1 - v / niceMax) * innerH;
  const ticks = [0, niceMax / 3, (niceMax * 2) / 3, niceMax];
  const lastAt = data[data.length - 1]?.at ?? 0;
  const interval = data.length > 1 ? lastAt - data[data.length - 2].at : STREAM_MS;
  const scroll = useStreamScroll(lead ? lastAt : undefined, step, Math.min(5000, Math.max(800, interval)));

  const onMove = (e: React.PointerEvent) => {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const px = e.clientX - rect.left - padL;
    const i = Math.round(px / step) - offset;
    setHover(Math.max(0, Math.min(data.length - 1, i)));
  };

  const hi = hover !== null ? data[hover] : null;
  const labelEvery = Math.max(1, Math.ceil(total / Math.max(2, Math.floor(innerW / 90))));
  // 实时模式按时间取刻度（随曲线一起滑动）；历史模式按序号均匀取
  const labelSpan = labelEvery * Math.max(1, interval);
  const showLabel = (i: number) =>
    (stream ? i > 0 && Math.floor(data[i].at / labelSpan) !== Math.floor(data[i - 1].at / labelSpan) : i % labelEvery === 0) &&
    x(i) > padL + 24 &&
    x(i) < w - 56;

  return (
    <div
      ref={ref}
      className={cn("relative w-full touch-pan-y select-none", className)}
      style={fill ? { minHeight } : { height }}
      onPointerMove={onMove}
      onPointerLeave={() => setHover(null)}
    >
      {w > 0 && (
        <>
          {/* 静态层：网格与纵轴刻度 */}
          <svg width={w} height={height} className={fill ? "absolute inset-x-0 top-0" : "block"}>
            {ticks.map((t) => (
              <g key={t}>
                <line x1={padL} x2={w - 4} y1={y(t)} y2={y(t)} stroke="var(--border)" strokeDasharray={t === 0 ? undefined : "3 5"} />
                <text x={padL - 8} y={y(t)} dy="0.32em" textAnchor="end" className="fill-faint num text-[10px]">
                  {fmtNum(t)}
                </text>
              </g>
            ))}
            <text x={w - 4} y={height - 6} textAnchor="end" className="fill-faint text-[10px]">
              现在
            </text>
          </svg>
          {/* 数据层：色带与横轴时间，单独的 HTML 图层，实时模式下整体向左滑动。
              色带画法：每层实色低透明填充，层与层之间用卡片底色细线隔开，整体轮廓用一条中性线收边。 */}
          <div className="pointer-events-none absolute top-0 overflow-hidden" style={{ left: padL, width: innerW + 2, height }}>
            <div ref={scroll} style={stream ? { willChange: "transform" } : undefined}>
              <svg width={innerW + 2} height={height} className="block overflow-visible">
                <g transform={`translate(${-padL} 0)`}>
                  {stacks.map(({ s, lower, upper }) => {
                    const top = upper.map((v, i) => [xp(i), y(v)] as [number, number]);
                    const bottom = lower.map((v, i) => [xp(i), y(v)] as [number, number]).reverse();
                    const topD = smoothPath(top);
                    const bottomD = smoothPath(bottom).replace(/^M/, "L");
                    const area = top.length > 1 ? `${topD} ${bottomD} Z` : "";
                    const dim = hoverKey !== null && hoverKey !== s.key;
                    return (
                      <path
                        key={s.key}
                        d={area}
                        fill={toneVar(s.tone)}
                        style={{ fillOpacity: `calc(var(${s.key === series[0]?.key ? "--band-main" : "--band"}) * ${dim ? 0.35 : 1})` }}
                      />
                    );
                  })}
                  {stacks.slice(0, -1).map(({ s, upper }) => (
                    <path key={`gap-${s.key}`} d={smoothPath(upper.map((v, i) => [xp(i), y(v)] as [number, number]))} fill="none" stroke="var(--card)" strokeWidth={1.5} />
                  ))}
                  {stacks.length > 0 && (
                    <path
                      d={smoothPath(stacks[stacks.length - 1].upper.map((v, i) => [xp(i), y(v)] as [number, number]))}
                      fill="none"
                      stroke="var(--c-graph)"
                      strokeWidth={1.5}
                      strokeLinejoin="round"
                    />
                  )}
                  {data.map((d, i) =>
                    showLabel(i) ? (
                      <text key={d.at} x={x(i)} y={height - 6} textAnchor="middle" className="fill-faint num text-[10px]">
                        {formatX(d.at)}
                      </text>
                    ) : null,
                  )}
                </g>
              </svg>
            </div>
          </div>
          {/* 交互层：十字线与数据点 */}
          {hover !== null && hi && (
            <svg width={w} height={height} className="pointer-events-none absolute inset-x-0 top-0">
              <line x1={x(hover)} x2={x(hover)} y1={padT} y2={padT + innerH} stroke="var(--border-strong)" strokeWidth={1} />
              {stacks.map(({ s, upper }) => (
                <circle key={s.key} cx={x(hover)} cy={y(upper[hover + li])} r={4} fill={toneVar(s.tone)} stroke="var(--card)" strokeWidth={2} />
              ))}
            </svg>
          )}
        </>
      )}
      {hover !== null && hi && (
        <div
          className="pointer-events-none absolute top-1 z-10 w-40 rounded-xl border bg-popover p-2.5 text-xs shadow-lg"
          style={{ left: Math.min(Math.max(x(hover) + 12, 0), w - 168) }}
        >
          <div className="mb-1.5 flex items-center justify-between text-muted-foreground">
            <span className="num">{formatX(hi.at)}</span>
            <span>{unit}</span>
          </div>
          {[...series].reverse().map((s) => (
            <div key={s.key} className="flex items-center gap-2 py-0.5">
              <span className="size-2 rounded-full" style={{ background: toneVar(s.tone) }} />
              <span className="flex-1 text-muted-foreground">{s.label}</span>
              <span className="num font-medium">{fmtNum(hi.v[s.key] ?? 0)}</span>
            </div>
          ))}
          <div className="mt-1 flex items-center justify-between border-t pt-1.5">
            <span className="text-muted-foreground">合计</span>
            <span className="num font-semibold">{fmtNum(series.reduce((a, s) => a + (hi.v[s.key] ?? 0), 0))}</span>
          </div>
        </div>
      )}
    </div>
  );
}

function niceCeil(v: number) {
  if (v <= 0) return 1;
  const p = Math.pow(10, Math.floor(Math.log10(v)));
  const n = v / p;
  const step = n <= 1.5 ? 1.5 : n <= 3 ? 3 : n <= 6 ? 6 : 10;
  return step * p;
}

export function Legend({
  items,
  className,
  onHover,
}: {
  items: { key?: string; label: string; tone: Tone; value?: ReactNode }[];
  className?: string;
  onHover?: (key: string | null) => void;
}) {
  return (
    <div className={cn("flex flex-wrap items-center gap-x-4 gap-y-1.5 text-xs", className)} onPointerLeave={() => onHover?.(null)}>
      {items.map((it) => (
        <span
          key={it.label}
          className={cn("inline-flex items-center gap-1.5 text-muted-foreground", onHover && "cursor-default hover:text-foreground")}
          onPointerEnter={() => onHover?.(it.key ?? it.label)}
        >
          <span className="size-2 rounded-full" style={{ background: toneVar(it.tone) }} />
          {it.label}
          {it.value !== undefined && <span className="num font-medium text-foreground">{it.value}</span>}
        </span>
      ))}
    </div>
  );
}

/* ---------------- 环形图 ---------------- */

export function Donut({
  segments,
  size = 180,
  thickness = 18,
  center,
}: {
  segments: { key: string; label: string; tone: Tone; value: number }[];
  size?: number;
  thickness?: number;
  center?: (active: { label: string; value: number; pct: number } | null) => ReactNode;
}) {
  const [active, setActive] = useState<string | null>(null);
  const { minimal } = useMotionSettings();
  const total = Math.max(1, segments.reduce((a, s) => a + s.value, 0));
  const r = (size - thickness) / 2 - 4;
  const c = size / 2;
  const gap = 0.035; // 弧度间隙
  let start = -Math.PI / 2;
  const arcs = segments.map((s) => {
    const angle = (s.value / total) * Math.PI * 2;
    const a0 = start + gap / 2;
    const a1 = start + angle - gap / 2;
    start += angle;
    return { s, a0, a1: Math.max(a0 + 0.001, a1) };
  });
  const act = arcs.find((a) => a.s.key === active);
  return (
    <div className="relative" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="block overflow-visible">
        <circle cx={c} cy={c} r={r} fill="none" stroke="var(--muted)" strokeWidth={thickness} />
        {arcs.map(({ s, a0, a1 }) => {
          const isActive = active === s.key;
          const rr = r;
          const large = a1 - a0 > Math.PI ? 1 : 0;
          const d = `M${c + rr * Math.cos(a0)},${c + rr * Math.sin(a0)} A${rr},${rr} 0 ${large} 1 ${c + rr * Math.cos(a1)},${c + rr * Math.sin(a1)}`;
          return (
            <path
              key={s.key}
              d={d}
              fill="none"
              stroke={toneVar(s.tone)}
              strokeLinecap="round"
              opacity={active && !isActive ? 0.35 : 1}
              strokeWidth={isActive ? thickness + 4 : thickness}
              // 占比每次推送只变化零点几个百分点，弧长不做过渡（路径插值要在主线程逐帧计算，实测是总览最大的开销）
              style={{ transition: minimal ? "none" : "stroke-width 180ms ease, opacity 180ms ease" }}
              onPointerEnter={() => setActive(s.key)}
              onPointerLeave={() => setActive(null)}
              className="cursor-pointer"
            />
          );
        })}
      </svg>
      <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center text-center">
        {center?.(act ? { label: act.s.label, value: act.s.value, pct: act.s.value / total } : null)}
      </div>
    </div>
  );
}

/* ---------------- 健康环形仪表 ---------------- */

export function Gauge({
  value,
  size = 168,
  thickness = 14,
  children,
  trackClass = "stroke-muted",
  colors = ["var(--c-chat)", "var(--c-chat)"],
}: {
  value: number;
  size?: number;
  thickness?: number;
  children?: ReactNode;
  trackClass?: string;
  colors?: [string, string];
}) {
  const r = (size - thickness) / 2;
  const c = size / 2;
  const sweep = 0.75; // 270°
  const circ = 2 * Math.PI * r;
  const ticks = Array.from({ length: 28 });
  return (
    <div className="relative" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="block rotate-[135deg]">
        <circle
          cx={c}
          cy={c}
          r={r}
          fill="none"
          className={trackClass}
          strokeWidth={thickness}
          strokeLinecap="round"
          strokeDasharray={`${circ * sweep} ${circ}`}
        />
        <circle
          cx={c}
          cy={c}
          r={r}
          fill="none"
          stroke={colors[0]}
          strokeWidth={thickness}
          strokeLinecap="round"
          className="t-dash"
          // 按整数取值：小数波动不触发过渡（弧长只能在主线程逐帧计算）
          style={{ strokeDasharray: `${(circ * sweep * Math.max(0, Math.min(100, Math.round(value)))) / 100} ${circ}` }}
        />
        {ticks.map((_, i) => {
          const a = (i / (ticks.length - 1)) * Math.PI * 2 * sweep;
          const r1 = r - thickness / 2 - 6;
          const r2 = r1 - (i % 3 === 0 ? 6 : 3);
          return (
            <line
              key={i}
              x1={c + r1 * Math.cos(a)}
              y1={c + r1 * Math.sin(a)}
              x2={c + r2 * Math.cos(a)}
              y2={c + r2 * Math.sin(a)}
              className="stroke-border-strong"
              strokeWidth={1.2}
              strokeLinecap="round"
            />
          );
        })}
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">{children}</div>
    </div>
  );
}

/* ---------------- 分钟堆叠柱 ---------------- */

export function MinuteBars({
  data: allData,
  series,
  height = 140,
  className,
}: {
  data: Record<string, number>[];
  series: SeriesDef[];
  height?: number;
  className?: string;
}) {
  const data = useMemo(() => recentPoints(allData), [allData]);
  const [ref, { w }] = useSize<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const totals = data.map((d) => series.reduce((a, s) => a + (d[s.key] ?? 0), 0));
  const max = niceCeil(Math.max(1, ...totals) * 1.05);
  const gap = Math.min(3, w / Math.max(1, data.length * 3));
  const bw = Math.max(0, (w - gap * (data.length - 1)) / Math.max(1, data.length));
  const padB = 18;
  const innerH = height - padB;
  return (
    <div ref={ref} className={cn("relative w-full", className)} style={{ height }} onPointerLeave={() => setHover(null)}>
      {w > 0 && (
        <svg width={w} height={height} className="block">
          {[0.5, 1].map((t) => (
            <line key={t} x1={0} x2={w} y1={innerH * (1 - t)} y2={innerH * (1 - t)} stroke="var(--border)" strokeDasharray="3 5" />
          ))}
          {data.map((d, i) => {
            let acc = 0;
            const x = i * (bw + gap);
            return (
              <g key={i} onPointerEnter={() => setHover(i)} opacity={hover !== null && hover !== i ? 0.45 : 1}>
                <rect x={x} y={0} width={bw} height={innerH} fill="transparent" />
                {series.map((s, si) => {
                  const v = d[s.key] ?? 0;
                  const h = (v / max) * innerH;
                  const yTop = innerH - ((acc + v) / max) * innerH;
                  acc += v;
                  if (h <= 0) return null;
                  const isTop = series.slice(si + 1).every((n) => !(d[n.key] > 0));
                  return (
                    <rect
                      key={s.key}
                      className="t-rect"
                      x={x}
                      width={bw}
                      rx={isTop ? Math.min(3, bw / 2) : 0}
                      fill={toneVar(s.tone)}
                      style={{ y: `${yTop}px`, height: `${Math.max(0, h - (si > 0 ? 1 : 0))}px` } as CSSProperties}
                    />
                  );
                })}
              </g>
            );
          })}
          <text x={0} y={height - 4} className="fill-faint text-[10px]">
            30 分钟前
          </text>
          <text x={w} y={height - 4} textAnchor="end" className="fill-faint text-[10px]">
            现在
          </text>
        </svg>
      )}
      {hover !== null && data[hover] && (
        <div
          className="pointer-events-none absolute -top-2 z-10 w-36 -translate-y-full rounded-xl border bg-popover p-2.5 text-xs shadow-lg"
          style={{ left: Math.min(Math.max(hover * (bw + gap) - 60, 0), w - 144) }}
        >
          <div className="mb-1 text-muted-foreground">{data.length - hover} 分钟前</div>
          {[...series].reverse().map((s) => (
            <div key={s.key} className="flex items-center gap-2 py-0.5">
              <span className="size-2 rounded-full" style={{ background: toneVar(s.tone) }} />
              <span className="flex-1 text-muted-foreground">{s.label}</span>
              <span className="num font-medium">{fmtNum(data[hover][s.key] ?? 0)}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/* ---------------- 单系列折线（测试实验室小多图） ---------------- */

export function LineChart({
  data: allData,
  tone,
  height = 120,
  max,
  threshold,
  format = (v: number) => fmtNum(v),
  stages,
  slots,
  className,
}: {
  data: number[];
  /** 横轴总点数（未跑完时右侧留空） */
  slots?: number;
  tone: Tone;
  height?: number;
  max?: number;
  threshold?: { value: number; label: string };
  format?: (v: number) => string;
  /** 档位分隔：在第 i 个点处画竖线 */
  stages?: { at: number; label: string }[];
  className?: string;
}) {
  const data = useMemo(() => recentPoints(allData), [allData]);
  const startIndex = Math.max(0, allData.length - MAX_CHART_POINTS);
  const [ref, { w }] = useSize<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const capacity = Math.max(2, data.length, Math.min(MAX_CHART_POINTS, (slots ?? 60) - startIndex));
  const m = max ?? niceCeil(Math.max(1, ...data, threshold?.value ?? 0) * 1.1);
  const padT = 6;
  const padB = 16;
  const innerH = height - padT - padB;
  const x = (i: number) => (i / (capacity - 1)) * w;
  const y = (v: number) => padT + (1 - v / m) * innerH;
  const pts = data.map((v, i) => [x(i), y(v)] as [number, number]);
  const line = smoothPath(pts);
  const area = pts.length > 1 ? `${line} L${pts[pts.length - 1][0]},${padT + innerH} L0,${padT + innerH} Z` : "";
  return (
    <div
      ref={ref}
      className={cn("relative w-full touch-pan-y", className)}
      style={{ height }}
      onPointerMove={(e) => {
        const rect = e.currentTarget.getBoundingClientRect();
        const i = Math.round(((e.clientX - rect.left) / w) * (capacity - 1));
        setHover(i >= 0 && i < data.length ? i : null);
      }}
      onPointerLeave={() => setHover(null)}
    >
      {w > 0 && (
        <svg width={w} height={height} className="block overflow-visible">
          <line x1={0} x2={w} y1={padT + innerH} y2={padT + innerH} stroke="var(--border)" />
          <line x1={0} x2={w} y1={padT} y2={padT} stroke="var(--border)" strokeDasharray="3 5" />
          <text x={2} y={padT + 10} className="fill-faint num text-[10px]">
            {format(m)}
          </text>
          {stages?.filter((s) => s.at >= startIndex && s.at < startIndex + capacity).map((s) => (
            <g key={s.at}>
              <line x1={x(s.at - startIndex)} x2={x(s.at - startIndex)} y1={padT} y2={padT + innerH} stroke="var(--border-strong)" strokeDasharray="2 4" />
              <text x={x(s.at - startIndex) + 3} y={height - 3} className="fill-faint num text-[10px]">
                {s.label}
              </text>
            </g>
          ))}
          {threshold && (
            <g>
              <line x1={0} x2={w} y1={y(threshold.value)} y2={y(threshold.value)} stroke="var(--c-warn)" strokeDasharray="4 4" strokeOpacity={0.7} />
              <text x={w - 2} y={y(threshold.value) - 4} textAnchor="end" className="fill-ink-warn text-[10px]">
                {threshold.label}
              </text>
            </g>
          )}
          <path className="t-path" d={area} fill={toneVar(tone)} fillOpacity={0.1} />
          <path className="t-path" d={line} fill="none" stroke={toneVar(tone)} strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          {pts.length > 0 && <circle cx={pts[pts.length - 1][0]} cy={pts[pts.length - 1][1]} r={3.5} fill={toneVar(tone)} stroke="var(--card)" strokeWidth={2} />}
          {hover !== null && pts[hover] && (
            <g>
              <line x1={pts[hover][0]} x2={pts[hover][0]} y1={padT} y2={padT + innerH} stroke="var(--border-strong)" />
              <circle cx={pts[hover][0]} cy={pts[hover][1]} r={4} fill={toneVar(tone)} stroke="var(--card)" strokeWidth={2} />
            </g>
          )}
        </svg>
      )}
      {hover !== null && pts[hover] && (
        <div
          className="pointer-events-none absolute top-0 z-10 rounded-lg border bg-popover px-2 py-1 text-xs shadow-lg"
          style={{ left: Math.min(pts[hover][0] + 8, w - 70) }}
        >
          <span className="num font-medium">{format(data[hover])}</span>
        </div>
      )}
    </div>
  );
}
