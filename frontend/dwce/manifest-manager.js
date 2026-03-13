import { addAudit, put } from "./storage.js";

export class ManifestManager {
  constructor({ apiBase = "", token, swBridge }) {
    this.apiBase = apiBase;
    this.token = token;
    this.swBridge = swBridge;
  }

  async prepareWorkflowManifest(goal, steps, resourceClosure) {
    const qs = new URLSearchParams({ goal, include_prepare: "1" });
    if (steps?.length) {
      qs.set("steps", steps.join(","));
    }
    if (resourceClosure?.length) {
      qs.set("resources", resourceClosure.join(","));
    }

    const response = await fetch(`${this.apiBase}/v1/manifest?${qs.toString()}`, {
      headers: { Authorization: `Bearer ${this.token}` },
    });
    if (!response.ok) {
      let detail = `manifest request failed: ${response.status}`;
      try {
        const body = await response.json();
        if (body?.error) {
          detail = body.error;
        }
      } catch (_) {
        // Keep default detail when server does not return JSON.
      }
      throw new Error(detail);
    }

    const manifest = await response.json();
    assertManifestShape(manifest);
    const swResult = await this.swBridge.importManifest(manifest);
    if (!swResult?.ok) {
      throw new Error(swResult?.error || "service worker manifest import failed");
    }

    await put("manifests", {
      manifest_id: manifest.manifest_id,
      goal: manifest.goal,
      version: manifest.version,
      created_at: manifest.created_at,
      expires_at: manifest.expires_at,
      payload: manifest,
    });

    for (const resource of manifest.resources || []) {
      await put("resources_meta", {
        cid: resource.cid,
        url: resource.url,
        integrity: resource.integrity,
        size: resource.size,
        ttl_seconds: resource.ttl_seconds,
        cached_at: new Date().toISOString(),
      });
    }

    await addAudit("manifest.prepared", {
      goal,
      manifest_id: manifest.manifest_id,
      resources: (manifest.resources || []).length,
    });

    return manifest;
  }
}

function assertManifestShape(manifest) {
  if (!manifest || typeof manifest !== "object") {
    throw new Error("manifest payload invalid");
  }
  if (!Array.isArray(manifest.resources)) {
    throw new Error("manifest resources missing");
  }
  // Compatibility: backend currently names workflow as goal.
  if (!manifest.workflow && !manifest.goal) {
    throw new Error("manifest workflow missing");
  }
  if (typeof manifest.version !== "number") {
    throw new Error("manifest version missing");
  }
  if (!manifest.expires_at || !manifest.key_id) {
    throw new Error("manifest expiry or key id missing");
  }
  if (!manifest.manifest_jws) {
    throw new Error("manifest_jws missing");
  }
}
