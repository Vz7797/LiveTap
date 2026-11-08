import { toneVar, type Tone } from "@/lib/meta";
import { cn } from "@/lib/utils";

/* ---------------- Logo：黄色底块 + 气泡 + 三道弹幕 ---------------- */

export function Logo({ size = 36, className }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 40 40" className={className} aria-hidden>
      <rect x="0" y="0" width="40" height="40" rx="12" fill="var(--primary)" />
      <path
        d="M11 12.5h18a3 3 0 0 1 3 3v9a3 3 0 0 1-3 3h-9.5l-5.2 4.1c-.5.4-1.3 0-1.3-.6v-3.5H11a3 3 0 0 1-3-3v-9a3 3 0 0 1 3-3Z"
        fill="var(--primary-foreground)"
      />
      <rect x="12" y="16.5" width="11" height="2.4" rx="1.2" fill="var(--primary)" />
      <rect x="16" y="21" width="12" height="2.4" rx="1.2" fill="var(--primary)" />
      <circle cx="13.2" cy="22.2" r="1.3" fill="var(--primary)" />
    </svg>
  );
}

/* ---------------- 弹幕示意：静态展示，不运行背景动画 ---------------- */

const LANES: { text: string; tone: Tone }[][] = [
  [{ text: "这个套餐几点能用？", tone: "chat" }, { text: "点赞 ×12", tone: "like" }, { text: "小王 来了", tone: "member" }],
  [{ text: "送出 小心心 ×3", tone: "gift" }, { text: "周末可以用吗", tone: "chat" }],
  [{ text: "Lily 通过同城进入", tone: "member" }, { text: "关注了主播", tone: "social" }, { text: "已拍，期待！", tone: "chat" }],
  [{ text: "点赞 ×5", tone: "like" }, { text: "有停车位吗？", tone: "chat" }],
];

export function DanmuFlow({ className }: { className?: string }) {
  return (
    <div className={cn("pointer-events-none grid content-center gap-2 overflow-hidden", className)} aria-hidden>
      {LANES.map((lane, li) => (
        <div key={li} className="flex gap-2" style={{ paddingLeft: li % 2 ? 28 : 0 }}>
          {lane.map((it, ii) => (
            <span key={ii} className="flex h-8 shrink-0 items-center gap-2 rounded-full border bg-muted px-3 text-[13px] font-medium whitespace-nowrap text-muted-foreground">
              <span className="size-1.5 shrink-0 rounded-full" style={{ background: toneVar(it.tone) }} />
              {it.text}
            </span>
          ))}
        </div>
      ))}
    </div>
  );
}

/* ---------------- 风控雷达 ---------------- */

export function RadarScope({
  blips,
  size = 132,
}: {
  blips: { angle: number; dist: number; tone: Tone; key: string }[];
  size?: number;
}) {
  const c = size / 2;
  return (
    <div className="relative shrink-0" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="block">
        {[0.33, 0.66, 1].map((f) => (
          <circle key={f} cx={c} cy={c} r={(c - 2) * f} fill="none" stroke="var(--border-strong)" strokeDasharray={f === 1 ? undefined : "2 4"} />
        ))}
        <line x1={c} y1={2} x2={c} y2={size - 2} stroke="var(--border)" />
        <line x1={2} y1={c} x2={size - 2} y2={c} stroke="var(--border)" />
      </svg>
      {blips.map((b) => {
        const r = (c - 10) * b.dist;
        const a = (b.angle * Math.PI) / 180;
        return (
          <span key={b.key} className="absolute" style={{ left: c + r * Math.cos(a) - 5, top: c + r * Math.sin(a) - 5 }}>
            <span className="relative block size-2.5 rounded-full ring-2 ring-card" style={{ background: toneVar(b.tone) }} />
          </span>
        );
      })}
      <span className="absolute size-2 rounded-full bg-foreground/60" style={{ left: c - 4, top: c - 4 }} />
    </div>
  );
}

/* ---------------- 空状态插画 ---------------- */

export function EmptyArt({ className, tone = "chat" }: { className?: string; tone?: Tone }) {
  return (
    <svg viewBox="0 0 160 120" className={cn("h-28 w-auto", className)} aria-hidden>
      <ellipse cx="80" cy="108" rx="52" ry="6" fill="var(--muted)" />
      <rect x="34" y="22" width="92" height="64" rx="14" fill="var(--muted)" stroke="var(--border-strong)" />
      <rect x="46" y="38" width="42" height="7" rx="3.5" fill={toneVar(tone)} opacity="0.9" />
      <rect x="58" y="52" width="54" height="7" rx="3.5" fill="var(--border-strong)" />
      <rect x="46" y="66" width="30" height="7" rx="3.5" fill="var(--border-strong)" />
      <path d="M62 86l-6 12 16-12" fill="var(--muted)" stroke="var(--border-strong)" strokeLinejoin="round" />
      <circle cx="128" cy="22" r="4" fill="var(--c-like)" />
      <circle cx="24" cy="44" r="3" fill="var(--c-member)" />
      <path d="M136 52l2 5 5 2-5 2-2 5-2-5-5-2 5-2z" fill="var(--c-social)" />
      <path d="M22 78l1.5 3.5 3.5 1.5-3.5 1.5-1.5 3.5-1.5-3.5-3.5-1.5 3.5-1.5z" fill="var(--c-gift)" />
    </svg>
  );
}

export function EmptyState({ title, desc, tone }: { title: string; desc?: string; tone?: Tone }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-6 py-12 text-center">
      <EmptyArt tone={tone} />
      <div className="mt-2 text-sm font-medium">{title}</div>
      {desc && <div className="max-w-xs text-xs text-muted-foreground">{desc}</div>}
    </div>
  );
}

/* ---------------- 发现直播间的静态扫描示意 ---------------- */

export function ScanGrid({ active, className }: { active: boolean; className?: string }) {
  const cells = Array.from({ length: 48 });
  return (
    <div className={cn("relative grid grid-cols-12 gap-1.5", className)} aria-hidden>
      {cells.map((_, i) => (
        <span
          key={i}
          className="aspect-square rounded-[4px] bg-muted"
          style={{ background: active ? (i % 7 === 0 ? "var(--c-ok)" : "var(--border-strong)") : undefined }}
        />
      ))}
    </div>
  );
}
