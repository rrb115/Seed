export class DependencyGraphEngine {
  constructor() {
    this.stepDeps = new Map(); // workflow:step -> resources[]
    this.resourceDeps = new Map(); // resource -> resource[]
  }

  registerStepDependencies(workflowName, step, resources) {
    const key = `${workflowName}:${step}`;
    this.stepDeps.set(key, uniq(resources || []));
  }

  registerWorkflowStepMap(workflowName, map) {
    for (const [step, resources] of Object.entries(map || {})) {
      this.registerStepDependencies(workflowName, step, resources);
    }
  }

  registerResourceDependencies(resource, deps) {
    this.resourceDeps.set(resource, uniq(deps || []));
  }

  registerResourceDependencyMap(map) {
    for (const [resource, deps] of Object.entries(map || {})) {
      this.registerResourceDependencies(resource, deps);
    }
  }

  dependenciesForStep(workflowName, step) {
    return this.stepDeps.get(`${workflowName}:${step}`) || [];
  }

  computeMinimalClosure(workflowName, steps) {
    const initial = new Set();
    for (const step of steps) {
      for (const resource of this.dependenciesForStep(workflowName, step)) {
        initial.add(resource);
      }
    }

    const closure = new Set(initial);
    const stack = [...initial];

    while (stack.length > 0) {
      const resource = stack.pop();
      for (const dep of this.resourceDeps.get(resource) || []) {
        if (!closure.has(dep)) {
          closure.add(dep);
          stack.push(dep);
        }
      }
    }

    return [...closure];
  }
}

function uniq(arr) {
  return [...new Set((arr || []).filter(Boolean))];
}
