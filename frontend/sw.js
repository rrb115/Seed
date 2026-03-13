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
        const { manifest } = event.data;
        await importManifest(manifest);
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
  if (event.tag !== "flush-ops" && event.tag !== "dwce-sync") {
    return;
  }
  event.waitUntil(broadcast({ type: "SYNC_REQUESTED" }));
});

async function importManifest(manifest) {
  if (manifest.offline_eligible === false || manifest.safety_class === "UNSAFE") {
    throw new Error("workflow is not eligible for offline execution");
  }
  if (!manifest.manifest_jws) {
    throw new Error("manifest_jws missing");
  }

  const expiresAt = Date.parse(manifest.expires_at || "");
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) {
    throw new Error("manifest expired");
  }

  await assertMonotonicVersion(manifest);

  const keys = await fetchJWKS();
  const verified = await verifyManifestJWS(manifest, keys);
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
    new Response(JSON.stringify({
      manifest_id: manifest.manifest_id,
      goal: manifest.goal,
      version: manifest.version,
      imported_at: new Date().toISOString(),
    }), {
      headers: { "Content-Type": "application/json" },
    }),
  );

  await cache.put(
    `/__manifest_goal__/${manifest.goal}.json`,
    new Response(JSON.stringify({ goal: manifest.goal, version: manifest.version }), {
      headers: { "Content-Type": "application/json" },
    }),
  );
}

async function assertMonotonicVersion(manifest) {
  const cache = await caches.open(GOAL_CACHE);
  const previous = await cache.match(`/__manifest_goal__/${manifest.goal}.json`);
  if (!previous) {
    return;
  }
  try {
    const prev = await previous.json();
    if (typeof prev.version === "number" && typeof manifest.version === "number" && manifest.version < prev.version) {
      throw new Error("manifest version rollback detected");
    }
  } catch (err) {
    if (err instanceof Error) {
      throw err;
    }
  }
}

async function fetchJWKS() {
  const response = await fetch("/.well-known/dwce-keys", { cache: "no-store" });
  if (!response.ok) {
    throw new Error("failed to fetch jwks");
  }
  const body = await response.json();
  if (!Array.isArray(body.keys) || body.keys.length === 0) {
    throw new Error("jwks response missing keys");
  }
  return body.keys;
}

async function verifyManifestJWS(manifest, keys) {
  if (!self.crypto || !self.crypto.subtle) {
    return false;
  }

  const parts = manifest.manifest_jws.split(".");
  if (parts.length !== 3) {
    return false;
  }

  let header;
  try {
    header = JSON.parse(new TextDecoder().decode(base64URLToBytes(parts[0])));
  } catch (_) {
    return false;
  }
  if (header.alg !== "EdDSA" || !header.kid) {
    return false;
  }

  const key = keys.find((item) => item.kid === header.kid);
  if (!key) {
    return false;
  }

  let cryptoKey;
  try {
    cryptoKey = await self.crypto.subtle.importKey("raw", base64URLToBytes(key.x), { name: "Ed25519" }, false, ["verify"]);
  } catch (_) {
    return false;
  }

  const signingInput = new TextEncoder().encode(`${parts[0]}.${parts[1]}`);
  const signature = base64URLToBytes(parts[2]);
  const signatureValid = await self.crypto.subtle.verify({ name: "Ed25519" }, cryptoKey, signature, signingInput);
  if (!signatureValid) {
    return false;
  }

  const canonical = canonicalManifestPayload(manifest);
  const expectedPayload = new TextEncoder().encode(canonical);
  const actualPayload = base64URLToBytes(parts[1]);
  if (!bytesEqual(expectedPayload, actualPayload)) {
    return false;
  }

  return true;
}

function canonicalManifestPayload(manifest) {
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
  if (manifest.prepare_required) {
    payload.prepare_required = true;
  }
  if (manifest.prepare_token) {
    payload.prepare_token = manifest.prepare_token;
  }
  return JSON.stringify(payload);
}

function bytesEqual(a, b) {
  if (a.length !== b.length) {
    return false;
  }
  for (let i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) {
      return false;
    }
  }
  return true;
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

    return await fetch(request);
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

function base64URLToBytes(b64url) {
  const normalized = b64url.replace(/-/g, "+").replace(/_/g, "/");
  const pad = normalized.length % 4 === 0 ? "" : "=".repeat(4 - (normalized.length % 4));
  const raw = atob(normalized + pad);
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
