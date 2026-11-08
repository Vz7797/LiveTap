import { PageErrorBoundary } from "@/app/PageErrorBoundary";
import { Shell } from "@/app/Shell";
import { useRoute, type PageKey } from "@/app/router";
import { lazy, Suspense, useEffect, useMemo, type ComponentType } from "react";
import { LOADED_PAGES, PAGE_LOADERS, loadPage } from "@/app/pages";

const PAGES = Object.fromEntries((Object.keys(PAGE_LOADERS) as PageKey[]).map((key) => [key, lazy<ComponentType>(() => loadPage(key))])) as unknown as Record<PageKey, ComponentType>;

/** 首屏完成后在浏览器空闲时依次预取其余页面，切换页面时就不必再等下载。 */
function usePrefetchPagesWhenIdle(current: PageKey) {
  useEffect(() => {
    const keys = (Object.keys(PAGE_LOADERS) as PageKey[]).filter((k) => k !== current);
    let cancelled = false;
    const idle = (fn: () => void) => (typeof window.requestIdleCallback === "function" ? window.requestIdleCallback(fn, { timeout: 4000 }) : globalThis.setTimeout(fn, 1500));
    const next = () => {
      if (cancelled) return;
      const key = keys.shift();
      if (!key) return;
      loadPage(key).catch(() => undefined).finally(() => idle(next));
    };
    const start = window.setTimeout(() => idle(next), 1500);
    return () => { cancelled = true; window.clearTimeout(start); };
    // 只在首次进入时安排一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
}

/** 加载占位：与常见页面的标题和卡片网格同尺寸，避免切换时大块留白和内容跳动 */
function PageSkeleton() {
  return (
    <div role="status" aria-label="正在加载页面" className="animate-none">
      <div className="mb-6 flex items-center gap-3.5">
        <div className="size-11 rounded-2xl bg-muted" />
        <div className="space-y-2">
          <div className="h-5 w-32 rounded-md bg-muted" />
          <div className="h-3.5 w-56 rounded-md bg-muted/70" />
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="h-44 rounded-3xl border bg-card" />
        ))}
      </div>
    </div>
  );
}

export default function App() {
  const { page } = useRoute();
  // 脚本已预取完成的页面直接同步渲染；否则走懒加载并显示骨架占位。
  // 同一页面内组件类型必须保持不变：懒加载完成后若换成已加载的组件，React 会把页面卸载重建，
  // 刚打开的弹窗等状态会丢失（例如命令面板跳到 ?add=1、?create=1 打开的窗口一闪就没了）。
  const Page = useMemo(() => LOADED_PAGES[page] ?? PAGES[page], [page]);
  usePrefetchPagesWhenIdle(page);
  return (
    <Shell>
      <PageErrorBoundary resetKey={page}>
        <Suspense fallback={<PageSkeleton />}>
          <Page />
        </Suspense>
      </PageErrorBoundary>
    </Shell>
  );
}
