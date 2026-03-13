const DB_NAME = "goal_cache_v1";
const DB_VERSION = 2;

let openPromise;

export function openDWCEDB() {
  if (!openPromise) {
    openPromise = new Promise((resolve, reject) => {
      const req = indexedDB.open(DB_NAME, DB_VERSION);
      req.onerror = () => reject(req.error);
      req.onupgradeneeded = () => {
        const db = req.result;
        ensureStore(db, "manifests", { keyPath: "manifest_id" });
        ensureStore(db, "resources_meta", { keyPath: "cid" });
        if (!db.objectStoreNames.contains("ops")) {
          const ops = db.createObjectStore("ops", { keyPath: "op_id" });
          ops.createIndex("status", "status", { unique: false });
        } else {
          const ops = req.transaction.objectStore("ops");
          if (!ops.indexNames.contains("status")) {
            ops.createIndex("status", "status", { unique: false });
          }
        }
        ensureStore(db, "objects", { keyPath: "object_id" });
        ensureStore(db, "audit", { keyPath: "id", autoIncrement: true });
      };
      req.onsuccess = () => resolve(req.result);
    });
  }
  return openPromise;
}

function ensureStore(db, name, opts) {
  if (!db.objectStoreNames.contains(name)) {
    db.createObjectStore(name, opts);
  }
}

export async function withStore(name, mode, fn) {
  const db = await openDWCEDB();
  return new Promise((resolve, reject) => {
    const tx = db.transaction(name, mode);
    const store = tx.objectStore(name);
    const request = fn(store);
    tx.oncomplete = () => resolve(request?.result);
    tx.onerror = () => reject(tx.error || request?.error);
  });
}

export function put(store, value) {
  return withStore(store, "readwrite", (s) => s.put(value));
}

export function get(store, key) {
  return withStore(store, "readonly", (s) => s.get(key));
}

export function getAll(store) {
  return withStore(store, "readonly", (s) => s.getAll());
}

export function del(store, key) {
  return withStore(store, "readwrite", (s) => s.delete(key));
}

export function getByIndex(storeName, indexName, value) {
  return withStore(storeName, "readonly", (store) => {
    const index = store.index(indexName);
    return index.getAll(value);
  });
}

export function addAudit(event, payload = {}) {
  return put("audit", {
    at: new Date().toISOString(),
    event,
    payload,
  });
}
