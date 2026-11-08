import { memo, useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

/**
 * 滚动数字（里程表式）：每一位是 0–9 的竖向条带，数值变化时用 CSS transform 平移到新数字。
 *
 * - 只用 transform 过渡，由合成线程完成，不触发重排，几十个数字同时滚动也不卡。
 * - 位数按“从右往左”对齐：数字变长时新增的高位淡入，其余各位原地滚动。
 * - 首次渲染不滚动；系统开启“减少动态效果”时直接切换（见 index.css 的 .roll-strip）。
 * - 每一位保留一个隐形的同字符占位，保证与周围文字的基线对齐、宽度稳定。
 */
export const RollingNumber = memo(function RollingNumber({ text, className }: { text: string; className?: string }) {
  const chars = Array.from(text);
  // 首帧不播放：挂载后才打开过渡
  const [ready, setReady] = useState(false);
  const raf = useRef(0);
  useEffect(() => {
    raf.current = requestAnimationFrame(() => setReady(true));
    return () => cancelAnimationFrame(raf.current);
  }, []);
  return (
    <span className={cn("roll inline-flex", className)} aria-label={text} role="text">
      {chars.map((ch, i) => {
        // 从右往左编号，数字变长时已有各位保持同一个 key，不会整体重建
        const key = chars.length - i;
        return /\d/.test(ch) ? <Digit key={key} digit={Number(ch)} animate={ready} /> : <span key={`s${key}-${ch}`} aria-hidden>{ch}</span>;
      })}
    </span>
  );
});

const DIGITS = [0, 1, 2, 3, 4, 5, 6, 7, 8, 9];

function Digit({ digit, animate }: { digit: number; animate: boolean }) {
  return (
    <span className="roll-slot" aria-hidden>
      <span className="invisible">0</span>
      <span className={cn("roll-strip", animate && "roll-animate")} style={{ transform: `translateY(${-digit * 10}%)` }}>
        {DIGITS.map((d) => (
          <span key={d}>{d}</span>
        ))}
      </span>
    </span>
  );
}
