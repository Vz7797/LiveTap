import type { Tone } from "./meta";

/**
 * Tailwind 只识别源码中完整出现的类名，所以按色调把常用组合写全。
 * tint = 12% 同色底 + 同色文字；ink = 文字色；fill = 实色；ring = 同色描边。
 */
export const TONE: Record<
  Tone,
  { tint: string; ink: string; fill: string; soft: string; border: string; glow: string }
> = {
  chat: {
    tint: "tint-chat",
    ink: "text-ink-chat",
    fill: "bg-chat",
    soft: "bg-chat/8",
    border: "border-chat/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-chat)]",
  },
  member: {
    tint: "tint-member",
    ink: "text-ink-member",
    fill: "bg-member",
    soft: "bg-member/8",
    border: "border-member/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-member)]",
  },
  like: {
    tint: "tint-like",
    ink: "text-ink-like",
    fill: "bg-like",
    soft: "bg-like/8",
    border: "border-like/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-like)]",
  },
  social: {
    tint: "tint-social",
    ink: "text-ink-social",
    fill: "bg-social",
    soft: "bg-social/8",
    border: "border-social/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-social)]",
  },
  gift: {
    tint: "tint-gift",
    ink: "text-ink-gift",
    fill: "bg-gift",
    soft: "bg-gift/8",
    border: "border-gift/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-gift)]",
  },
  info: {
    tint: "tint-info",
    ink: "text-ink-info",
    fill: "bg-info",
    soft: "bg-info/8",
    border: "border-info/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-info)]",
  },
  ok: {
    tint: "tint-ok",
    ink: "text-ink-ok",
    fill: "bg-ok",
    soft: "bg-ok/8",
    border: "border-ok/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-ok)]",
  },
  warn: {
    tint: "tint-warn",
    ink: "text-ink-warn",
    fill: "bg-warn",
    soft: "bg-warn/8",
    border: "border-warn/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-warn)]",
  },
  err: {
    tint: "tint-err",
    ink: "text-ink-err",
    fill: "bg-err",
    soft: "bg-err/8",
    border: "border-err/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-err)]",
  },
  idle: {
    tint: "tint-idle",
    ink: "text-ink-idle",
    fill: "bg-idle",
    soft: "bg-idle/8",
    border: "border-idle/30",
    glow: "shadow-[0_0_24px_-6px_var(--c-idle)]",
  },
};
