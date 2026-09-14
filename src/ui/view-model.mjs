export const SCOPES = Object.freeze({
  attention: "Attention",
  projects: "Projets",
  system: "Système",
  all: "Tous",
});

export const SCOPE_ORDER = Object.freeze([
  "attention", "projects", "system", "all",
]);

export function nextScope(current) {
  const index = SCOPE_ORDER.indexOf(current);
  return SCOPE_ORDER[(index + 1) % SCOPE_ORDER.length];
}

export function defaultFilters() {
  return { scope: "attention", query: "", showUdp: false };
}

export function isUnknownExposed(item) {
  return item.exposed && !item.pid;
}

export function isAnomaly(item) {
  return diagnosticOf(item) !== "normal";
}

export function diagnosticOf(item) {
  if (item.orphan) return "action";
  if (isUnknownExposed(item) || item.exposed && ["unknown", "partial"].includes(item.confidence ?? "unknown")) return "review";
  return "normal";
}

export function categoryOf(item) {
  return diagnosticOf(item);
}

function matchesScope(item, scope) {
  if (scope === "all") return true;
  if (scope === "projects") return Boolean(item.project);
  if (scope === "system") return item.system;
  return diagnosticOf(item) !== "normal";
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
    actions: items.filter((item) => diagnosticOf(item) === "action").length,
    review: items.filter((item) => diagnosticOf(item) === "review").length,
    normal: items.filter((item) => diagnosticOf(item) === "normal").length,
    alerts: items.filter((item) => diagnosticOf(item) !== "normal").length,
    projects: new Set(items.map((item) => item.project).filter(Boolean)).size,
    projectServers: items.filter((item) => item.project && !item.system).length,
    exposed: items.filter((item) => item.exposed).length,
    system: items.filter((item) => item.system).length,
    worktrees: items.filter((item) => item.worktree).length,
    total: items.length,
  };
}

const SECTION_ORDER = ["action", "review", "normal"];
export const SECTION_LABELS = Object.freeze({
  action: "ORPHELINS — ACTION REQUISE",
  review: "À VÉRIFIER",
  normal: "SANS ANOMALIE",
});

export function groupServers(items) {
  const groups = new Map(SECTION_ORDER.map((key) => [key, []]));
  for (const item of items) groups.get(categoryOf(item)).push(item);
  return SECTION_ORDER.map((key) => ({ key, label: SECTION_LABELS[key], items: groups.get(key) }))
    .filter((section) => section.items.length);
}
