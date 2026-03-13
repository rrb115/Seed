import { DWCE } from "./dwce/index.js";

const API_TOKEN = "dev-token";

const logEl = document.getElementById("log");
const objectsViewEl = document.getElementById("objects-view");
const networkEl = document.getElementById("network-status");
const manifestStatusEl = document.getElementById("manifest-status");
const syncStatusEl = document.getElementById("sync-status");
const workflowSafetyEl = document.getElementById("workflow-safety");
const timelineEl = document.getElementById("timeline");
const manifestListEl = document.getElementById("manifest-list");
const manifestVizEl = document.getElementById("manifest-viz");
const workflowVizEl = document.getElementById("workflow-viz");

init().catch((err) => appendLog(`Init error: ${err.message}`));

async function init() {
  updateNetworkLabel();
  window.addEventListener("online", async () => {
    updateNetworkLabel();
    await runSync();
  });
  window.addEventListener("offline", updateNetworkLabel);

  await DWCE.init({ token: API_TOKEN, apiBase: "", serviceWorkerURL: "/sw.js" });

  DWCE.onEvent(async ({ event, payload, at }) => {
    appendLog(`${at} ${event} ${JSON.stringify(payload || {})}`);
    if (event === "sync.completed") {
      if ((payload?.conflicts || []).length > 0) {
        syncStatusEl.textContent = `Rejected (${payload.conflicts.length})`;
      } else if (payload?.status === "completed") {
        syncStatusEl.textContent = "Validated";
      }
      await renderSnapshot();
    }
    if (event === "workflow.prepared") {
      renderWorkflowViz(payload);
      workflowSafetyEl.textContent = `${payload.safety_class} (offline allowed)`;
    }
    if (event === "op.queued") {
      highlightActiveStep(payload);
      syncStatusEl.textContent = "Pending Sync";
    }
    if (event === "op.intent_stored") {
      syncStatusEl.textContent = "Draft (requires online execution)";
    }
    if (event === "workflow.offline_blocked") {
      workflowSafetyEl.textContent = `${payload.safety_class} (offline disabled)`;
    }
  });

  registerWorkflows();
  registerDependencies();

  document.getElementById("prepare-goal").addEventListener("click", prepareWorkflow);
  document.getElementById("queue-op").addEventListener("click", queueOperationFromForm);
  document.getElementById("sync-now").addEventListener("click", runSync);

  await renderSnapshot();
  appendLog("Ready.");
}

function registerWorkflows() {
  DWCE.registerWorkflow("note_draft", {
    steps: ["open_editor", "edit_content", "save_draft"],
    safety_class: "SAFE",
  });

  DWCE.registerWorkflow("support_ticket", {
    steps: ["fill_form", "attach_context", "submit_ticket"],
    requires_validation: true,
  });

  DWCE.registerWorkflow("checkout", {
    steps: ["review_cart", "enter_address", "payment", "confirmation"],
    requires_global_state: true,
  });
}

function registerDependencies() {
  DWCE.registerStepDependencies("note_draft", {
    open_editor: ["/index.html", "/styles.css", "/app.js", "/sw.js"],
    edit_content: ["/index.html", "/styles.css", "/app.js"],
    save_draft: ["/index.html", "/styles.css", "/app.js", "/offline.html"],
  });

  DWCE.registerStepDependencies("support_ticket", {
    fill_form: ["/index.html", "/styles.css", "/app.js", "/sw.js"],
    attach_context: ["/index.html", "/styles.css", "/app.js"],
    submit_ticket: ["/index.html", "/styles.css", "/app.js", "/offline.html"],
  });

  DWCE.registerStepDependencies("checkout", {
    review_cart: ["/index.html", "/styles.css", "/app.js", "/sw.js"],
    enter_address: ["/index.html", "/styles.css", "/app.js"],
    payment: ["/index.html", "/styles.css", "/app.js"],
    confirmation: ["/index.html", "/styles.css", "/app.js", "/offline.html"],
  });

  DWCE.registerResourceDependencies({
    "/app.js": ["/dwce/index.js"],
    "/dwce/index.js": [
      "/dwce/workflow-engine.js",
      "/dwce/dependency-graph.js",
      "/dwce/manifest-manager.js",
      "/dwce/offline-state.js",
      "/dwce/op-queue.js",
      "/dwce/sync-agent.js",
      "/dwce/service-worker-bridge.js",
      "/dwce/storage.js",
    ],
  });
}

async function prepareWorkflow() {
  const goal = document.getElementById("goal").value;
  manifestStatusEl.textContent = `Preparing ${goal}...`;

  try {
    const prepared = await DWCE.prepareWorkflow(goal);
    if (!prepared.offline_allowed) {
      manifestStatusEl.textContent = `Offline disabled for ${goal}. Intents can be stored and executed online.`;
      manifestVizEl.style.display = "none";
      return;
    }
    manifestStatusEl.textContent = `Manifest prepared (${prepared.closure.length} resources).`;
    renderManifestViz(prepared.manifest.resources);
  } catch (err) {
    manifestStatusEl.textContent = `Error: ${err.message}`;
    appendLog(`Prepare error: ${err.message}`);
  }
}

async function queueOperationFromForm() {
  const objectID = document.getElementById("object-id").value.trim();
  const type = document.getElementById("op-type").value;
  const sku = document.getElementById("sku").value.trim();
  const qty = Number(document.getElementById("qty").value || 0);
  const path = document.getElementById("field-path").value.trim();
  const value = document.getElementById("field-value").value;
  const workflow = document.getElementById("goal").value;

  const op = { object_id: objectID, type, workflow };
  if (type === "set_field") {
    op.path = path ? path.split(".") : [];
    op.value = value;
  } else if (type === "remove_item") {
    op.value = sku;
  } else {
    op.value = { sku, qty };
  }

  await DWCE.queueOperation(op);
  await renderSnapshot();
}

async function runSync() {
  syncStatusEl.textContent = "Syncing...";
  try {
    const result = await DWCE.sync();
    if (result.status === "idle") {
      syncStatusEl.textContent = "Idle.";
      return;
    }

    if (result.conflicts.length > 0) {
      syncStatusEl.textContent = `Rejected (${result.conflicts.length})`;
    } else {
      syncStatusEl.textContent = `Validated (${result.acked})`;
    }
  } catch (err) {
    syncStatusEl.textContent = `Sync failed: ${err.message}`;
  }

  await renderSnapshot();
}

async function renderSnapshot() {
  const snapshot = await DWCE.snapshot();
  objectsViewEl.textContent = JSON.stringify(snapshot, null, 2);
}

function updateNetworkLabel() {
  const isOnline = navigator.onLine;
  networkEl.textContent = isOnline ? "Online" : "Offline";
  networkEl.className = isOnline ? "status-online" : "status-offline";
}

function appendLog(message) {
  const entry = document.createElement("div");
  entry.style.padding = "0.25rem 0";
  entry.style.borderBottom = "1px solid rgba(255,255,255,0.05)";

  // Extract timestamp if present
  const parts = message.split(" ");
  if (parts[0].includes("T") && parts[0].includes("Z")) {
    const time = new Date(parts[0]).toLocaleTimeString();
    const rest = parts.slice(1).join(" ");
    entry.innerHTML = `<span style="color:var(--accent-primary);font-weight:600;margin-right:0.5rem;">${time}</span> <span>${rest}</span>`;
  } else {
    entry.textContent = message;
  }

  logEl.prepend(entry);
}

function renderWorkflowViz(payload) {
  workflowVizEl.style.display = "block";
  timelineEl.innerHTML = "";

  payload.steps.forEach((step, idx) => {
    const card = document.createElement("div");
    card.className = "step-card";
    card.id = `step-${step}`;
    card.innerHTML = `
      <div class="step-name">${step.replace(/_/g, " ")}</div>
      <div class="step-status">Pending</div>
    `;
    timelineEl.appendChild(card);
  });
}

function renderManifestViz(resources) {
  manifestVizEl.style.display = "block";
  document.getElementById("manifest-label").textContent = `Resources (${resources.length})`;
  manifestListEl.innerHTML = "";

  resources.forEach(res => {
    const item = document.createElement("div");
    item.className = "resource-item";
    const ext = res.url.split(".").pop();
    const icon = getFileIcon(ext);

    item.innerHTML = `
      <div class="resource-icon">${icon}</div>
      <div class="resource-info">
        <div class="resource-name">${res.url}</div>
        <div class="resource-meta">${(res.size / 1024).toFixed(1)} KB • ${res.cid.substring(0, 15)}...</div>
      </div>
    `;
    manifestListEl.appendChild(item);
  });
}

function getFileIcon(ext) {
  if (ext === "js") return '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><path d="M9 15l2 2 4-4"/></svg>';
  if (ext === "html") return '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>';
  if (ext === "css") return '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>';
  return '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><polyline points="13 2 13 9 20 9"/></svg>';
}

function highlightActiveStep(payload) {
  // Logic to guess which step is "active" based on op type or simple progression
  // In a real app, the workflow engine would track this.
  // For the demo, we can just highlight based on objects modified.
  const allSteps = Array.from(timelineEl.querySelectorAll(".step-card"));
  if (allSteps.length === 0) return;

  // Highlight the first pending step as active for demo purposes
  const firstPending = allSteps.find(s => !s.classList.contains("completed"));
  if (firstPending) {
    allSteps.forEach(s => s.classList.remove("active"));
    firstPending.classList.add("active");
    firstPending.querySelector(".step-status").textContent = "Active";
  }
}
