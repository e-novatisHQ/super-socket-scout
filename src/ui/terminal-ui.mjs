import confirm from "@inquirer/confirm";
import input from "@inquirer/input";
import select from "@inquirer/select";
import { diagnosticOf, filterServers, SCOPES, groupServers, summarize } from "./view-model.mjs";
import { serverPrompt } from "./server-prompt.mjs";

const color = {
  red: (v) => `\x1b[31m${v}\x1b[0m`, green: (v) => `\x1b[32m${v}\x1b[0m`,
  yellow: (v) => `\x1b[33m${v}\x1b[0m`, cyan: (v) => `\x1b[36m${v}\x1b[0m`,
  dim: (v) => `\x1b[2m${v}\x1b[0m`, bold: (v) => `\x1b[1m${v}\x1b[0m`,
};

function short(value, length) {
  if (!value) return "—";
  return value.length > length ? `${value.slice(0, length - 1)}…` : value;
}

function flags(server) {
  return [
    server.exposed ? color.red("EXPOSÉ") : color.green("LOCAL"),
    server.system ? color.yellow("SYSTÈME") : null,
    server.worktree ? color.cyan("WORKTREE") : null,
    server.orphan ? color.red("ORPHELIN") : null,
  ].filter(Boolean).join(" ");
}

function ownerLabel(item) {
  return item.serviceName ?? item.processName ?? item.command?.split(" ")[0]?.split("/").at(-1) ?? "Service non identifié";
}

function compactOwnerLabel(item) {
  return ownerLabel(item)
    .replace(/^Application\b/, "App")
    .replace(/^Service non identifié$/, "Non identifié");
}

function contextLabel(item) {
  if (item.orphan) return item.worktree ? "Worktree disparu" : "Dossier disparu";
  const projectName = item.project?.split("/").at(-1);
  if (item.worktree) return `${projectName ?? "Projet"} · worktree`;
  if (projectName) return projectName;
  if (item.system) return "Système";
  return "Hors projet";
}

function listeningLabel(item) {
  const protocol = item.protocol === "udp" ? " UDP" : "";
  const more = item.endpoints?.length > 1 ? ` +${item.endpoints.length - 1}` : "";
  return `${item.port}${protocol}${more} ${item.exposed ? "ext" : "loc"}`;
}

function usefulWidth(columns) {
  return Math.max(50, Math.min(columns ?? 100, 130));
}

export function tableLayout(width = 100) {
  const contentWidth = usefulWidth(width) - 2;
  if (width < 80) return { service: Math.max(18, contentWidth - 19), listening: 18, context: 0, runtime: 0 };
  if (width < 120) return { service: 23, context: Math.max(21, contentWidth - 42), listening: 17, runtime: 0 };
  return { service: 26, context: Math.max(28, contentWidth - 67), listening: 18, runtime: 20 };
}

function tableHeader(width, leading = true) {
  const layout = tableLayout(width);
  const prefix = leading ? "  " : "";
  if (!layout.context) return `${prefix}${"SERVICE".padEnd(layout.service)} ${"ÉCOUTE".padEnd(layout.listening)}`;
  const base = `${prefix}${"SERVICE".padEnd(layout.service)} ${"CONTEXTE".padEnd(layout.context)} ${"ÉCOUTE".padEnd(layout.listening)}`;
  return layout.runtime ? `${base} RUNTIME` : base;
}

export function guidance(item) {
  if (item.orphan) return "Dossier de travail disparu · Entrée : comprendre ou arrêter";
  if (diagnosticOf(item) === "review" && !item.pid) return "Propriétaire masqué · e : mieux identifier · Entrée : détails";
  if (diagnosticOf(item) === "review") return "Identification incertaine · Entrée : voir les preuves";
  if (item.project) return "Serveur rattaché à un projet · Entrée : détails";
  if (item.system) return "Service système identifié · Entrée : détails";
  return "Aucune anomalie détectée · Entrée : détails";
}

function row(item, width = 100, leading = true) {
  const layout = tableLayout(width);
  const prefix = leading ? "  " : "";
  const service = short(compactOwnerLabel(item), layout.service).padEnd(layout.service);
  const listening = short(listeningLabel(item), layout.listening).padEnd(layout.listening);
  if (!layout.context) return `${prefix}${service} ${listening}`;
  const context = short(contextLabel(item), layout.context).padEnd(layout.context);
  const base = `${prefix}${service} ${context} ${listening}`;
  const runtime = item.runtime ?? item.manager ?? "—";
  return layout.runtime ? `${base} ${short(runtime, layout.runtime)}` : base;
}

function sectionLabel(section) {
  const label = `${section.label}  (${section.items.length})`;
  if (section.key === "action") return color.red(label);
  if (section.key === "review") return color.yellow(label);
  return color.green(label);
}

export function renderInventory(allItems, visibleItems = allItems, filters = { scope: "all", query: "", showUdp: true }, stdout = process.stdout) {
  const width = usefulWidth(stdout.columns);
  renderDashboardHeader(allItems, visibleItems, filters, stdout);
  if (!visibleItems.length) stdout.write("\n  Aucun serveur ne correspond aux filtres.\n");
  for (const section of groupServers(visibleItems)) {
    stdout.write(`\n${color.bold(sectionLabel(section))}\n`);
    stdout.write(`${color.dim(tableHeader(width))}\n`);
    for (const item of section.items) stdout.write(`${row(item, width)}\n`);
  }
}

export function renderDashboardHeader(allItems, visibleItems, filters, stdout = process.stdout) {
  const summary = summarize(allItems);
  const scopeTotal = filterServers(allItems, { ...filters, query: "", showUdp: true }).length;
  stdout.write("\x1b[2J\x1b[H");
  stdout.write(`${color.bold("Supervision réseau")}  ${color.dim(`actualisé ${new Date().toLocaleTimeString("fr-FR")}`)}\n\n`);
  if (!summary.alerts) stdout.write(`${color.green("✓ Rien ne nécessite votre attention")}\n`);
  else stdout.write(`${summary.actions ? color.red(`⚠ ${summary.actions} action${summary.actions > 1 ? "s" : ""} recommandée${summary.actions > 1 ? "s" : ""}`) : color.dim("0 action requise")}   ${summary.review ? color.yellow(`? ${summary.review} à vérifier`) : color.dim("0 à vérifier")}   ${color.green(`✓ ${summary.normal} sans anomalie`)}\n`);
  stdout.write(color.dim(`${summary.projectServers} serveurs de projet · ${summary.system} services système · ${summary.total} services au total\n\n`));
  const tabEntries = Object.entries(SCOPES);
  const plainTabs = tabEntries.map(([key, label], index) => key === filters.scope ? `|${index + 1} ${label}|` : `${index + 1} ${label}`);
  const tabs = tabEntries.map(([key, label], index) => key === filters.scope
    ? color.cyan(`|${index + 1} ${label}|`)
    : color.dim(`${index + 1} ${label}`));
  const targetWidth = usefulWidth(stdout.columns);
  const activeIndex = tabEntries.findIndex(([key]) => key === filters.scope);
  const gap = "   ";
  const activeStart = 2 + plainTabs.slice(0, activeIndex).reduce((length, tab) => length + tab.length + gap.length, 0);
  const activeWidth = plainTabs[activeIndex]?.length ?? 0;
  const underline = `${"─".repeat(activeStart)}${" ".repeat(activeWidth)}${"─".repeat(Math.max(0, targetWidth - activeStart - activeWidth))}`;
  stdout.write(`  ${tabs.join(gap)}\n${color.cyan(underline)}\n`);
  const visibleCount = scopeTotal ? `${visibleItems.length}/${scopeTotal} affichés · ` : "";
  stdout.write(color.dim(`   ${visibleCount}UDP ${filters.showUdp ? "affiché" : "masqué"} · ident. ${filters.elevated ? "élevée" : "standard"}${filters.query ? ` · recherche « ${filters.query} »` : ""}\n`));
}

export function buildInteractiveChoices(items, filters, width = 100) {
  const choices = [];
  for (const section of groupServers(items)) {
    choices.push({ name: sectionLabel(section), value: null, disabled: true });
    choices.push({ name: color.dim(tableHeader(width, false)), value: null, disabled: true });
    for (const item of section.items) {
      choices.push({
        name: row(item, width, false),
        value: { type: "server", id: item.id },
        description: guidance(item),
      });
    }
  }
  return choices;
}

export async function chooseAction(items, filters) {
  const width = usefulWidth(process.stdout.columns);
  return serverPrompt({
    choices: buildInteractiveChoices(items, filters, width),
    scopes: Object.keys(SCOPES),
    emptyLabel: filters.scope === "attention" ? "✓ Rien ne nécessite votre attention" : "Aucun serveur dans cette vue",
    width,
    pageSize: Math.min(24, items.length + 10),
  });
}

export async function askSearch(current = "") {
  return input({ message: "Port, projet, PID ou commande", default: current });
}

export async function chooseServerAction(server) {
  process.stdout.write(`\n${color.bold(server.serviceName ?? server.label)} — ${server.label}\n`);
  process.stdout.write(`Identification: ${server.confidence ?? "inconnue"}\nGestionnaire: ${server.manager ?? "inconnu"}\nRuntime: ${server.runtime ?? "inconnu"}\n`);
  process.stdout.write(`PID: ${server.pid ?? "masqué"}\nCommande: ${server.command ?? "inaccessible"}\nProjet: ${server.project ?? "aucun"}\n`);
  process.stdout.write(`Écoutes: ${(server.endpoints ?? []).map((e) => `${e.protocol}://${e.address}:${e.port}`).join(", ") || server.label}\n`);
  process.stdout.write(`Branche: ${server.branch ?? "—"}\nDossier: ${server.cwd ?? "—"}\nÉtat: ${flags(server)}\n`);
  if (server.orphan) process.stdout.write(`Cause: ${server.worktree ? "worktree ou dossier de travail disparu" : "dossier de travail disparu"}\n`);
  if (server.unknownReason) process.stdout.write(`Limite: ${server.unknownReason}\n`);
  process.stdout.write("Preuves:\n");
  for (const evidence of server.evidence ?? ["aucune preuve disponible"]) process.stdout.write(`  • ${evidence}\n`);
  process.stdout.write("\n");
  const choices = [{ name: "Retour à l'inventaire (recommandé)", value: "back" }];
  if (server.pid) choices.push({ name: "Arrêter le serveur", value: "stop" });
  else choices.push({ name: "Arrêt indisponible — PID non visible", value: "unavailable", disabled: true });
  return select({
    message: "Action sur ce serveur",
    choices,
  });
}

export async function confirmStop(plan, { confirmPrompt = confirm } = {}) {
  process.stdout.write("\nPlan d'arrêt :\n");
  for (const action of plan.actions) process.stdout.write(`  - ${action.description}\n`);
  const targets = plan.actions.map((action) => `PID ${action.pid}`).join(", ");
  return confirmPrompt({
    message: `Confirmer l'arrêt de ${targets} ?`,
    default: false,
  });
}

export function report(results) {
  process.stdout.write("\nRésultat :\n");
  for (const result of results) process.stdout.write(`  - ${result.id}: ${result.status}${result.message ? ` — ${result.message}` : ""}\n`);
}
