// Offline browser checks only. API keys stay in memory and are never captured.
import fs from "node:fs";
import path from "node:path";
import http from "node:http";
import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const { chromium } = createRequire(path.join(root, "web/package.json"))("playwright-core");
const dist = path.join(root, "web/dist");
const output = path.join(root, "test-results/offline-clients");
fs.mkdirSync(output, { recursive: true });
const zero = { chat: 0, member: 0, like: 0, social: 0, gift: 0 };
const now = Date.now();
const fixture = { id: "client-fixture", name: "示例接入方 · 离线样本", env: "测试", enabled: true, quota: 50, used: 12, whitelist: ["10.0.0.0/24"], keyPrefix: "lt_live_••••", createdAt: now, lastSeen: now, connections: 2, pushPerSec: 18.5, bytesPerSec: 4096, dropped: 3, queue: 8, spark: [] };
const snapshot = {
  tick: 0, rooms: [], feed: [], seq: 0, series: [{ at: now, v: zero }], totals: zero, incidents: [], timeline: [], logs: [], proxies: [], accounts: [], clients: [],
  history: [], rhythm: { today: Array(48).fill(0), yesterday: Array(48).fill(0) }, rollout: { running: false, done: 0, total: 0 },
  node: { name: "离线界面检查", version: "1.0.0", engineVersion: "v2.3.1", bootId: "offline", startedAt: now, cpu: 0, memMB: 0, memLimitMB: 0, goroutines: 0, conns: 0, reqPerMin: 0, reqBudgetPerSec: 5, maxRooms: 200, health: 100, cpuHist: [], memHist: [], reqHist: [], netHist: [], upKBs: 0, downKBs: 0, bandwidthAvailable: false, northbound: { connections: 2, queue: 8, dropped: 3, bytes_per_sec: 4096, push_per_sec: 18.5, sent: 1110, bytes: 245760 } },
};
const server = http.createServer((request, response) => {
  const pathname = new URL(request.url, "http://localhost").pathname;
  const file = path.resolve(dist, "." + (pathname === "/" ? "/index.html" : pathname));
  if (!file.startsWith(dist + path.sep) || !fs.existsSync(file) || !fs.statSync(file).isFile()) return response.writeHead(404).end();
  response.setHeader("Content-Type", ({ ".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".woff2": "font/woff2", ".woff": "font/woff" })[path.extname(file)] ?? "application/octet-stream");
  response.end(fs.readFileSync(file));
});
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({ executablePath: process.env.CHROME_PATH ?? "C:/Program Files/Google/Chrome/Application/chrome.exe", headless: true });
const results = [];
try {
  for (const width of [1440, 390]) for (const theme of ["light", "dark"]) {
    const data = structuredClone(snapshot);
    data.clients = [structuredClone(fixture)];
    let oneTimeKey = "";
    const context = await browser.newContext({ viewport: { width, height: 1000 }, colorScheme: theme, reducedMotion: "reduce" });
    await context.addInitScript(value => localStorage.setItem("livetap-theme", value), theme);
    await context.routeWebSocket("**/*", socket => socket.send(JSON.stringify(data)));
    const operations = [];
    await context.route("**/*", async route => {
      const request = route.request(), url = new URL(request.url());
      if (url.origin !== base) return route.abort();
      if (!url.pathname.startsWith("/admin/api/")) return route.continue();
      const endpoint = url.pathname.slice("/admin/api".length), method = request.method();
      operations.push({ endpoint, method });
      let value = {};
      if (endpoint === "/session") value = { authenticated: true, must_change_password: false, csrf: "offline-fixture" };
      else if (endpoint === "/snapshot") value = data;
      else if (endpoint === "/clients" && method === "POST") {
        const body = request.postDataJSON();
        assert.deepEqual(body.whitelist, ["10.2.0.0/16"]);
        oneTimeKey = "lt_live_" + BigInt("0x" + randomBytes(32).toString("hex")).toString(36).slice(0, 43).padStart(43, "0");
        const client = { ...fixture, ...body, id: "created", used: 0, connections: 0, lastSeen: 0, queue: 0, dropped: 0 };
        data.clients.push(client);
        value = { client, api_key: oneTimeKey };
      } else if (endpoint.startsWith("/clients/") && method === "PUT") {
        const body = request.postDataJSON();
        data.clients = data.clients.map(c => c.id === endpoint.split("/").at(-1) ? { ...c, ...body, connections: 0, used: 0 } : c);
      } else if (endpoint.startsWith("/clients/") && method === "DELETE") {
        data.clients = data.clients.filter(c => c.id !== endpoint.split("/").at(-1));
      }
      await route.fulfill({ json: value });
    });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", () => errors.push("page-error"));
    page.on("console", message => { if (message.type() === "error") errors.push("console-error"); });
    async function shot(name) {
      await page.waitForTimeout(220);
      assert.equal(await page.locator("[data-private]").count(), 0, "refuse screenshot while a key is visible");
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
      assert.ok(overflow <= 0, "horizontal overflow");
      assert.deepEqual(errors, []);
      const file = `${width}-${theme}-${name}.png`;
      await page.screenshot({ path: path.join(output, file), fullPage: true });
      results.push({ file, horizontalOverflowPx: overflow, errors: [] });
    }
    await page.goto(base + "/#/clients");
    await page.getByText(fixture.name, { exact: true }).waitFor();
    await page.getByText("礼物、关注", { exact: true }).waitFor();
    await shot("clients");
    await page.getByRole("button", { name: "创建 API Key", exact: true }).click();
    assert.ok(await page.getByRole("button", { name: "生成 Key", exact: true }).isDisabled());
    await page.getByLabel("接入方名称", { exact: true }).fill("新增接入方");
    await page.getByLabel("来源 IP 白名单", { exact: true }).fill("10.2.0.0/16");
    await shot("client-create");
    await page.getByRole("button", { name: "生成 Key", exact: true }).click();
    await page.getByText("Key 已生成", { exact: true }).waitFor();
    assert.ok(await page.locator("[data-private]").textContent() === oneTimeKey, "one-time reveal mismatch");
    await page.getByRole("button", { name: "我已保存", exact: true }).click();
    await page.locator("[data-private]").waitFor({ state: "detached" });
    assert.ok(!(await page.content()).includes(oneTimeKey), "key retained in page after closing");
    const storage = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
    assert.ok(!storage.includes(oneTimeKey), "key persisted in browser storage");
    oneTimeKey = "";
    await page.getByRole("switch", { name: "启用 新增接入方", exact: true }).click();
    await page.waitForFunction(() => document.querySelector('[aria-label="启用 新增接入方"]')?.getAttribute("aria-checked") === "false");
    const card = page.locator("div").filter({ has: page.getByText("新增接入方", { exact: true }) }).filter({ has: page.getByRole("button", { name: "吊销", exact: true }) }).last();
    await card.getByRole("button", { name: "吊销", exact: true }).click();
    await card.getByRole("button", { name: "确认吊销", exact: true }).click();
    await page.getByRole("switch", { name: "启用 新增接入方", exact: true }).waitFor({ state: "detached" });
    assert.ok(operations.some(r => r.method === "POST" && r.endpoint === "/clients"));
    assert.ok(operations.some(r => r.method === "PUT" && r.endpoint === "/clients/created"));
    assert.ok(operations.some(r => r.method === "DELETE" && r.endpoint === "/clients/created"));
    await page.goto(base + "/#/overview");
    await page.getByText("接入方推送", { exact: true }).waitFor();
    await shot("overview");
    await context.close();
  }
} finally {
  await browser.close();
  await new Promise(resolve => server.close(resolve));
}
fs.writeFileSync(path.join(output, "ui-check.json"), JSON.stringify({ mode: "offline-mocked-api", passed: true, checks: results }, null, 2) + "\n");
console.log(`Clients offline UI checks passed: ${results.length} screenshots, plus creation/revocation and one-time key checks`);
