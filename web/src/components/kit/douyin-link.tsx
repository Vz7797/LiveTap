import { ExternalLink } from "lucide-react";
import { cn } from "@/lib/utils";

/** 抖音直播间地址：只接受数字 web_rid，其余不生成链接 */
export function douyinLiveUrl(liveId: string) {
  return /^\d{4,20}$/.test(liveId) ? `https://live.douyin.com/${liveId}` : undefined;
}

/**
 * 在新标签页打开抖音直播间。放在可点击的卡片或行里时阻止事件冒泡，点链接不会同时打开详情。
 * variant="icon" 为紧凑图标（卡片、表格），"pill" 为带文字的按钮（详情面板）。
 */
export function DouyinLink({ liveId, variant = "icon", className }: { liveId: string; variant?: "icon" | "pill"; className?: string }) {
  const href = douyinLiveUrl(liveId);
  if (!href) return null;
  const stop = (e: React.SyntheticEvent) => e.stopPropagation();
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      onClick={stop}
      onKeyDown={stop}
      title="在抖音打开这个直播间"
      aria-label="在抖音打开这个直播间"
      className={cn(
        "inline-flex shrink-0 cursor-pointer items-center justify-center transition-colors",
        variant === "icon"
          ? "size-6 rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
          : "h-7 gap-1 rounded-full tint-chat px-2.5 text-xs font-medium hover:brightness-95",
        className,
      )}
    >
      <ExternalLink className={variant === "icon" ? "size-3.5" : "size-3.5"} />
      {variant === "pill" && "打开直播间"}
    </a>
  );
}
