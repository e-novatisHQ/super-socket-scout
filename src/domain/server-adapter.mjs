import { createLinuxAdapter } from "../system/linux-adapter.mjs";

export function createServerAdapter(options = {}) {
  const system = options.system ?? createLinuxAdapter(options);
  let snapshot = new Map();

  return {
    isPrivileged() { return system.isPrivileged?.() ?? false; },
    async enablePrivilege() {
      return system.enablePrivilege?.() ?? { ok: false, message: "Élévation indisponible sur ce système" };
    },
    disablePrivilege() {
      return system.disablePrivilege?.() ?? { ok: true, message: "Identification standard active" };
    },

    async discover() {
      const items = await system.discover();
      snapshot = new Map(items.map((item) => [item.id, item]));
      return items;
    },

    async buildPlan(selectedIds, context = {}) {
      const actions = [];
      const conflicts = [];
      for (const id of [...new Set(selectedIds)]) {
        const item = snapshot.get(id);
        if (!item) { conflicts.push({ id, reason: "Serveur absent de l'inventaire" }); continue; }
        if (!item.pid) { conflicts.push({ id, reason: item.disabledReason }); continue; }
        if (item.system && !context.includeSystem) {
          conflicts.push({ id, reason: "Processus système protégé (utiliser --include-system)" });
          continue;
        }
        actions.push({
          id, pid: item.pid, startTime: item.startTime, target: item,
          description: `Arrêter PID ${item.pid} — ${item.label} — ${item.command ?? item.processName ?? "inconnu"}`,
        });
      }
      const token = `STOP:${actions.map((action) => action.pid).sort((a, b) => a - b).join(",")}`;
      return { risk: "destructive", confirmationToken: token, actions, conflicts };
    },

    async execute(plan) {
      const results = plan.conflicts.map((conflict) => ({ ...conflict, status: "conflict" }));
      for (const action of plan.actions) results.push(await system.stop(action.target));
      return results;
    },

    formatChoice(item) { return item.label; },
    formatAction(action) { return action.description; },
  };
}

const defaultAdapter = createServerAdapter();
export async function discover(...args) { return defaultAdapter.discover(...args); }
export async function buildPlan(...args) { return defaultAdapter.buildPlan(...args); }
export async function execute(...args) { return defaultAdapter.execute(...args); }
export const formatChoice = (...args) => defaultAdapter.formatChoice(...args);
export const formatAction = (...args) => defaultAdapter.formatAction(...args);
