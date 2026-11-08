import type { LucideIcon } from "lucide-react";
import { Search, X } from "lucide-react";
import type { ReactNode } from "react";
import { Tabs, TabsList, TabsTrigger } from "@/components/motion/tabs";
import type { Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";

/** 胶囊标签切换：指示器弹簧滑动（beUI Tabs，layoutId） */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  size = "md",
  className,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode; icon?: LucideIcon; count?: number; disabled?: boolean }[];
  size?: "sm" | "md";
  className?: string;
}) {
  return (
    <Tabs value={value} onValueChange={(v) => onChange(v as T)} variant="pill" className={className}>
      <TabsList className="border bg-background/60" wrapperClassName="w-auto max-w-full">
        {options.map((o) => {
          const Icon = o.icon;
          return (
            <TabsTrigger
              key={o.value}
              value={o.value}
              disabled={o.disabled}
              className={cn("gap-1.5", size === "sm" ? "px-3 py-1 text-xs" : "px-3.5 py-1.5 text-[13px]", o.disabled && "cursor-not-allowed opacity-40")}
            >
              {Icon && <Icon className="size-3.5" />}
              {o.label}
              {o.count !== undefined && <span className="num text-[11px] opacity-60">{o.count}</span>}
            </TabsTrigger>
          );
        })}
      </TabsList>
    </Tabs>
  );
}

export function SearchInput({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <label
      className={cn(
        "flex h-9 items-center gap-2 rounded-full border bg-background/60 px-3 text-sm transition-colors focus-within:border-ring/60",
        className,
      )}
    >
      <Search className="size-4 shrink-0 text-muted-foreground" />
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-faint"
      />
      {value && (
        <button type="button" onClick={() => onChange("")} className="cursor-pointer text-muted-foreground hover:text-foreground" aria-label="清除">
          <X className="size-3.5" />
        </button>
      )}
    </label>
  );
}

/** 彩色可切换标签（如消息类型筛选） */
export function ToggleChip({
  active,
  onClick,
  tone,
  icon: Icon,
  children,
  className,
}: {
  active: boolean;
  onClick: () => void;
  tone: Tone;
  icon?: LucideIcon;
  children: ReactNode;
  className?: string;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex h-8 cursor-pointer items-center gap-1.5 rounded-full border px-3 text-xs font-medium transition-all active:scale-95",
        active ? cn(TONE[tone].tint, "border-transparent") : "border-border bg-transparent text-muted-foreground hover:text-foreground",
        className,
      )}
    >
      {Icon && <Icon className="size-3.5" />}
      {children}
    </button>
  );
}

export function Field({
  label,
  hint,
  children,
  className,
  inline,
}: {
  label: ReactNode;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
  inline?: boolean;
}) {
  // 纵向排列时说明文字放在输入框下方（与常见表单规范一致），同一行的多个字段输入框才能对齐
  if (!inline)
    return (
      <div className={cn("space-y-2", className)}>
        <div className="text-sm font-medium">{label}</div>
        {children}
        {hint && <div className="text-xs leading-relaxed text-muted-foreground">{hint}</div>}
      </div>
    );
  return (
    <div className={cn("flex items-center justify-between gap-4", className)}>
      <div className="min-w-0">
        <div className="text-sm font-medium">{label}</div>
        {hint && <div className="mt-0.5 text-xs text-muted-foreground">{hint}</div>}
      </div>
      {children}
    </div>
  );
}

export function TextInput({ className, ...rest }: React.ComponentProps<"input">) {
  return (
    <input
      className={cn(
        "h-9 w-full rounded-xl border bg-background/60 px-3 text-sm transition-colors outline-none placeholder:text-faint focus:border-ring/60",
        className,
      )}
      {...rest}
    />
  );
}
