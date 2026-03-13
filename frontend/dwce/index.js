import { DependencyGraphEngine } from "./dependency-graph.js";
import { ManifestManager } from "./manifest-manager.js";
import { OfflineStateEngine } from "./offline-state.js";
import { OperationQueue } from "./op-queue.js";
import { ServiceWorkerBridge } from "./service-worker-bridge.js";
import { SyncAgent } from "./sync-agent.js";
import { addAudit, openDWCEDB } from "./storage.js";
import { WorkflowEngine } from "./workflow-engine.js";

class DWCERuntime {
  constructor() {
    this.workflowEngine = new WorkflowEngine();
    this.dependencyGraph = new DependencyGraphEngine();
    this.opQueue = new OperationQueue();
    this.offlineState = new OfflineStateEngine();
    this.swBridge = new ServiceWorkerBridge();

    this.token = "dev-token";
    this.apiBase = "";
    this.clientID = null;
    this.listeners = new Set();

    this.manifestManager = null;
    this.syncAgent = null;
    this.lastPreparedWorkflow = null;
    this.workflowPrepareTokens = new Map();
  }

  async init(options = {}) {
    this.token = options.token || this.token;
    this.apiBase = options.apiBase || this.apiBase;
    this.clientID = options.clientID || this.#getOrCreateClientID();

    await openDWCEDB();
    await this.swBridge.register(options.serviceWorkerURL || "/sw.js");

    this.manifestManager = new ManifestManager({
      apiBase: this.apiBase,
      token: this.token,
      swBridge: this.swBridge,
    });

    this.syncAgent = new SyncAgent({
      apiBase: this.apiBase,
      token: this.token,
      opQueue: this.opQueue,
      offlineState: this.offlineState,
    });

    navigator.serviceWorker?.addEventListener("message", (event) => {
      if (!event.data) {
        return;
      }
      if (event.data.type === "SYNC_REQUESTED") {
        this.sync().catch((err) => this.#emit("sync.error", { message: err.message }));
      }
      this.#emit("sw.event", event.data);
    });

    if ("storage" in navigator && navigator.storage?.persist) {
      const granted = await navigator.storage.persist();
      await addAudit("storage.persist", { granted });
    }

    await addAudit("dwce.init", { client_id: this.clientID });
    return this;
  }

  onEvent(handler) {
    this.listeners.add(handler);
    return () => this.listeners.delete(handler);
  }

  registerWorkflow(name, definition) {
    const wf = this.workflowEngine.registerWorkflow(name, definition);
    this.#emit("workflow.registered", {
      name,
      steps: wf.steps,
      safety_class: this.workflowEngine.evaluateSafety(name),
    });
    return wf;
  }

  registerStepDependencies(workflowName, stepMap) {
    this.dependencyGraph.registerWorkflowStepMap(workflowName, stepMap);
    this.#emit("deps.step_map_registered", { workflowName, steps: Object.keys(stepMap || {}) });
  }

  registerResourceDependencies(resourceMap) {
    this.dependencyGraph.registerResourceDependencyMap(resourceMap);
    this.#emit("deps.resource_map_registered", { resources: Object.keys(resourceMap || {}) });
  }

  async prepareWorkflow(goal, options = {}) {
    this.#ensureReady();
    const safetyClass = this.workflowEngine.evaluateSafety(goal);
    const offlineAllowed = this.workflowEngine.isOfflineExecutable(goal);

    if (!offlineAllowed) {
      this.lastPreparedWorkflow = goal;
      const blocked = {
        goal,
        safety_class: safetyClass,
        offline_allowed: false,
        reason: "requires_global_state",
      };
      this.#emit("workflow.offline_blocked", blocked);
      return blocked;
    }

    const steps = this.workflowEngine.reachableSteps(goal, options.startStep);
    const closure = this.dependencyGraph.computeMinimalClosure(goal, steps);

    const manifest = await this.manifestManager.prepareWorkflowManifest(goal, steps, closure);
    if (manifest.prepare_token) {
      this.workflowPrepareTokens.set(goal, manifest.prepare_token);
    }
    this.lastPreparedWorkflow = goal;
    this.#emit("workflow.prepared", {
      goal,
      steps,
      closure_size: closure.length,
      safety_class: safetyClass,
      manifest_id: manifest.manifest_id,
    });

    return { goal, steps, closure, manifest, safety_class: safetyClass, offline_allowed: true };
  }

  async queueOperation(opInput) {
    this.#ensureReady();
    const workflow = opInput?.workflow || this.lastPreparedWorkflow;
    if (!workflow) {
      throw new Error("workflow is required (pass op.workflow or call prepareWorkflow first)");
    }

    const safetyClass = this.workflowEngine.evaluateSafety(workflow);
    const offline = !navigator.onLine;
    const prepareToken = opInput?.prepare_token || this.workflowPrepareTokens.get(workflow);
    const op = this.#normalizeOp({ ...opInput, workflow, prepare_token: prepareToken });

    if (safetyClass === "UNSAFE" && offline) {
      await this.opQueue.enqueue(op, { status: "intent", lifecycle: "Draft" });
      await this.offlineState.applyOperation(op);
      this.#emit("op.intent_stored", {
        op_id: op.op_id,
        workflow,
        object_id: op.object_id,
        state: "Draft",
      });
      return op;
    }

    const lifecycle = safetyClass === "EVENTUAL" ? "Pending Sync" : "Pending Sync";
    await this.opQueue.enqueue(op, { status: "queued", lifecycle });
    await this.offlineState.applyOperation(op);
    this.#emit("op.queued", {
      op_id: op.op_id,
      type: op.type,
      object_id: op.object_id,
      workflow,
      safety_class: safetyClass,
      state: "Pending Sync",
    });
    return op;
  }

  async sync() {
    this.#ensureReady();
    const result = await this.syncAgent.flush(this.clientID);
    this.#emit("sync.completed", result);
    return result;
  }

  async snapshot() {
    return this.offlineState.snapshot();
  }

  #normalizeOp(opInput) {
    if (!opInput?.object_id) {
      throw new Error("object_id is required");
    }
    if (!opInput?.type) {
      throw new Error("type is required");
    }

    const clock = Date.now();
    return {
      op_id: opInput.op_id || `${this.clientID}:${clock}:${Math.floor(Math.random() * 10000)}`,
      object_id: opInput.object_id,
      client_id: this.clientID,
      workflow: opInput.workflow,
      prepare_token: opInput.prepare_token,
      clock,
      type: opInput.type,
      path: opInput.path,
      value: opInput.value,
    };
  }

  #ensureReady() {
    if (!this.manifestManager || !this.syncAgent || !this.clientID) {
      throw new Error("DWCE.init() must be called before using the runtime");
    }
  }

  #emit(event, payload) {
    for (const listener of this.listeners) {
      listener({ event, payload, at: new Date().toISOString() });
    }
  }

  #getOrCreateClientID() {
    const key = "seed_client_id";
    const existing = localStorage.getItem(key);
    if (existing) {
      return existing;
    }
    const next = `device-${crypto.randomUUID()}`;
    localStorage.setItem(key, next);
    return next;
  }
}

export const DWCE = new DWCERuntime();
