import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { fileURLToPath } from "node:url";
import { verifyEngine } from "../../deploy/verify-engine.mjs";

const root = fileURLToPath(new URL("../../", import.meta.url));

function fixture(t) {
  const temporaryRoot = path.join(root, "test-results/engine-tests");
  fs.mkdirSync(temporaryRoot, { recursive: true });
  const dir = fs.mkdtempSync(path.join(temporaryRoot, "case-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const enginePath = path.join(dir, "engine");
  fs.mkdirSync(enginePath);
  fs.writeFileSync(path.join(enginePath, "sample.txt"), "patched\n");
  const baselinePath = path.join(dir, "baseline.json");
  fs.writeFileSync(baselinePath, JSON.stringify({ version: "v2.3.2", commit: "3b0c44876c2a9221cb11d27041af83720b9210c5", files: { "sample.txt": createHash("sha256").update("original\n").digest("hex") } }));
  const patchPath = path.join(dir, "change.patch");
  fs.writeFileSync(patchPath, "diff --git a/sample.txt b/sample.txt\n--- a/sample.txt\n+++ b/sample.txt\n@@ -1 +1 @@\n-original\n+patched\n");
  return { enginePath, baselinePath, patchPath, temporaryRoot: path.join(dir, "scratch") };
}

test("离线反向补丁能还原基线，原文件不改变", t => {
  const config = fixture(t);
  assert.equal(verifyEngine(config).files, 1);
  assert.equal(fs.readFileSync(path.join(config.enginePath, "sample.txt"), "utf8"), "patched\n");
});

test("拒绝内核漂移、额外文件和基线哈希错误", t => {
  const config = fixture(t);
  fs.writeFileSync(path.join(config.enginePath, "extra.txt"), "unexpected");
  assert.throws(() => verifyEngine(config), /文件清单/);
  fs.unlinkSync(path.join(config.enginePath, "extra.txt"));
  const baseline = JSON.parse(fs.readFileSync(config.baselinePath));
  baseline.files["sample.txt"] = "0".repeat(64);
  fs.writeFileSync(config.baselinePath, JSON.stringify(baseline));
  assert.throws(() => verifyEngine(config), /基线校验失败/);
  fs.writeFileSync(path.join(config.enginePath, "sample.txt"), "drifted\n");
  assert.throws(() => verifyEngine(config), /补丁不能还原/);
});

test("后端只使用仓库内相对 replace，工作区不依赖本机路径", () => {
  const mod = fs.readFileSync(path.join(root, "server/go.mod"), "utf8");
  assert.match(mod, /replace github\.com\/jwwsjlm\/douyinLive\/v2 => \.\.\/third_party\/douyinLive/);
  assert.equal(fs.existsSync(path.join(root, "go.work")), false);
  assert.equal(fs.existsSync(path.join(root, "third_party/douyinLive/LICENSE")), true);
});
