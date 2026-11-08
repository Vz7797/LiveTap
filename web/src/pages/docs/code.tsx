import { Check, Copy } from "lucide-react";
import { useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";

/* 文档用的代码块：多语言标签切换、一键复制、轻量语法着色（只做字符串、注释、数字、关键字与 JSON 键）。 */

export interface CodeTab {
  label: string;
  code: string;
}

const TOKEN =
  /("(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|`[^`]*`)|((?:^|(?<=\s))(?:\/\/|#)[^\n]*)|((?<![\w.:/-])\d+(?:\.\d+)?(?![\w.:/-]))|(\b(?:const|let|var|func|return|if|else|for|range|await|async|import|from|package|def|while|try|except|catch|new|true|false|null|nil|None|True|False|curl|go|type|struct)\b)/gm;

function highlight(code: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let i = 0;
  for (const m of code.matchAll(TOKEN)) {
    const at = m.index ?? 0;
    if (at > last) out.push(code.slice(last, at));
    const [text, str, comment, num] = m;
    // 字符串后面紧跟冒号的是 JSON 键
    const isKey = !!str && /^\s*:/.test(code.slice(at + text.length, at + text.length + 3));
    const cls = str ? (isKey ? "text-ink-info" : "text-ink-ok") : comment ? "text-faint italic" : num ? "text-ink-gift" : "text-ink-social";
    out.push(
      <span key={i++} className={cls}>
        {text}
      </span>,
    );
    last = at + text.length;
  }
  if (last < code.length) out.push(code.slice(last));
  return out;
}

export function CodeBlock({ tabs, title, className }: { tabs: CodeTab[]; title?: string; className?: string }) {
  const [active, setActive] = useState(0);
  const [copied, setCopied] = useState(false);
  const tab = tabs[Math.min(active, tabs.length - 1)];
  return (
    <div className={cn("overflow-hidden rounded-2xl border bg-muted/30", className)}>
      <div className="flex items-center gap-1 border-b bg-muted/40 px-2 py-1.5">
        {title && <span className="mr-2 pl-1.5 text-xs font-medium text-muted-foreground">{title}</span>}
        <div className="scrollbar-none flex min-w-0 gap-1 overflow-x-auto" role="tablist">
          {tabs.map((t, i) => (
            <button
              key={t.label}
              type="button"
              role="tab"
              aria-selected={i === active}
              onClick={() => {
                setActive(i);
                setCopied(false);
              }}
              className={cn(
                "shrink-0 cursor-pointer rounded-lg px-2.5 py-1 text-xs font-medium transition-colors",
                i === active ? "bg-card text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t.label}
            </button>
          ))}
        </div>
        <button
          type="button"
          onClick={async () => {
            try {
              await navigator.clipboard.writeText(tab.code);
              setCopied(true);
              window.setTimeout(() => setCopied(false), 1500);
            } catch {
              setCopied(false);
            }
          }}
          className={cn("ml-auto inline-flex shrink-0 cursor-pointer items-center gap-1 rounded-lg px-2 py-1 text-xs", copied ? "text-ink-ok" : "text-muted-foreground hover:bg-accent hover:text-foreground")}
          aria-label="复制代码"
        >
          {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
          {copied ? "已复制" : "复制"}
        </button>
      </div>
      <pre className="scroll-area max-h-[520px] p-4 text-[12.5px] leading-relaxed">
        <code className="font-mono">{highlight(tab.code)}</code>
      </pre>
    </div>
  );
}

/** 行内代码 */
export function C({ children }: { children: ReactNode }) {
  return <code className="rounded-md bg-muted px-1.5 py-0.5 font-mono text-[0.86em] text-foreground">{children}</code>;
}
