// Pure observation helpers. Unit tests use a virtual clock and synthetic snapshots.
export async function observeFor({ durationMs, intervalMs = 5000, read, sample, now = Date.now, sleep = ms => new Promise(resolve => setTimeout(resolve, ms)) }) {
  const end = now() + durationMs;
  while (true) {
    try {
      await sample({ at: now(), snapshot: await read() });
    } catch {
      await sample({ at: now(), error: "节点快照读取失败" });
    }
    const remaining = end - now();
    if (remaining <= 0) return;
    await sleep(Math.min(intervalMs, remaining));
  }
}

export function beginObservation(snapshot, at) {
  return {
    startedAt: new Date(at).toISOString(),
    bootId: snapshot.node.bootId,
    cohort: snapshot.rooms.filter(room => room.status === "collecting").map(room => room.liveId),
    startupUnconnected: snapshot.rooms.filter(room => room.status !== "collecting").map(room => ({ id: room.liveId, state: room.status, reason: room.reason })),
    rooms: Object.fromEntries(snapshot.rooms.map(room => [room.liveId, {
      baseline: room.diagnostics ?? null,
      disconnects: null,
      reconnectDurationsMs: [],
      silenceFalsePositives: null,
      requestsPerMinute: [],
      recoveryCursor: room.diagnostics?.reconnectRecoveryCount ?? 0,
      state: room.status,
      reason: room.reason,
    }])),
    samples: [],
    errors: [],
  };
}

export function recordObservation(report, sample) {
  const at = new Date(sample.at).toISOString();
  if (sample.error) {
    report.errors.push({ at, error: sample.error });
    return;
  }
  const snapshot = sample.snapshot;
  if (snapshot.node.bootId !== report.bootId) {
    report.errors.push({ at, error: "观察期间节点重启，累计诊断不能跨启动比较" });
    return;
  }
  report.samples.push({
    at,
    collecting: snapshot.rooms.filter(room => room.status === "collecting").map(room => room.liveId),
    requestsPerMinute: snapshot.node.reqPerMin,
    homepageRequestsPerMinute: snapshot.node.homepageRequestsPerMinute,
    cooldown: snapshot.node.cooldown,
  });
  for (const room of snapshot.rooms) {
    const target = report.rooms[room.liveId];
    if (!target) continue;
    target.state = room.status;
    target.reason = room.reason;
    target.disconnectedAt = room.diagnostics?.disconnectedAt ?? null;
    const diagnostic = room.diagnostics;
    if (!diagnostic || !target.baseline) {
      target.metricsUnavailable = true;
      continue;
    }
    target.disconnects = diagnostic.disconnects - target.baseline.disconnects;
    target.silenceFalsePositives = diagnostic.silenceFalsePositives - target.baseline.silenceFalsePositives;
    target.requestsPerMinute.push({ at, rolling60Seconds: diagnostic.requestsPerMinute, totalSinceObservation: diagnostic.requestsTotal - target.baseline.requestsTotal });
    const recovered = diagnostic.reconnectRecoveryCount - target.recoveryCursor;
    const durations = diagnostic.reconnectDurationsMs ?? [];
    if (recovered > durations.length || recovered < 0) target.recoveryHistoryIncomplete = true;
    if (recovered > 0) target.reconnectDurationsMs.push(...durations.slice(-recovered));
    target.recoveryCursor = diagnostic.reconnectRecoveryCount;
  }
}

export function finishObservation(report, at) {
  report.finishedAt = new Date(at).toISOString();
  report.durationSeconds = (at - Date.parse(report.startedAt)) / 1000;
  report.minimumCollecting = report.samples.length ? Math.min(...report.samples.map(sample => sample.collecting.length)) : 0;
  report.stabilityPassed = report.errors.length === 0 && report.cohort.length >= 11 && report.samples.length > 0 && report.samples.every(sample => report.cohort.every(id => sample.collecting.includes(id))) && Object.values(report.rooms).every(room => !room.metricsUnavailable && !room.recoveryHistoryIncomplete);
  return report;
}
