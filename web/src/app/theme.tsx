import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { useMotionSettings } from "./motion";

type Theme = "dark" | "light";
const KEY = "livetap-theme";

const Ctx = createContext<{ theme: Theme; toggle: (origin?: { x: number; y: number }) => void } | null>(null);

function readTheme(): Theme {
  try {
    return localStorage.getItem(KEY) === "light" ? "light" : "dark";
  } catch {
    return "dark";
  }
}

function apply(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark");
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    /* 隐私模式下忽略 */
  }
}

/** 主题：默认深色；切换时从点击位置做圆形扩散（View Transitions），减少动态效果时直接切换 */
export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setTheme] = useState<Theme>(readTheme);
  const { minimal } = useMotionSettings();

  const toggle = useCallback(
    (origin?: { x: number; y: number }) => {
      const next: Theme = theme === "dark" ? "light" : "dark";
      const reduce = minimal || window.matchMedia("(prefers-reduced-motion: reduce)").matches;
      const doc = document as Document & { startViewTransition?: (cb: () => void) => { finished: Promise<void> } };
      const commit = () =>
        flushSync(() => {
          apply(next);
          setTheme(next);
        });
      if (reduce || !doc.startViewTransition) {
        commit();
        return;
      }
      const root = document.documentElement;
      const x = origin?.x ?? window.innerWidth - 40;
      const y = origin?.y ?? 32;
      const r = Math.hypot(Math.max(x, window.innerWidth - x), Math.max(y, window.innerHeight - y));
      root.style.setProperty("--vt-x", `${x}px`);
      root.style.setProperty("--vt-y", `${y}px`);
      root.style.setProperty("--vt-r", `${r}px`);
      root.dataset.vt = "theme";
      doc.startViewTransition(commit).finished.finally(() => {
        delete root.dataset.vt;
      });
    },
    [theme, minimal],
  );

  return <Ctx.Provider value={{ theme, toggle }}>{children}</Ctx.Provider>;
}

export function useTheme() {
  const v = useContext(Ctx);
  if (!v) throw new Error("useTheme 需要在 ThemeProvider 内使用");
  return v;
}
