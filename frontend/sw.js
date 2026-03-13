const APP_SHELL_CACHE = "app-shell-v1";
const GOAL_CACHE = "goal-cache-v1";
const APP_SHELL = [
  "/",
  "/index.html",
  "/styles.css",
  "/app.js",
  "/offline.html",
  "/dwce/index.js",
  "/dwce/workflow-engine.js",
  "/dwce/dependency-graph.js",
  "/dwce/manifest-manager.js",
  "/dwce/offline-state.js",
  "/dwce/op-queue.js",
  "/dwce/sync-agent.js",
  "/dwce/service-worker-bridge.js",
  "/dwce/storage.js",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(APP_SHELL_CACHE);
      await cache.addAll(APP_SHELL);
      await self.skipWaiting();
    })(),
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("message", (event) => {
  if (!event.data || event.data.type !== "IMPORT_MANIFEST") {
    return;
  }

  event.waitUntil(
    (async () => {
      try {
        const { manifest, publicKey } = event.data;
        await importManifest(manifest, publicKey);
        reply(event, { ok: true, manifest_id: manifest.manifest_id });
        broadcast({ type: "MANIFEST_IMPORTED", manifest_id: manifest.manifest_id, goal: manifest.goal });
      } catch (err) {
        reply(event, { ok: false, error: err.message || "manifest import failed" });
      }
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  if (event.request.method !== "GET") {
    return;
  }

  const url = new URL(event.request.url);
  if (url.origin !== self.location.origin) {
    return;
  }

  if (url.pathname.startsWith("/v1/")) {
    event.respondWith(networkFirst(event.request));
    return;
  }

  if (event.request.mode === "navigate") {
    event.respondWith(navigationStrategy(event.request));
    return;
  }

  event.respondWith(cacheFirst(event.request));
});

self.addEventListener("sync", (event) => {
  if (event.tag !== "flush-ops") {
    return;
  }
  event.waitUntil(broadcast({ type: "SYNC_REQUESTED" }));
});

async function importManifest(manifest, publicKeyB64) {
  if (manifest.offline_eligible === false || manifest.safety_class === "UNSAFE") {
    throw new Error("workflow is not eligible for offline execution");
  }

  const verified = await verifyManifestSignature(manifest, publicKeyB64);
  if (!verified) {
    throw new Error("manifest signature verification failed");
  }

  const cache = await caches.open(GOAL_CACHE);
  for (const resource of manifest.resources) {
    const response = await fetch(resource.url, { cache: "reload" });
    if (!response.ok) {
      throw new Error(`failed to fetch resource ${resource.url}`);
    }

    const body = await response.clone().arrayBuffer();
    if (resource.integrity) {
      const valid = await verifyIntegrity(body, resource.integrity);
      if (!valid) {
        throw new Error(`integrity mismatch for ${resource.url}`);
      }
    }

    await cache.put(resource.url, response);
  }

  await cache.put(
    `/__manifest_meta__/${manifest.manifest_id}.json`,
    new Response(JSON.stringify({ manifest_id: manifest.manifest_id, goal: manifest.goal, imported_at: new Date().toISOString() }), {
      headers: { "Content-Type": "application/json" },
    }),
  );
}

async function verifyManifestSignature(manifest, publicKeyB64) {
  if (!self.crypto || !self.crypto.subtle) {
    return false;
  }

  let key;
  try {
    key = await self.crypto.subtle.importKey("raw", base64ToBytes(publicKeyB64), { name: "Ed25519" }, false, ["verify"]);
  } catch (_) {
    return false;
  }

  const payload = {
    manifest_id: manifest.manifest_id,
    goal: manifest.goal,
    objectives: manifest.objectives,
    resources: (manifest.resources || []).map((r) => ({
      url: r.url,
      cid: r.cid,
      size: r.size,
      integrity: r.integrity,
      ttl_seconds: r.ttl_seconds,
    })),
    safety_class: manifest.safety_class,
    offline_eligible: manifest.offline_eligible,
    validation_required: manifest.validation_required,
    version: manifest.version,
    key_id: manifest.key_id,
    audience: manifest.audience,
    created_at: manifest.created_at,
    expires_at: manifest.expires_at,
  };

  const payloadBytes = new TextEncoder().encode(JSON.stringify(payload));
  const signature = base64ToBytes(manifest.edge_signature);

  try {
    return await self.crypto.subtle.verify({ name: "Ed25519" }, key, signature, payloadBytes);
  } catch (_) {
    return false;
  }
}

async function verifyIntegrity(body, integrity) {
  const [algo, expectedB64] = integrity.split("-");
  if (algo !== "sha256" || !expectedB64) {
    return false;
  }
  const digest = await self.crypto.subtle.digest("SHA-256", body);
  const actual = bytesToBase64(new Uint8Array(digest));
  return actual === expectedB64;
}

async function navigationStrategy(request) {
  try {
    const cached = await caches.match(request);
    if (cached) {
      return cached;
    }

    const network = await fetch(request);
    return network;
  } catch (_) {
    const fallback = await caches.match("/offline.html");
    if (fallback) {
      return fallback;
    }
    return new Response("Offline", { status: 503 });
  }
}

async function cacheFirst(request) {
  const cacheMatch = await caches.match(request);
  if (cacheMatch) {
    return cacheMatch;
  }

  try {
    return await fetch(request);
  } catch (_) {
    return new Response("Offline", { status: 503 });
  }
}

async function networkFirst(request) {
  try {
    return await fetch(request);
  } catch (_) {
    const cached = await caches.match(request);
    if (cached) {
      return cached;
    }
    return new Response(JSON.stringify({ error: "offline" }), {
      status: 503,
      headers: { "Content-Type": "application/json" },
    });
  }
}

function reply(event, payload) {
  if (event.ports && event.ports[0]) {
    event.ports[0].postMessage(payload);
    return;
  }
  if (event.source) {
    event.source.postMessage(payload);
  }
}

async function broadcast(payload) {
  const clients = await self.clients.matchAll({ includeUncontrolled: true });
  for (const client of clients) {
    client.postMessage(payload);
  }
}

function base64ToBytes(b64) {
  const raw = atob(b64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) {
    out[i] = raw.charCodeAt(i);
  }
  return out;
}

function bytesToBase64(bytes) {
  let out = "";
  for (let i = 0; i < bytes.length; i += 1) {
    out += String.fromCharCode(bytes[i]);
  }
  return btoa(out);
}
