import { addAudit, put } from "./storage.js";

export class ManifestManager {
  constructor({ apiBase = "", token, swBridge }) {
    this.apiBase = apiBase;
    this.token = token;
    this.swBridge = swBridge;
  }

  async prepareWorkflowManifest(goal, steps, resourceClosure) {
    const qs = new URLSearchParams({ goal });
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
    const publicKey = response.headers.get("X-Manifest-Public-Key");

    if (!publicKey) {
      throw new Error("manifest response missing public key");
    }

    const swResult = await this.swBridge.importManifest(manifest, publicKey);
    if (!swResult?.ok) {
      throw new Error(swResult?.error || "service worker manifest import failed");
    }

    await put("manifests", {
      manifest_id: manifest.manifest_id,
      goal: manifest.goal,
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
