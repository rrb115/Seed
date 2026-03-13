import { addAudit, del, getAll, put } from "./storage.js";

export class OperationQueue {
  async enqueue(op, { status = "queued", lifecycle = "Pending Sync" } = {}) {
    await put("ops", {
      op_id: op.op_id,
      object_id: op.object_id,
      status,
      lifecycle,
      retry_count: 0,
      created_at: new Date().toISOString(),
      op,
    });
    await addAudit("op.enqueued", {
      op_id: op.op_id,
      type: op.type,
      object_id: op.object_id,
      status,
      lifecycle,
    });
  }

  async listByStatus(statuses) {
    const all = await getAll("ops");
    return all.filter((record) => statuses.includes(record.status));
  }

  async markStatus(records, status) {
    for (const record of records) {
      record.status = status;
      if (status === "queued" && record.retry_count != null) {
        record.retry_count += 1;
      }
      if (status === "queued") {
        record.lifecycle = "Pending Sync";
      } else if (status === "syncing") {
        record.lifecycle = "Pending Sync";
      } else if (status === "rejected") {
        record.lifecycle = "Rejected";
      }
      await put("ops", record);
    }
  }

  async listIntents() {
    return this.listByStatus(["intent"]);
  }

  async promoteIntents() {
    const intents = await this.listIntents();
    for (const record of intents) {
      record.status = "queued";
      record.lifecycle = "Pending Sync";
      await put("ops", record);
    }
    if (intents.length > 0) {
      await addAudit("op.intent_promoted", { count: intents.length });
    }
    return intents.length;
  }

  async removeByOpIDs(opIDs) {
    const set = new Set(opIDs);
    const all = await getAll("ops");
    for (const record of all) {
      if (set.has(record.op_id)) {
        await del("ops", record.op_id);
      }
    }
  }
}
