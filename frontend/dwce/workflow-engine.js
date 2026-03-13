export class WorkflowEngine {
  constructor() {
    this.workflows = new Map();
  }

  registerWorkflow(name, definition) {
    if (!name) {
      throw new Error("workflow name is required");
    }
    const normalized = normalizeDefinition(definition);
    this.workflows.set(name, normalized);
    return normalized;
  }

  getWorkflow(name) {
    const workflow = this.workflows.get(name);
    if (!workflow) {
      throw new Error(`workflow not registered: ${name}`);
    }
    return workflow;
  }

  evaluateSafety(name) {
    const workflow = this.getWorkflow(name);
    if (workflow.requires_global_state) {
      return "UNSAFE";
    }
    if (workflow.requires_validation) {
      return "EVENTUAL";
    }
    return "SAFE";
  }

  isOfflineExecutable(name) {
    return this.evaluateSafety(name) !== "UNSAFE";
  }

  reachableSteps(name, startStep) {
    const workflow = this.getWorkflow(name);
    const inDegree = new Map();
    const out = new Map();

    for (const node of workflow.steps) {
      inDegree.set(node, 0);
      out.set(node, []);
    }

    for (const [from, to] of workflow.edges) {
      out.get(from).push(to);
      inDegree.set(to, (inDegree.get(to) || 0) + 1);
    }

    const queue = [];
    for (const [node, deg] of inDegree.entries()) {
      if (deg === 0) {
        queue.push(node);
      }
    }

    const topo = [];
    while (queue.length > 0) {
      const node = queue.shift();
      topo.push(node);
      for (const next of out.get(node)) {
        inDegree.set(next, inDegree.get(next) - 1);
        if (inDegree.get(next) === 0) {
          queue.push(next);
        }
      }
    }

    if (topo.length !== workflow.steps.length) {
      throw new Error(`workflow ${name} contains a cycle`);
    }

    if (!startStep) {
      return topo;
    }

    const startIdx = topo.indexOf(startStep);
    if (startIdx < 0) {
      throw new Error(`unknown start step: ${startStep}`);
    }

    return topo.slice(startIdx);
  }
}

function normalizeDefinition(definition) {
  if (!definition || !Array.isArray(definition.steps) || definition.steps.length === 0) {
    throw new Error("workflow definition requires a non-empty steps array");
  }
  const steps = [...new Set(definition.steps)];
  const edges = [];
  if (Array.isArray(definition.edges) && definition.edges.length > 0) {
    for (const edge of definition.edges) {
      if (!Array.isArray(edge) || edge.length !== 2) {
        throw new Error("each edge must be [from, to]");
      }
      edges.push([edge[0], edge[1]]);
    }
  } else {
    for (let i = 0; i < steps.length - 1; i += 1) {
      edges.push([steps[i], steps[i + 1]]);
    }
  }

  const explicitSafety = (definition.safety_class || "").toUpperCase();
  const normalized = {
    steps,
    edges,
    requires_global_state: Boolean(definition.requires_global_state),
    requires_validation: Boolean(definition.requires_validation),
  };

  if (explicitSafety === "UNSAFE") {
    normalized.requires_global_state = true;
  } else if (explicitSafety === "EVENTUAL") {
    normalized.requires_validation = true;
  }

  return normalized;
}
