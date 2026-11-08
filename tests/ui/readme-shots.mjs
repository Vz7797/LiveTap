// README screenshots from the built-in demo simulator (web/src/mock). Fully offline:
// every admin API call and the snapshot WebSocket are answered in the browser with
// fictional data, so no real streamer, account or credential can appear.
// Usage: pnpm --dir web build && node tests/ui/readme-shots.mjs
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const web = path.join(root, "web");
const requireWeb = createRequire(path.join(web, "package.json"));
const { chromium } = requireWeb("playwright-core");
const { createServer } = await import(pathToFileURL(requireWeb.resolve("vite")).href);
const dist = path.join(web, "dist");
const out = path.join(root, "docs/images");
fs.mkdirSync(out, { recursive: true });
if (!fs.existsSync(path.join(dist, "index.html"))) throw new Error("run `pnpm --dir web build` first");

// 1. Run the demo simulator for a few simulated minutes so charts and feeds are filled.
const vite = await createServer({ root: web, logLevel: "error", server: { middlewareMode: true }, appType: "custom" });
const { sim } = await vite.ssrLoadModule("/src/mock/sim.ts");
for (let i = 0; i < 96; i++) sim.step();
const state = structuredClone(sim.state);
await vite.close();

const now = Date.now();
const MIN = 60_000;
const shape = (t) => { const h = new Date(t).getHours() + new Date(t).getMinutes() / 60; return 0.25 + 0.75 * Math.max(0, Math.sin(((h - 9) / 15) * Math.PI)) ** 1.4; };
const scale = (v, k) => Object.fromEntries(Object.entries(v).map(([key, n]) => [key, Math.round(n * k * (0.9 + Math.random() * 0.2))]));
const base = state.series.at(-1)?.v ?? { chat: 300, member: 400, like: 2000, social: 30, gift: 20 };
const history = Array.from({ length: 24 * 60 }, (_, i) => { const at = now - (24 * 60 - i) * MIN; return { at, v: scale(base, shape(at)) }; });
const half = (offset) => Array.from({ length: 48 }, (_, i) => { const at = new Date(now).setHours(0, 0, 0, 0) - offset + i * 30 * MIN; return at > now ? 0 : Math.round(base.chat * 30 * shape(at)); });
const data = {
  ...state,
  node: { ...state.node, name: "node-01", version: "1.0.0", engineVersion: "v2.3.2" },
  history,
  rhythm: { today: half(0), yesterday: half(24 * 60 * MIN) },
  rollout: { running: false, paused: false, done: 0, total: 0 },
};
const byId = new Map(data.rooms.map((r) => [r.liveId, r]));

// 2. Serve the built admin UI and answer the admin API with the demo state.
const server = http.createServer((req, res) => {
  const pathname = new URL(req.url, "http://localhost").pathname;
  let file = path.resolve(dist, "." + pathname);
  if (!file.startsWith(dist) || !fs.existsSync(file) || !fs.statSync(file).isFile()) file = path.join(dist, "index.html");
  res.setHeader("Content-Type", { ".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml", ".woff2": "font/woff2" }[path.extname(file)] ?? "application/octet-stream");
  res.end(fs.readFileSync(file));
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;

function answer(endpoint, query) {
  if (endpoint === "/session") return { authenticated: true, must_change_password: false, csrf: "demo" };
  if (endpoint === "/snapshot") return data;
  if (endpoint === "/history") return history;
  if (endpoint === "/feed") { const types = (query.get("types") ?? "").split(","); return data.feed.filter((m) => types.includes(m.type)); }
  const detail = endpoint.match(/^\/rooms\/([^/]+)\/detail$/);
  if (detail) return byId.get(decodeURIComponent(detail[1])) ?? {};
  if (["/settings", "/alerts/summary", "/logs/stats", "/alert-rules"].includes(endpoint)) return {};
  return [];
}

const SHOTS = [
  ["overview", "/overview"],
  ["rooms", "/rooms"],
  ["messages", "/messages"],
];

const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH ?? "C:/Program Files/Google/Chrome/Application/chrome.exe", headless: true });
try {
  for (const theme of ["light", "dark"]) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1, colorScheme: theme, reducedMotion: "reduce" });
    await context.addInitScript((value) => localStorage.setItem("livetap-theme", value), theme);
    await context.routeWebSocket("**/*", (socket) => socket.send(JSON.stringify(data)));
    await context.route("**/*", (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== origin) return route.abort();
      if (!url.pathname.startsWith("/admin/api/")) return route.continue();
      return route.fulfill({ json: answer(url.pathname.slice("/admin/api".length), url.searchParams) });
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(String(e)));
    for (const [name, target] of SHOTS) {
      await page.goto(origin + target);
      await page.waitForLoadState("networkidle");
      await page.waitForTimeout(1200);
      await page.screenshot({ path: path.join(out, `${name}-${theme}.png`) });
      console.log(`${name}-${theme}.png`);
    }
    if (errors.length) throw new Error(errors.join("\n"));
    await context.close();
  }
} finally {
  await browser.close();
  server.close();
}
process.exit(0);
