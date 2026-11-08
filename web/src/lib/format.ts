const nf = new Intl.NumberFormat("zh-CN");

/** 1234 → "1,234"；超过 10 万显示为 "12.3万" */
export function fmtNum(n: number, compact = true) {
  if (compact && Math.abs(n) >= 100_000) return `${(n / 10_000).toFixed(1)}万`;
  return nf.format(Math.round(n));
}

export function fmtDec(n: number, digits = 1) {
  return n.toFixed(digits);
}

export function fmtAgo(ts: number, now = Date.now()) {
  const s = Math.max(0, Math.round((now - ts) / 1000));
  if (s < 5) return "刚刚";
  if (s < 60) return `${s} 秒前`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分钟前`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h} 小时前`;
  return `${Math.floor(h / 24)} 天前`;
}

export function fmtClock(ts: number, withSec = true) {
  const d = new Date(ts);
  const p = (x: number) => String(x).padStart(2, "0");
  return withSec ? `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}` : `${p(d.getHours())}:${p(d.getMinutes())}`;
}

export function fmtDuration(ms: number) {
  const m = Math.floor(ms / 60_000);
  const d = Math.floor(m / 1440);
  const h = Math.floor((m % 1440) / 60);
  const mm = m % 60;
  if (d > 0) return `${d} 天 ${h} 小时`;
  if (h > 0) return `${h} 小时 ${mm} 分`;
  return `${mm} 分钟`;
}

export function fmtBytesPerSec(kb: number) {
  if (kb >= 1024) return { value: (kb / 1024).toFixed(2), unit: "MB/s" };
  return { value: kb.toFixed(1), unit: "KB/s" };
}
