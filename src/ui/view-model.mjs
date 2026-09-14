export const SCOPES = Object.freeze({
  relevant: "Vue pertinente",
  exposed: "Exposés",
  projects: "Projets",
  worktrees: "Worktrees",
  orphans: "Orphelins",
  system: "Système",
  all: "Tous",
});

export const SCOPE_ORDER = Object.freeze([
  "relevant", "exposed", "projects", "worktrees", "orphans", "system", "all",
]);

export function nextScope(current) {
  const index = SCOPE_ORDER.indexOf(current);
  return SCOPE_ORDER[(index + 1) % SCOPE_ORDER.length];
}

export function defaultFilters() {
  return { scope: "relevant", query: "", showUdp: false };
}

export function isUnknownExposed(item) {
  return item.exposed && !item.pid;
}

export function isAnomaly(item) {
  return item.orphan || isUnknownExposed(item);
}

export function categoryOf(item) {
  if (isAnomaly(item)) return "alerts";
  if (item.project && !item.system) return "projects";
  if (item.system) return "system";
  if (!item.exposed) return "local";
  return "other";
}

function matchesScope(item, scope) {
  if (scope === "all") return true;
  if (scope === "exposed") return item.exposed;
  if (scope === "projects") return Boolean(item.project);
  if (scope === "worktrees") return item.worktree;
  if (scope === "orphans") return item.orphan;
  if (scope === "system") return item.system;
  return isAnomaly(item) || Boolean(item.project) || (!item.system && item.exposed && item.protocol === "tcp");
}

function haystack(item) {
  return [item.id, item.port, item.pid, item.serviceName, item.runtime, item.manager, item.project, item.branch, item.cwd, item.command, item.processName, ...(item.evidence ?? [])]
    .filter((value) => value !== null && value !== undefined)
    .join(" ").toLocaleLowerCase("fr-FR");
}

export function filterServers(items, filters) {
  const query = filters.query.trim().toLocaleLowerCase("fr-FR");
  return items.filter((item) =>
    (filters.showUdp || item.protocol === "tcp")
    && matchesScope(item, filters.scope)
    && (!query || haystack(item).includes(query)),
  );
}

export function summarize(items) {
  return {
    alerts: items.filter(isAnomaly).length,
    projects: new Set(items.map((item) => item.project).filter(Boolean)).size,
    exposed: items.filter((item) => item.exposed).length,
    system: items.filter((item) => item.system).length,
    worktrees: items.filter((item) => item.worktree).length,
    total: items.length,
  };
}

const SECTION_ORDER = ["alerts", "projects", "other", "system", "local"];
export const SECTION_LABELS = Object.freeze({
  alerts: "À SURVEILLER",
  projects: "SERVEURS DE PROJET",
  other: "AUTRES APPLICATIONS EXPOSÉES",
  system: "SERVICES SYSTÈME",
  local: "SERVICES LOCAUX",
});

export function groupServers(items) {
  const groups = new Map(SECTION_ORDER.map((key) => [key, []]));
  for (const item of items) groups.get(categoryOf(item)).push(item);
  return SECTION_ORDER.map((key) => ({ key, label: SECTION_LABELS[key], items: groups.get(key) }))
    .filter((section) => section.items.length);
}
