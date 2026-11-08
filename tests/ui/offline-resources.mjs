// Offline UI regression. Every API and WebSocket is intercepted; external requests are aborted.
import fs from "node:fs";
import path from "node:path";
import http from "node:http";
import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
// Custom select: open it, then pick the option by its value.
async function choose(page, name, value) {
  await page.getByRole("combobox", { name }).click();
  await page.locator(`[role="option"][data-value="${value}"]`).click();
}

const root = fileURLToPath(new URL("../../", import.meta.url));
const { chromium } = createRequire(path.join(root, "web/package.json"))("playwright-core");
const dist = path.join(root, "web/dist");
const output = path.join(root, "test-results/offline-resources");
fs.mkdirSync(output, { recursive: true });
const zero = { chat: 0, member: 0, like: 0, social: 0, gift: 0 };
const now = Date.now();
const defaults = { engine: "douyinlive", proxy_url: "", cookie: "", sign_provider: "local", tikhub_token: "", protocol_mode: "web", poll_seconds: 15, silence_seconds: 0, event_types: Object.keys(zero) };
const room = { liveId: "123456789", name: "离线界面样本", title: "暂停状态", city: "", category: "", status: "stopped", source: "manual", tags: [], engine: "douyinlive", sign: "local", protocol: "web", proxyId: null, accountId: null, online: 0, totalPv: 0, heat: 0, msgPerMin: 0, reconnects30m: 0, lastMsgAt: 0, since: now, spark: [0], counts: zero, subscribers: 0, silenceThreshold: 90, events: [], enabledTypes: Object.keys(zero), minutes: [zero], options: defaults };
const snapshot = {
  tick: 0, rooms: [room], feed: [], seq: 0, series: [{ at: now, v: zero }], totals: zero, incidents: [], timeline: [], logs: [], proxies: [], accounts: [], clients: [],
  history: [], rhythm: { today: Array(48).fill(0), yesterday: Array(48).fill(0) }, rollout: { running: false, done: 0, total: 0 },
  node: { name: "离线界面检查", version: "1.0.0", engineVersion: "v2.3.1", bootId: "offline", startedAt: now, cpu: 0, memMB: 0, memLimitMB: 0, goroutines: 0, conns: 0, reqPerMin: 0, reqBudgetPerSec: 5, maxRooms: 200, health: 100, cpuHist: [], memHist: [], reqHist: [], netHist: [], upKBs: 0, downKBs: 0, bandwidthAvailable: false, shareGuestIdentity: false, homepageRequestsPerMinute: 0, cooldown: { active: false, remainingSeconds: 0, strikes: 0 } },
};
const resource = kind => ({ id: kind + "-fixture", kind, name: kind === "proxy" ? "示例出口" : "专用小号", tags: ["离线样本"], enabled: true, limit: 5, state: "unknown", checked_at: 0, latency_ms: 0, detail: "尚未进行真实检测", mask: kind === "proxy" ? "http://***@127.0.0.1:19090" : "Cookie 已加密保存", rooms: [] });
const rules = { enabled: Object.fromEntries(["mass_failure", "silence", "risk", "capacity", "proxy_invalid", "account_invalid", "node_cooldown"].map(k => [k, true])), mass_percent: 30, capacity_percent: 85, webhook: "", webhook_configured: false };
const report = { id: "offline-report", status: "completed", started: now, finished: now + 60000, detail: "仅用于界面测试的报告样本", conclusion: "离线数据，不代表真实采集已验证", current_mode: "anonymous", current_sharing: false, current_stage: 1, phase: "finished", recent: Array.from({ length: 6 }, (_, i) => ({ at: now + i * 5000, stage: 1, collecting: 0, risk: 0, silent: 0, memory_mb: 30 + i, cpu: i / 2, homepage_requests_per_minute: 0, cooldown: { active: false, remainingSeconds: 0 } })), stages: [{ mode: "anonymous", sharing: false, size: 1, started: now, finished: now + 60000, minimum_collecting: 0, max_memory_mb: 35, max_cpu: 2.5, rooms: [{ id: room.liveId, disconnects: 0, recovery_ms: [], false_positives: 0, requests: 0, state: "stopped", reason: "离线样本" }], startup_unconnected: [{ id: room.liveId, reason: "离线样本" }] }] };
const server = http.createServer((request, response) => {
  const pathname = new URL(request.url, "http://localhost").pathname;
  const file = path.resolve(dist, "." + (pathname === "/" ? "/index.html" : pathname));
  if (!file.startsWith(dist + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) return response.writeHead(404).end();
  response.setHeader("Content-Type", ({ ".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".woff2": "font/woff2", ".woff": "font/woff", ".svg": "image/svg+xml" })[path.extname(file)] ?? "application/octet-stream");
  response.end(fs.readFileSync(file));
});
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH ?? "C:/Program Files/Google/Chrome/Application/chrome.exe", headless: true });
const results = [];
try {
  for (const width of [1440, 390]) for (const theme of ["light", "dark"]) {
    const data = { proxies: [resource("proxy")], accounts: [resource("account")], runs: [structuredClone(report)], rules: structuredClone(rules), alerts: [{ id: 1, at: now, rule: "node_cooldown", level: "warning", live_id: "", detail: "离线告警样本：冷却 120 秒", acked: false }] };
    const requests = [];
    const context = await browser.newContext({ viewport: { width, height: 900 }, colorScheme: theme, reducedMotion: "reduce" });
    await context.addInitScript(value => localStorage.setItem("livetap-theme", value), theme);
    await context.routeWebSocket("**/*", socket => socket.send(JSON.stringify(snapshot)));
    await context.route("**/*", async route => {
      const request = route.request(), url = new URL(request.url());
      if (url.origin !== base) return route.abort();
      if (!url.pathname.startsWith("/admin/api/")) return route.continue();
      const endpoint = url.pathname.slice("/admin/api".length), method = request.method();
      requests.push({ endpoint, method, search: url.search });
      let value = {};
      if (endpoint === "/session") value = { authenticated: true, must_change_password: false, csrf: "offline-test" };
      else if (endpoint === "/snapshot") value = snapshot;
      else if (endpoint.endsWith("/events")) value = [];
      else if (endpoint === "/settings") value = { defaults };
      else if (endpoint === "/proxies" || endpoint === "/accounts") {
        const key = endpoint.slice(1);
        if (method === "POST") {
          const body = request.postDataJSON();
          assert.ok(body.secret);
          data[key].push({ ...resource(key === "proxies" ? "proxy" : "account"), id: key + "-added", name: body.name });
        }
        value = data[key];
      } else if (endpoint === "/lab/runs") {
        if (method === "POST") {
          const plan = request.postDataJSON();
          assert.deepEqual(plan.sharing, [false, true]);
          assert.equal(plan.duration_seconds, 60);
          data.runs = [{ ...report, status: "running", phase: "observing" }];
        }
        value = data.runs;
      } else if (endpoint.endsWith("/stop")) data.runs[0].status = "cancelled";
      else if (endpoint.endsWith("/export")) value = report;
      else if (endpoint === "/alerts") value = data.alerts;
      else if (endpoint.endsWith("/ack")) { data.alerts[0].acked = true; value = { acked: 1 }; }
      else if (endpoint === "/alert-rules") {
        if (method === "PUT") data.rules = { ...request.postDataJSON(), webhook: "" };
        value = data.rules;
      } else if (endpoint === "/logs") value = [{ at: now, level: "WARN", message: "节点受限冷却（离线样本）", fields: {} }];
      await route.fulfill({ json: value });
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", e => errors.push(e.message));
    page.on("console", m => { if (m.type() === "error") errors.push(m.text()); });
    async function shot(name) {
      for (let i = 0; i < 6 && await page.getByRole("button", { name: "关闭通知", exact: true }).count(); i++) { await page.getByRole("button", { name: "关闭通知", exact: true }).first().click({ timeout: 500 }).catch(() => {}); await page.waitForTimeout(150); }
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.waitForTimeout(200);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
      const file = `${width}-${theme}-${name}.png`;
      await page.screenshot({ path: path.join(output, file), fullPage: true });
      results.push({ file, horizontalOverflowPx: overflow, errors: [...errors] });
      assert.ok(overflow <= 0, `${name} horizontal overflow ${overflow}`);
      assert.deepEqual(errors, []);
    }
    for (const kind of ["proxies", "accounts"]) {
      await page.goto(`${base}/#/${kind}`);
      await page.getByRole("button", { name: kind === "proxies" ? "添加代理" : "添加小号", exact: true }).click();
      await page.getByLabel("名称 / 备注").fill("新增离线样本");
      const input = page.getByLabel(kind === "proxies" ? "代理地址（HTTP / SOCKS5，可带认证）" : "小号 Cookie", { exact: true });
      assert.equal(await input.getAttribute("type"), "password");
      await shot(`${kind}-editor`);
      // Synthetic values remain in memory and are never captured in screenshots.
      await input.fill(kind === "proxies" ? "http://127.0.0.1:19091" : "sessionid=" + randomBytes(16).toString("hex"));
      if (kind === "accounts") await page.getByLabel("确认是专用小号，不是商家主账号").check();
      await page.getByRole("button", { name: "保存资源", exact: true }).click();
      await page.getByRole("heading", { name: "新增离线样本", exact: true }).waitFor();
      await choose(page, /^直播间/, room.liveId);
      await choose(page, kind === "proxies" ? /^代理资源/ : /^小号资源/, "auto:least_used");
      await page.getByRole("button", { name: "应用分配", exact: true }).click();
      await shot(kind);
    }
    await page.goto(`${base}/#/rooms?room=${room.liveId}`);
    await page.getByRole("tab", { name: "单独设置", exact: true }).click();
    await page.getByText("为本房间分配资源", { exact: true }).waitFor();
    await shot("room-settings");
    await page.goto(`${base}/#/lab`);
    await page.getByLabel("直播间样本（数字 ID，空格或换行分隔）").fill("223456789");
    await page.getByLabel("加量档位", { exact: false }).fill("1");
    await page.getByLabel("每档观察（分钟）").fill("1");
    await page.getByRole("button", { name: "开始实验", exact: true }).click();
    await page.getByRole("button", { name: "停止并清理", exact: true }).waitFor();
    await shot("lab");
    await page.getByRole("button", { name: "停止并清理", exact: true }).click();
    await page.getByText("已停止", { exact: true }).waitFor();
    const download = page.waitForEvent("download");
    await page.getByRole("button", { name: "导出 JSON" }).click();
    await (await download).delete();
    await page.goto(`${base}/#/alerts`);
    await page.getByRole("button", { name: "确认", exact: true }).click();
    await page.getByText("已确认 1 条告警", { exact: true }).waitFor();
    await shot("alerts");
    await page.goto(`${base}/#/alerts?tab=rules`);
    await page.getByLabel("容量将满（%）").fill("90");
    await page.getByRole("button", { name: "保存规则" }).click();
    await page.getByText("告警规则已保存", { exact: true }).waitFor();
    assert.equal(data.rules.capacity_percent, 90);
    await shot("rules");
    await page.goto(`${base}/#/alerts?tab=logs`);
    await page.getByRole("tab", { name: "WARN", exact: true }).click();
    await page.getByPlaceholder("关键字").fill("冷却");
    await page.getByRole("button", { name: "暂停", exact: true }).click();
    await shot("logs");
    assert.ok(requests.some(r => r.endpoint === "/logs" && new URLSearchParams(r.search).get("q") === "冷却"));
    assert.ok(requests.some(r => r.endpoint.endsWith("/assignment") && r.method === "POST"));
    await context.close();
  }
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
  fs.writeFileSync(path.join(output, "ui-check.json"), JSON.stringify({ mode: "offline-fixtures", results }, null, 2) + "\n");
}
console.log(`资源与告警离线界面 ${results.length} 项通过，没有访问抖音。`);
