import { Check, ChevronDown, type LucideIcon } from "lucide-react";
import { Select as Primitive } from "radix-ui";
import type { ReactNode } from "react";
import type { Tone } from "@/lib/meta";
import { TONE } from "@/lib/tone";
import { cn } from "@/lib/utils";

export interface SelectOption {
  value: string;
  label: ReactNode;
  /** 右侧弱化说明，例如数量或“推荐” */
  hint?: ReactNode;
  /** 左侧彩色圆点，例如告警级别 */
  tone?: Tone;
  icon?: LucideIcon;
  disabled?: boolean;
}

// Radix 不允许空字符串作为选项值；业务里常用 "" 表示“全部/不指定”，在这里转换。
const EMPTY = "__empty__";
const toInner = (v: string) => (v === "" ? EMPTY : v);
const toOuter = (v: string) => (v === EMPTY ? "" : v);

/**
 * 统一的下拉选择：替代浏览器原生 select。
 * 触发器与输入框同高同圆角；弹出层与触发器同宽，选项支持图标、色点和右侧说明，内容多时内部滚动。
 */
export function SelectBox({
  value,
  onChange,
  options,
  ariaLabel,
  placeholder = "请选择",
  className,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  ariaLabel?: string;
  placeholder?: string;
  className?: string;
  disabled?: boolean;
}) {
  const current = options.find((o) => o.value === value);
  return (
    <Primitive.Root value={toInner(value)} onValueChange={(v) => onChange(toOuter(v))} disabled={disabled}>
      <Primitive.Trigger
        aria-label={ariaLabel}
        className={cn(
          "group flex h-10 w-full min-w-0 cursor-pointer items-center gap-2 rounded-xl border bg-background px-3 text-left text-sm transition-[border-color,box-shadow] outline-none",
          "hover:border-border-strong focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/25 data-[state=open]:border-ring data-[state=open]:ring-3 data-[state=open]:ring-ring/20",
          "disabled:cursor-not-allowed disabled:opacity-50",
          className,
        )}
      >
        <OptionBody option={current} fallback={<span className="text-muted-foreground">{placeholder}</span>} />
        <ChevronDown className="ml-auto size-4 shrink-0 text-muted-foreground transition-transform duration-200 group-data-[state=open]:rotate-180" />
      </Primitive.Trigger>
      <Primitive.Portal>
        <Primitive.Content
          position="popper"
          sideOffset={6}
          collisionPadding={12}
          className={cn(
            "z-[80] w-(--radix-select-trigger-width) min-w-44 overflow-hidden rounded-2xl border border-border-strong bg-popover text-popover-foreground shadow-xl",
            "origin-(--radix-select-content-transform-origin) data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-[0.97] data-[side=bottom]:slide-in-from-top-1 data-[side=top]:slide-in-from-bottom-1",
          )}
        >
          <Primitive.Viewport className="scroll-area overscroll-contain max-h-[min(320px,var(--radix-select-content-available-height))] p-1.5">
            {options.map((o) => (
              <Primitive.Item
                key={o.value}
                value={toInner(o.value)}
                data-value={o.value}
                disabled={o.disabled}
                className={cn(
                  "relative flex cursor-pointer items-center gap-2 rounded-xl py-2 pr-8 pl-2.5 text-sm outline-none select-none",
                  "data-[highlighted]:bg-accent data-[state=checked]:font-medium data-[disabled]:pointer-events-none data-[disabled]:opacity-45",
                )}
              >
                <Primitive.ItemText asChild>
                  <span className="flex min-w-0 flex-1 items-center gap-2">
                    <OptionBody option={o} />
                  </span>
                </Primitive.ItemText>
                {o.hint != null && <span className="shrink-0 text-xs text-muted-foreground">{o.hint}</span>}
                <Primitive.ItemIndicator className="absolute right-2.5 text-ink-chat">
                  <Check className="size-4" strokeWidth={2.5} />
                </Primitive.ItemIndicator>
              </Primitive.Item>
            ))}
          </Primitive.Viewport>
        </Primitive.Content>
      </Primitive.Portal>
    </Primitive.Root>
  );
}

function OptionBody({ option, fallback }: { option?: SelectOption; fallback?: ReactNode }) {
  if (!option) return <span className="min-w-0 flex-1 truncate">{fallback}</span>;
  const Icon = option.icon;
  return (
    <span className="flex min-w-0 flex-1 items-center gap-2">
      {option.tone && <span className={cn("size-2 shrink-0 rounded-full", TONE[option.tone].fill)} />}
      {Icon && <Icon className="size-4 shrink-0 text-muted-foreground" />}
      <span className="min-w-0 truncate">{option.label}</span>
    </span>
  );
}
