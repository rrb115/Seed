import { addAudit, get, getAll, put } from "./storage.js";

export class OfflineStateEngine {
  async applyOperation(op) {
    const record = (await get("objects", op.object_id)) || {
      object_id: op.object_id,
      state: {},
      version_vector: {},
    };

    switch (op.type) {
      case "set_field":
        if (Array.isArray(op.path) && op.path.length > 0) {
          setNested(record.state, op.path, op.value);
        }
        break;
      case "add_item":
        record.state.items = record.state.items || {};
        record.state.items[op.value.sku] = (record.state.items[op.value.sku] || 0) + op.value.qty;
        break;
      case "set_quantity":
        record.state.items = record.state.items || {};
        record.state.items[op.value.sku] = op.value.qty;
        break;
      case "remove_item":
        record.state.items = record.state.items || {};
        delete record.state.items[op.value];
        break;
      default:
        break;
    }

    record.version_vector[op.client_id] = Math.max(record.version_vector[op.client_id] || 0, op.clock);
    record.updated_at = new Date().toISOString();
    await put("objects", record);

    await addAudit("state.local_applied", {
      op_id: op.op_id,
      object_id: op.object_id,
      type: op.type,
    });

    return record;
  }

  async applyServerResults(results) {
    for (const result of results || []) {
      await put("objects", {
        object_id: result.object_id,
        state: result.state,
        version_vector: result.version_vector,
        synced_at: new Date().toISOString(),
      });
    }
  }

  async snapshot() {
    const all = await getAll("objects");
    const view = {};
    for (const item of all) {
      view[item.object_id] = {
        state: item.state,
        version_vector: item.version_vector,
      };
    }
    return view;
  }
}

function setNested(root, path, value) {
  let cursor = root;
  for (let i = 0; i < path.length - 1; i += 1) {
    const key = path[i];
    if (!cursor[key] || typeof cursor[key] !== "object") {
      cursor[key] = {};
    }
    cursor = cursor[key];
  }
  cursor[path[path.length - 1]] = value;
}
