// Local container smoke test: an internal Docker network blocks upstream access.
// Credentials exist only in process memory / container environment, never in output.
import { execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";

const root = fileURLToPath(new URL("../../", import.meta.url));
const suffix = randomBytes(5).toString("hex");
const name = `livetap-check-${suffix}`;
const clientName = `${name}-client`;
const nodeImage = "node:24-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1";
const image = "livetap:local";
const initial = randomBytes(24).toString("base64url");
const changed = randomBytes(24).toString("base64url");
const env = { ...process.env, LIVETAP_SECRET_KEY: randomBytes(32).toString("base64"), LIVETAP_ADMIN_INITIAL_PASSWORD: initial };
const docker = (...args) => {
  try { return execFileSync("docker", args, { cwd: root, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"], timeout: 90000 }).trim(); }
  catch (error) { let detail = String(error.stderr ?? ""); for (const value of [initial, changed, env.LIVETAP_SECRET_KEY]) detail = detail.replaceAll(value, "[redacted]"); throw new Error(`Docker ${args[0]} failed: ${detail}`); }
};
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
let base, cookie = "", csrf = "", apiKey = "";
// Run HTTP requests from a helper on the same internal network. Neither container
// has Internet access; request/response secrets travel only through process pipes.
async function containerFetch(url, options = {}) {
  const source = `let input="";process.stdin.on("data",c=>input+=c);process.stdin.on("end",async()=>{try{const {url,options}=JSON.parse(input);const r=await fetch(url,{...options,signal:AbortSignal.timeout(10000)});process.stdout.write(JSON.stringify({status:r.status,headers:Object.fromEntries(r.headers),body:await r.text()}));}catch{process.exit(1)}});`;
  let result;
  try { result = JSON.parse(execFileSync("docker", ["exec", "-i", clientName, "node", "-e", source], { input: JSON.stringify({ url, options }), encoding: "utf8", stdio: ["pipe", "pipe", "pipe"], timeout: 15000 })); }
  catch { throw new Error("isolated HTTP request failed"); }
  return { ok: result.status >= 200 && result.status < 300, status: result.status, headers: { get: name => result.headers[name] }, text: async () => result.body, json: async () => JSON.parse(result.body) };
}
async function request(path, method = "GET", body) {
  const response = await containerFetch(base + "/admin/api" + path, { method, headers: { "Content-Type": "application/json", Cookie: cookie, "X-CSRF-Token": csrf }, body: body === undefined ? undefined : JSON.stringify(body) });
  if (!response.ok) throw new Error(`${method} ${path} returned ${response.status}`);
  const setCookie = response.headers.get("set-cookie");
  if (setCookie) cookie = setCookie.split(";")[0];
  const data = await response.json();
  if (data.csrf) csrf = data.csrf;
  return data;
}
async function ready() {
  for (let i = 0; i < 60; i++) {
    try { if ((await containerFetch(base + "/healthz")).ok) return; } catch {}
    await wait(500);
  }
  throw new Error("container startup timeout");
}
// Seed counts on either side of Beijing midnight without starting a collector.
// The helper shares only this test's temporary volume; values contain no secrets.
function seedMidnightCounts() {
  const source = `
    const { DatabaseSync } = require("node:sqlite");
    const parts = Object.fromEntries(new Intl.DateTimeFormat("en-CA", {
      timeZone: "Asia/Shanghai", year: "numeric", month: "numeric", day: "numeric"
    }).formatToParts(new Date()).map(p => [p.type, p.value]));
    const midnight = Date.UTC(Number(parts.year), Number(parts.month) - 1, Number(parts.day)) - 8 * 3600000;
    const db = new DatabaseSync("/data/collector.db");
    db.exec("PRAGMA busy_timeout=5000");
    const insert = db.prepare("INSERT OR REPLACE INTO room_stats_minute (live_id,minute,counts,reconnects,silence,state) VALUES (?,?,?,0,0,'stopped')");
    insert.run("timezone-fixture", midnight - 60000, JSON.stringify({ chat: 11 }));
    insert.run("timezone-fixture", midnight, JSON.stringify({ chat: 7 }));
    db.close();
  `;
  docker("exec", clientName, "node", "-e", source);
}
function assertBeijingMidnight(snapshot) {
  assert.equal(snapshot.totals.chat, 7, "today must exclude counts before Beijing midnight");
  assert.equal(snapshot.rhythm.today[0], 7, "midnight counts belong in today's first bucket");
  assert.equal(snapshot.rhythm.yesterday[47], 11, "23:59 counts belong in yesterday's last bucket");
}
const result = { mode: "local-container-no-upstream", image, checks: [] };
try {
	docker("compose", "-f", "deploy/compose.yaml", "config", "--quiet");
  docker("network", "create", "--internal", name);
  docker("volume", "create", name);
  docker("run", "-d", "--name", name, "--network", name, "--read-only", "--cap-drop=ALL", "--security-opt", "no-new-privileges:true", "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m", "--mount", `type=volume,src=${name},dst=/data`, "--env", "LIVETAP_SECRET_KEY", "--env", "LIVETAP_ADMIN_INITIAL_PASSWORD", image);
  docker("run", "-d", "--name", clientName, "--network", name, "--user", "65532:65532", "--read-only", "--cap-drop=ALL", "--mount", `type=volume,src=${name},dst=/data`, nodeImage, "node", "-e", "setInterval(()=>{},60000)");
  base = `http://${name}:8090`;
  assert.equal(docker("network", "inspect", "--format", "{{.Internal}}", name), "true");
  await ready();
  assert.match(await (await containerFetch(base)).text(), /<html/);
  docker("exec", name, "/collector", "--healthcheck");
  assert.equal(docker("inspect", "--format", "{{.Config.User}}", name), "65532:65532");
  result.checks.push("nonroot-startup-and-embedded-ui", "healthcheck");
  await request("/login", "POST", { username: "admin", password: initial });
  await request("/password", "POST", { current_password: initial, new_password: changed });
  result.checks.push("login-and-required-password-change");
  {
    const created = await request("/clients", "POST", { name: "隔离接入方", env: "测试", enabled: true, quota: 2, whitelist: [] });
    apiKey = created.api_key;
    assert.ok(typeof apiKey === "string" && apiKey.length === 51, "API key generation failed");
    assert.ok(!JSON.stringify(await request("/clients")).includes(apiKey), "client list exposed key");
    const node = await containerFetch(base + "/v1/node", { headers: { Authorization: `Bearer ${apiKey}` } });
    assert.equal(node.status, 200);
    const nodeInfo = await node.json();
    assert.equal(nodeInfo.capacity.max, 200);
    assert.equal(nodeInfo.api_version, "v1");
    assert.equal((await containerFetch(base + "/v1/node", { headers: { Cookie: cookie } })).status, 401);
    assert.equal((await containerFetch(base + "/admin/api/snapshot", { headers: { Authorization: `Bearer ${apiKey}` } })).status, 401);
    result.checks.push("client-create-once-only-key", "northbound-node-and-auth-isolation");
  }
  const added = await request("/rooms", "POST", { live_ids: ["123456789"], enabled: false, tags: ["container-test"] });
  assert.equal(added.results[0].ok, true);
  const accountSecret = `sessionid=${randomBytes(24).toString("hex")}`;
  const proxy = await request("/proxies", "POST", { name: "隔离环境代理", enabled: true, limit: 2, tags: [], secret: "http://127.0.0.1:19090" });
  const account = await request("/accounts", "POST", { name: "离线小号", enabled: true, limit: 2, tags: [], small_account: true, secret: accountSecret });
  assert.ok(!JSON.stringify(account).includes(accountSecret));
  for (const [kind, resource] of [["proxy", proxy], ["account", account]]) await request("/rooms/123456789/assignment", "POST", { kind, resource_id: resource.id });
  seedMidnightCounts();
  await wait(2800);
  let snapshot = await request("/snapshot");
  assert.equal(snapshot.rooms[0].status, "stopped");
  assert.equal(snapshot.node.reqPerMin, 0);
  assert.equal(snapshot.node.homepageRequestsPerMinute, 0);
  assertBeijingMidnight(snapshot);
  result.checks.push("beijing-midnight-totals-and-rhythm");
  result.checks.push("add-paused-room", "encrypted-pool-crud-and-room-assignment", "zero-upstream-requests");
  for (const path of ["/alerts", "/alert-rules", "/logs", "/lab/runs"]) await request(path);
  docker("restart", name);
  await ready();
  cookie = ""; csrf = "";
  await request("/login", "POST", { username: "admin", password: changed });
  await wait(2800);
  snapshot = await request("/snapshot");
  assert.equal(snapshot.rooms[0].liveId, "123456789");
  assert.equal(snapshot.rooms[0].status, "stopped");
  assert.equal(snapshot.node.reqPerMin, 0);
  assertBeijingMidnight(snapshot);
  assert.equal((await request("/accounts")).length, 1);
  const logs = docker("logs", name);
  for (const secret of [initial, changed, accountSecret, env.LIVETAP_SECRET_KEY, apiKey].filter(Boolean)) assert.ok(!logs.includes(secret), "credential appeared in container logs");
  result.checks.push("volume-persistence-and-relogin", "log-redaction");
  for (let i=0; i<40 && docker("inspect", "--format", "{{.State.Health.Status}}", name)!=="healthy"; i++) await wait(500);
  assert.equal(docker("inspect", "--format", "{{.State.Health.Status}}", name), "healthy");
  {
    docker("stop", name);
    docker("rm", name);
    docker("run", "-d", "--name", name, "--network", name, "--read-only", "--cap-drop=ALL", "--security-opt", "no-new-privileges:true", "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m", "--mount", `type=volume,src=${name},dst=/data`, "--env", "LIVETAP_SECRET_KEY", "--env", "LIVETAP_ADMIN_LISTEN=0.0.0.0:8091", image);
    await ready();
    const publicBase = base;
    assert.equal((await containerFetch(publicBase + "/admin/api/snapshot", { headers: { Cookie: cookie } })).status, 404);
    assert.equal((await containerFetch(publicBase + "/v1/node", { headers: { Authorization: `Bearer ${apiKey}` } })).status, 200);
    base = `http://${name}:8091`;
    assert.equal((await containerFetch(base + "/v1/node", { headers: { Authorization: `Bearer ${apiKey}` } })).status, 404);
    cookie = ""; csrf = "";
    await request("/login", "POST", { username: "admin", password: changed });
    assert.equal((await request("/snapshot")).node.reqPerMin, 0);
    docker("exec", name, "/collector", "--healthcheck");
    result.checks.push("separate-listeners-and-persisted-key", "zero-upstream-after-listener-restart");
  }
  result.health = "healthy";
  result.user = "65532:65532";
  result.imageSizeBytes = Number(docker("image", "inspect", "--format", "{{.Size}}", image));
  result.imageId = docker("image", "inspect", "--format", "{{.Id}}", image);
  result.passed = true;
} catch (error) {
  try {
    console.error("Container status:", docker("inspect", "--format", "{{.State.Status}} {{.State.ExitCode}} {{.State.Error}}", name));
  } catch {}
  throw error;
} finally {
  for (const args of [["rm", "-f", clientName, name], ["volume", "rm", name], ["network", "rm", name]]) { try { docker(...args); } catch {} }
}
const output = new URL("../../test-results/docker/", import.meta.url);
mkdirSync(output, { recursive: true });
writeFileSync(new URL("docker-check.json", output), JSON.stringify(result, null, 2) + "\n");
console.log(JSON.stringify(result, null, 2));
