import type { ComponentType } from "react";
import type { PageKey } from "@/app/router";

/** 各页面的脚本加载函数；同一个 import() 只会下载一次，预取与正式加载共用缓存。 */
export const PAGE_LOADERS = {
  overview: () => import("@/pages/overview").then((m) => ({ default: m.OverviewPage })),
  rooms: () => import("@/pages/rooms").then((m) => ({ default: m.RoomsPage })),
  messages: () => import("@/pages/messages").then((m) => ({ default: m.MessagesPage })),
  proxies: () => import("@/pages/proxies").then((m) => ({ default: m.ProxiesPage })),
  accounts: () => import("@/pages/accounts").then((m) => ({ default: m.AccountsPage })),
  clients: () => import("@/pages/clients").then((m) => ({ default: m.ClientsPage })),
  docs: () => import("@/pages/docs").then((m) => ({ default: m.DocsPage })),
  lab: () => import("@/pages/lab").then((m) => ({ default: m.LabPage })),
  alerts: () => import("@/pages/alerts").then((m) => ({ default: m.AlertsPage })),
  settings: () => import("@/pages/settings").then((m) => ({ default: m.SettingsPage })),
} satisfies Record<PageKey, () => Promise<{ default: ComponentType }>>;

/** 已下载完成的页面组件：直接同步渲染，跳过 React.lazy 首次进入时的一帧加载占位。 */
export const LOADED_PAGES: Partial<Record<PageKey, ComponentType>> = {};

/** 下载并记住页面组件；预取、空闲预加载和正式进入共用。 */
export function loadPage(page: PageKey) {
  return PAGE_LOADERS[page]().then((m) => {
    LOADED_PAGES[page] = m.default;
    return m;
  });
}

/** 预取某个页面的脚本（鼠标移到侧栏菜单时调用），失败时静默，正式进入页面会再试。 */
export function prefetchPage(page: PageKey) {
  void loadPage(page).catch(() => undefined);
}

