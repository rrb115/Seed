import { addAudit } from "./storage.js";

export class ServiceWorkerBridge {
  async register(scriptURL = "/sw.js") {
    if (!("serviceWorker" in navigator)) {
      await addAudit("sw.unsupported");
      return null;
    }

    const registration = await navigator.serviceWorker.register(scriptURL, { scope: "/" });
    await addAudit("sw.registered", { scriptURL });
    return registration;
  }

  async importManifest(manifest, publicKey) {
    const payload = { type: "IMPORT_MANIFEST", manifest, publicKey };
    return this.#post(payload);
  }

  async #post(message) {
    const target = navigator.serviceWorker.controller || (await navigator.serviceWorker.ready).active;
    if (!target) {
      throw new Error("no active service worker");
    }

    return new Promise((resolve, reject) => {
      const channel = new MessageChannel();
      channel.port1.onmessage = (event) => resolve(event.data);
      target.postMessage(message, [channel.port2]);
      setTimeout(() => reject(new Error("service worker did not respond")), 15000);
    });
  }
}
