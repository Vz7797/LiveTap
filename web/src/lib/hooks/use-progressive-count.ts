import { startTransition, useEffect, useState } from "react";

/**
 * 分批渲染：先渲染 batch 条，之后每帧以低优先级再补 batch 条，直到全部渲染。
 * 进入长列表页面时首屏立即可见，避免一次性挂载几十上百个组件造成长任务（掉帧）。
 */
export function useProgressiveCount(total: number, batch = 24) {
  const [count, setCount] = useState(() => Math.min(total, batch));
  useEffect(() => {
    if (count >= total) return;
    const id = requestAnimationFrame(() => startTransition(() => setCount((c) => Math.min(total, c + batch))));
    return () => cancelAnimationFrame(id);
  }, [count, total, batch]);
  return Math.min(Math.max(count, Math.min(total, batch)), total);
}
