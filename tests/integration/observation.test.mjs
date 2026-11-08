import test from "node:test";
import assert from "node:assert/strict";
import { observeFor, beginObservation, recordObservation, finishObservation } from "./observation.mjs";

function snapshot(states) {
  return {
    node: { bootId: "test", reqPerMin: 5, homepageRequestsPerMinute: 1, cooldown: { active: true, remainingSeconds: 120 } },
    rooms: states.map((status, index) => ({
      liveId: String(index), status, reason: status === "risk" ? "上游受限" : "已连接",
      diagnostics: { disconnects: 0, reconnectRecoveryCount: 0, reconnectDurationsMs: [], silenceFalsePositives: 0, requestsPerMinute: 2, requestsTotal: 10, disconnectedAt: 0 },
    })),
  };
}

for (const states of [["risk", "risk"], ["collecting", "risk"]]) {
  test(`启动仅 ${states.filter(s => s === "collecting").length} 间连通也观察满时长`, async () => {
    let now = 0;
    const initial = snapshot(states);
    const report = beginObservation(initial, now);
    await observeFor({ durationMs: 600000, intervalMs: 30000, now: () => now, sleep: async ms => { now += ms; }, read: async () => initial, sample: sample => recordObservation(report, sample) });
    finishObservation(report, now);
    assert.equal(report.durationSeconds, 600);
    assert.equal(report.samples.length, 21);
    assert.equal(report.startupUnconnected.at(-1).reason, "上游受限");
    assert.equal(report.stabilityPassed, false);
  });
}

test("重连计数按累计诊断取差值，重复快照不会重复记耗时", () => {
  const first = snapshot(["collecting"]);
  const report = beginObservation(first, 0);
  const recovered = snapshot(["collecting"]);
  Object.assign(recovered.rooms[0].diagnostics, { disconnects: 2, reconnectRecoveryCount: 2, reconnectDurationsMs: [2000, 5000], silenceFalsePositives: 1, requestsTotal: 15 });
  recordObservation(report, { at: 5000, snapshot: recovered });
  recordObservation(report, { at: 10000, snapshot: recovered });
  assert.deepEqual(report.rooms["0"].reconnectDurationsMs, [2000, 5000]);
  assert.equal(report.rooms["0"].disconnects, 2);
  assert.equal(report.rooms["0"].silenceFalsePositives, 1);
  assert.equal(report.rooms["0"].requestsPerMinute[1].totalSinceObservation, 5);
});

test("快照临时失败不缩短观察；节点重启不能算连续通过", async () => {
  let now = 0;
  const report = beginObservation(snapshot(["collecting"]), now);
  await observeFor({ durationMs: 10000, now: () => now, sleep: async ms => { now += ms; }, read: async () => { throw Error("synthetic"); }, sample: sample => recordObservation(report, sample) });
  assert.equal(now, 10000);
  assert.equal(report.errors.length, 3);
  const restarted = snapshot(["collecting"]);
  restarted.node.bootId = "another-boot";
  recordObservation(report, { at: now, snapshot: restarted });
  assert.equal(finishObservation(report, now).stabilityPassed, false);
});
