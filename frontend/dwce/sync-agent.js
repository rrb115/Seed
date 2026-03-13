import { addAudit } from "./storage.js";

export class SyncAgent {
  constructor({ apiBase = "", token, opQueue, offlineState }) {
    this.apiBase = apiBase;
    this.token = token;
    this.opQueue = opQueue;
    this.offlineState = offlineState;
  }

  async flush(clientID) {
    if (navigator.onLine) {
      await this.opQueue.promoteIntents();
    }

    const queued = await this.opQueue.listByStatus(["queued"]);
    if (queued.length === 0) {
      return { status: "idle", acked: 0, conflicts: [] };
    }

    await this.opQueue.markStatus(queued, "syncing");

    const traceID = crypto.randomUUID();
    const payload = {
      client_tx_id: crypto.randomUUID(),
      client_id: clientID,
      ops: queued.map((q) => q.op),
    };

    let response;
    try {
      response = await fetch(`${this.apiBase}/v1/sync`, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${this.token}`,
          "Content-Type": "application/json",
          "X-Trace-Id": traceID,
        },
        body: JSON.stringify(payload),
      });
    } catch (err) {
      await this.opQueue.markStatus(queued, "queued");
      throw err;
    }

    if (response.status === 202) {
      const body = await response.json();
      const completed = await this.pollQueueStatus(body.queue_id);
      return this.consume(completed, queued);
    }

    if (!response.ok) {
      const txt = await response.text();
      await this.opQueue.markStatus(queued, "queued");
      throw new Error(`sync failed (${response.status}): ${txt}`);
    }

    const body = await response.json();
    return this.consume(body, queued);
  }

  async consume(syncResponse, syncingRecords = []) {
    const acked = syncResponse.acked_op_ids || [];
    const conflicts = syncResponse.conflicts || [];
    const conflictedIDs = new Set(conflicts.map((c) => c.op_id));

    for (const rec of syncingRecords) {
      if (conflictedIDs.has(rec.op_id)) {
        rec.lifecycle = "Rejected";
      } else if (acked.includes(rec.op_id)) {
        rec.lifecycle = "Validated";
      }
    }

    await this.opQueue.removeByOpIDs(acked);

    const unackedSyncing = syncingRecords.filter((rec) => !acked.includes(rec.op_id) && !conflictedIDs.has(rec.op_id));
    if (unackedSyncing.length > 0) {
      await this.opQueue.markStatus(unackedSyncing, "queued");
    }

    const rejected = syncingRecords.filter((rec) => conflictedIDs.has(rec.op_id));
    for (const rec of rejected) {
      rec.status = "rejected";
      rec.retry_count = rec.retry_count ?? 0;
      await this.opQueue.markStatus([rec], "rejected");
    }

    await this.offlineState.applyServerResults(syncResponse.results || []);

    await addAudit("sync.completed", {
      acked: acked.length,
      conflicts: conflicts.length,
      tx_id: syncResponse.tx_id,
    });

    return {
      status: "completed",
      acked: acked.length,
      conflicts,
      response: syncResponse,
    };
  }

  async pollQueueStatus(queueID) {
    for (let attempt = 0; attempt < 20; attempt += 1) {
      await sleep(750);
      const response = await fetch(`${this.apiBase}/v1/sync/status?queue_id=${encodeURIComponent(queueID)}`, {
        headers: { Authorization: `Bearer ${this.token}` },
      });
      if (!response.ok) {
        continue;
      }
      const status = await response.json();
      if (status.status === "completed" && status.response) {
        return status.response;
      }
    }
    throw new Error(`sync queue ${queueID} did not complete in time`);
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
