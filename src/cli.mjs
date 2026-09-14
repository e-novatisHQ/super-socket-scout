#!/usr/bin/env node
import { createServerAdapter } from "./domain/server-adapter.mjs";
import { EXIT, inventory, stopServers } from "./orchestrator.mjs";
import { askSearch, chooseAction, chooseServerAction, confirmStop, renderDashboardHeader, renderInventory, report } from "./ui/terminal-ui.mjs";
import { defaultFilters, filterServers, SCOPE_ORDER } from "./ui/view-model.mjs";

const usage = `Usage:
  npm run tui
  node src/cli.mjs status [--sudo] [--json]
  node src/cli.mjs stop (--server ID ... | --all) --yes --confirm STOP:PID[,PID] [--include-system] [--json]

L'arrêt est gracieux (SIGTERM) et vise l'arbre du lanceur lorsque celui-ci est identifiable.
Les processus système sont protégés sauf avec --include-system.`;

export function parseArguments(argv) {
  const firstIsOption = argv[0]?.startsWith("-") ?? false;
  const result = { command: firstIsOption ? "tui" : (argv[0] ?? "tui"), ids: [], all: false, yes: false, json: false, sudo: false, includeSystem: false };
  for (let i = firstIsOption || result.command === "tui" && argv[0] !== "tui" ? 0 : 1; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--server") { if (!argv[i + 1]) throw new Error("--server exige un ID"); result.ids.push(argv[++i]); }
    else if (arg === "--all") result.all = true;
    else if (arg === "--yes") result.yes = true;
    else if (arg === "--confirm") { if (!argv[i + 1]) throw new Error("--confirm exige un jeton"); result.confirmToken = argv[++i]; }
    else if (arg === "--json") result.json = true;
    else if (arg === "--sudo") result.sudo = true;
    else if (arg === "--include-system") result.includeSystem = true;
    else if (["--help", "-h"].includes(arg)) result.help = true;
    else throw new Error(`Option inconnue : ${arg}`);
  }
  if (!["tui", "status", "stop"].includes(result.command)) throw new Error(`Commande inconnue : ${result.command}`);
  if (result.sudo && result.command !== "status") throw new Error("--sudo est réservé à la commande status");
  return result;
}

function printable(item) {
  const { status, disabledReason, ...safe } = item;
  return safe;
}

async function runTui(adapter) {
  if (!process.stdin.isTTY || !process.stdout.isTTY) throw Object.assign(new Error("La TUI exige un terminal"), { exitCode: EXIT.INVALID });
  const filters = defaultFilters();
  while (true) {
    filters.elevated = adapter.isPrivileged?.() ?? false;
    const items = await inventory(adapter);
    const visible = filterServers(items, filters);
    renderDashboardHeader(items, visible, filters);
    const action = await chooseAction(visible, filters);
    if (action.type === "quit") return EXIT.SUCCESS;
    if (action.type === "refresh") continue;
    if (action.type === "scope") {
      const current = SCOPE_ORDER.indexOf(filters.scope);
      filters.scope = SCOPE_ORDER[(current + action.direction + SCOPE_ORDER.length) % SCOPE_ORDER.length];
      continue;
    }
    if (action.type === "scope-direct") { filters.scope = action.scope; continue; }
    if (action.type === "udp") { filters.showUdp = !filters.showUdp; continue; }
    if (action.type === "search") { filters.query = await askSearch(filters.query); continue; }
    if (action.type === "elevation" && adapter.isPrivileged?.()) { adapter.disablePrivilege(); continue; }
    if (action.type === "elevation") {
      process.stdout.write("\nAuthentification sudo pour une collecte en lecture seule…\n");
      const result = await adapter.enablePrivilege();
      process.stdout.write(`${result.ok ? "✓" : "✗"} ${result.message}\n`);
      await new Promise((resolve) => setTimeout(resolve, 1_000));
      continue;
    }
    const server = items.find((item) => item.id === action.id);
    if (await chooseServerAction(server) !== "stop") continue;
    const outcome = await stopServers({ adapter, ids: [server.id], includeSystem: true, confirm: confirmStop });
    if (outcome.error) process.stderr.write(`${outcome.error}\n`);
    report(outcome.results);
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
}

export async function main(argv = process.argv.slice(2), adapter = createServerAdapter()) {
  const options = parseArguments(argv);
  if (options.help) { console.log(usage); return EXIT.SUCCESS; }
  if (options.command === "tui") return runTui(adapter);
  if (options.sudo) {
    const elevated = await adapter.enablePrivilege();
    if (!elevated.ok) throw new Error(`Élévation impossible : ${elevated.message}`);
  }
  const items = await inventory(adapter);
  if (options.command === "status") {
    if (options.json) console.log(JSON.stringify(items.map(printable), null, 2));
    else renderInventory(items);
    return EXIT.SUCCESS;
  }
  const ids = options.all ? items.filter((item) => item.pid && (options.includeSystem || !item.system)).map((item) => item.id) : options.ids;
  if (!ids.length) throw Object.assign(new Error("stop exige --server ID ou --all"), { exitCode: EXIT.INVALID });
  const outcome = await stopServers({ adapter, ids, ...options });
  if (options.json) console.log(JSON.stringify(outcome, null, 2));
  else {
    if (outcome.plan) {
      console.log("Plan :");
      for (const action of outcome.plan.actions) console.log(`  - ${action.description}`);
      console.log(`Jeton requis : ${outcome.plan.confirmationToken}`);
    }
    if (outcome.error) console.error(outcome.error);
    if (outcome.results.length) report(outcome.results);
  }
  return outcome.code;
}

if (process.argv[1]?.endsWith("/cli.mjs")) {
  main().then((code) => { process.exitCode = code; }).catch((error) => {
    if (["ExitPromptError", "AbortPromptError"].includes(error.name)) process.exitCode = EXIT.CANCELLED;
    else { console.error(`Erreur : ${error.message}`); process.exitCode = error.exitCode ?? EXIT.ERROR; }
  });
}
