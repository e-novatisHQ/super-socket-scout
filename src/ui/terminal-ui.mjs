import confirm from "@inquirer/confirm";
import input from "@inquirer/input";
import select from "@inquirer/select";
import { SCOPES, groupServers, summarize } from "./view-model.mjs";
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

function stateLabel(server) {
  if (server.orphan) return color.red("ORPHELIN");
  if (server.confidence === "certain") return color.green("✓ CERTAIN");
  if (server.confidence === "probable") return color.yellow("~ PROBABLE");
  if (server.confidence === "partial") return color.yellow("· PARTIEL");
  return color.red("? INCONNU");
}

function ownerLabel(item) {
  return item.serviceName ?? item.processName ?? item.command?.split(" ")[0]?.split("/").at(-1) ?? "Service non identifié";
}

function row(item, compact = false) {
  const endpointWidth = compact ? 11 : 14;
  const ownerWidth = compact ? 18 : 26;
  const originWidth = compact ? 16 : 25;
  const endpoint = short(`${item.port}/${item.protocol}${item.endpoints?.length > 1 ? ` +${item.endpoints.length - 1}` : ""}`, endpointWidth).padEnd(endpointWidth);
  const exposure = (item.exposed ? "EXTERNE" : "LOCAL").padEnd(8);
  const owner = short(ownerLabel(item), ownerWidth).padEnd(ownerWidth);
  const projectName = item.project?.split("/").at(-1);
  const origin = item.worktree ? `${projectName} · worktree`
    : projectName ? `${projectName} · dépôt` : item.manager ?? (item.system ? "service système" : "hors projet");
  return `  ${endpoint} ${exposure} ${owner} ${short(origin, originWidth).padEnd(originWidth)} ${stateLabel(item)}`;
}

export function renderInventory(allItems, visibleItems = allItems, filters = { scope: "all", query: "", showUdp: true }, stdout = process.stdout) {
  renderDashboardHeader(allItems, visibleItems, filters, stdout);
  if (!visibleItems.length) stdout.write("\n  Aucun serveur ne correspond aux filtres.\n");
  for (const section of groupServers(visibleItems)) {
    stdout.write(`\n${color.bold(section.label)}  ${color.dim(`(${section.items.length})`)}\n`);
    stdout.write(color.dim("  PORT           ACCÈS    PROJET / SERVICE           ORIGINE                   ÉTAT\n"));
    for (const item of section.items) stdout.write(`${row(item)}\n`);
  }
}

export function renderDashboardHeader(allItems, visibleItems, filters, stdout = process.stdout) {
  const summary = summarize(allItems);
  const visibleSummary = summarize(visibleItems);
  const hiddenSystem = allItems.filter((item) => item.system && !visibleItems.some((visible) => visible.id === item.id)).length;
  stdout.write("\x1b[2J\x1b[H");
  stdout.write(`${color.bold("Supervision réseau")}  ${color.dim(`actualisé ${new Date().toLocaleTimeString("fr-FR")}`)}\n\n`);
  stdout.write(`${summary.alerts ? color.red(`⚠ ${visibleSummary.alerts} visibles / ${summary.alerts} à surveiller`) : color.green("✓ aucune anomalie")}   `);
  stdout.write(`${color.cyan(`● ${summary.projects} projets`)}   ${color.yellow(`◆ ${summary.system} système`)}   ○ ${summary.total} services\n`);
  stdout.write(`Exposition : ${color.red(`${summary.exposed} externes`)}   Worktrees : ${summary.worktrees}\n`);
  const tabs = Object.entries(SCOPES).map(([key, label], index) => key === filters.scope
    ? color.cyan(`[${index + 1} ${label}]`)
    : color.dim(`${index + 1} ${label}`)).join("  ");
  stdout.write(`Vues : ${tabs}\n`);
  stdout.write(`UDP ${filters.showUdp ? "affiché" : "masqué"} · Identification ${filters.elevated ? color.yellow("élevée") : "standard"}${filters.query ? ` · recherche « ${filters.query} »` : ""}\n`);
  if (filters.scope === "relevant" && hiddenSystem) stdout.write(color.dim(`\n${hiddenSystem} services système repliés — choisir le filtre Système ou Tous pour les afficher.\n`));
}

export function buildInteractiveChoices(items, filters) {
  const choices = [];
  for (const section of groupServers(items)) {
    choices.push({ name: `── ${section.label} (${section.items.length})`, value: null, disabled: true });
    choices.push({ name: color.dim("   PORT        ACCÈS    SERVICE            CONTEXTE         IDENT."), value: null, disabled: true });
    for (const item of section.items) {
      choices.push({
        name: row(item, true).trimStart(),
        value: { type: "server", id: item.id },
      });
    }
  }
  return choices;
}

export async function chooseAction(items, filters) {
  return serverPrompt({
    choices: buildInteractiveChoices(items, filters),
    scopes: Object.keys(SCOPES),
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
