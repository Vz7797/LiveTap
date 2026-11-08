import type { DropStats } from "@/data/types";

const REASONS: [keyof DropStats["reason"], string][] = [["full", "队列满"], ["oversize", "超大消息"]];
const TYPES: [keyof DropStats["type"], string][] = [["chat", "弹幕"], ["member", "进场"], ["social", "关注/分享"], ["gift", "礼物"], ["other", "其他"]];

function parts<K extends string>(values: Record<K, number>, labels: [K, string][]) {
  return labels.filter(([key]) => values[key] > 0).map(([key, label]) => `${label} ${values[key]}`).join("、");
}

function clock(ms: number) {
  const d = new Date(ms);
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

/** 例如“队列满 3；弹幕 1、其他 2；最近一次 10月25日 18:40”，没有丢弃时返回空串 */
export function describeDrops(drops?: DropStats) {
  if (!drops?.total) return "";
  const last = drops.lastAt ? `；最近一次 ${clock(drops.lastAt)}` : "";
  const peak = drops.peakItems ? `；队列最高 ${drops.peakItems} 条 / ${Math.round(drops.peakBytes / 1024)} KB` : "";
  return `${parts(drops.reason, REASONS)}；${parts(drops.type, TYPES)}${last}${peak}`;
}
