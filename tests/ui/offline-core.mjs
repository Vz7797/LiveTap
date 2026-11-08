// Completely offline UI fixture. No collector process or upstream connection.
import fs from "node:fs";
import path from "node:path";
import http from "node:http";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(path.join(root, "web/package.json"));
const { chromium } = require("playwright-core");
const dist = path.join(root, "web/dist");
const output = path.join(root, "test-results/offline-core");
fs.mkdirSync(output, { recursive: true });
const zero = { chat: 0, member: 0, like: 0, social: 0, gift: 0 };
const snapshot = {
  tick: 0, rooms: [], feed: [], seq: 0, series: [{ at: Date.now(), v: zero }], totals: zero, incidents: [], timeline: [], logs: [], proxies: [], accounts: [], clients: [],
  history: [], rhythm: { today: Array(48).fill(0), yesterday: Array(48).fill(0) }, rollout: { running: false, done: 0, total: 0 },
  node: { name: "离线界面检查", version: "1.0.0", engineVersion: "v2.3.1 + collector hooks", bootId: "offline", startedAt: Date.now(), cpu: 0, memMB: 0, memLimitMB: 0, goroutines: 0, conns: 0, reqPerMin: 12, reqBudgetPerSec: 5, maxRooms: 200, health: 100, cpuHist: [], memHist: [], reqHist: [], netHist: [], bandwidthAvailable: false, shareGuestIdentity: false, homepageRequestsPerMinute: 4, cooldown: { active: true, until: Date.now() + 120000, remainingSeconds: 120, strikes: 1 } },
};
const defaults = { engine: "douyinlive", proxy_url: "", cookie: "", sign_provider: "local", tikhub_token: "", protocol_mode: "web", poll_seconds: 15, silence_seconds: 0, event_types: Object.keys(zero), cookie_configured: false, proxy_url_configured: false, tikhub_token_configured: false };
snapshot.node.upKBs = 0;
snapshot.node.downKBs = 0;
const server = http.createServer((request, response) => {
  const pathname = new URL(request.url, "http://localhost").pathname;
  const file = path.resolve(dist, "." + (pathname === "/" ? "/index.html" : pathname));
  if (!file.startsWith(dist + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
    response.writeHead(404).end();
    return;
  }
  const mime = { ".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".woff2": "font/woff2", ".woff": "font/woff", ".svg": "image/svg+xml" };
  response.setHeader("Content-Type", mime[path.extname(file)] ?? "application/octet-stream");
  response.end(fs.readFileSync(file));
});
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH ?? "C:/Program Files/Google/Chrome/Application/chrome.exe", headless: true });
const results = [];
try {
  for (const width of [1440, 390]) {
    for (const theme of ["light", "dark"]) {
      let settings = { defaults, share_guest_identity: false, max_rooms: 200, starts_per_second: 2, requests_per_second: 5, rolling_seconds: 10, stats_days: 30, event_days: 90 };
      const context = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: theme, reducedMotion: "reduce" });
      await context.addInitScript(value => localStorage.setItem("livetap-theme", value), theme);
      await context.routeWebSocket("**/*", socket => socket.send(JSON.stringify(snapshot)));
      await context.route("**/*", async route => {
        const url = new URL(route.request().url());
        if (url.origin !== base) return route.abort();
        if (!url.pathname.startsWith("/admin/api/")) return route.continue();
        let data = {};
        if (url.pathname.endsWith("/session")) data = { authenticated: true, must_change_password: false, csrf: "offline-test" };
        if (url.pathname.endsWith("/snapshot")) data = snapshot;
        if (url.pathname.endsWith("/settings")) {
          if (route.request().method() === "PUT") settings = { ...settings, ...route.request().postDataJSON().settings };
          data = settings;
        }
        await route.fulfill({ json: data });
      });
      const page = await context.newPage();
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
      for (const route of ["overview", "settings?tab=capacity"]) {
        await page.goto(`${base}/#/${route}`);
        await page.waitForTimeout(1500);
        if (errors.length) throw new Error(errors.join("\n"));
        if (route === "overview") {
          await page.getByText("节点冷却中，剩余", { exact: false }).waitFor();
          await page.getByText("首页请求", { exact: false }).waitFor();
        } else {
          const toggle = page.getByRole("checkbox", { name: /节点内共享游客身份/ });
          assert.equal(await toggle.isChecked(), false);
          await toggle.check();
          await page.getByRole("button", { name: "保存", exact: true }).click();
          await page.getByText("容量与限速已生效", { exact: true }).waitFor();
          assert.equal(settings.share_guest_identity, true);
        }
        await page.evaluate(() => window.scrollTo(0, 0));
        await page.waitForTimeout(250);
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
        const file = `${width}-${theme}-${route.startsWith("settings") ? "settings" : "overview"}.png`;
        await page.screenshot({ path: path.join(output, file), fullPage: true });
        results.push({ file, horizontalOverflowPx: overflow, errors: [...errors] });
        assert.ok(overflow <= 0);
        assert.deepEqual(errors, []);
      }
      await context.close();
    }
  }
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
  fs.writeFileSync(path.join(output, "ui-check.json"), JSON.stringify({ mode: "offline-fixtures", results }, null, 2) + "\n");
}
console.log(`离线界面 ${results.length} 项通过；没有启动采集服务或访问抖音。`);
