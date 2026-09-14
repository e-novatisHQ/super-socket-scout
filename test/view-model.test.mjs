import assert from "node:assert/strict";
import test from "node:test";
import { categoryOf, defaultFilters, filterServers, groupServers, nextScope, SCOPE_ORDER, summarize } from "../src/ui/view-model.mjs";
import { createLinuxAdapter } from "../src/system/linux-adapter.mjs";
import { buildInteractiveChoices } from "../src/ui/terminal-ui.mjs";
import { shortcutAction } from "../src/ui/server-prompt.mjs";

const items = [
  { id: "orphan", protocol: "tcp", port: 31000, exposed: true, pid: 1, orphan: true, system: false, worktree: false },
  { id: "project", protocol: "tcp", port: 5173, exposed: true, pid: 2, project: "/repo/app", orphan: false, system: false, worktree: false },
  { id: "worktree", protocol: "tcp", port: 5174, exposed: false, pid: 3, project: "/repo/app", orphan: false, system: false, worktree: true },
  { id: "unknown", protocol: "tcp", port: 7070, exposed: true, pid: null, orphan: false, system: true, worktree: false },
  { id: "system", protocol: "tcp", port: 22, exposed: true, pid: 4, orphan: false, system: true, worktree: false },
  { id: "udp", protocol: "udp", port: 5353, exposed: true, pid: 5, orphan: false, system: true, worktree: false },
];

test("la vue pertinente masque le bruit système et UDP", () => {
  assert.deepEqual(filterServers(items, defaultFilters()).map((item) => item.id), ["orphan", "project", "worktree", "unknown"]);
});

test("les filtres worktree, système et recherche sont combinables", () => {
  assert.deepEqual(filterServers(items, { scope: "worktrees", query: "5174", showUdp: true }).map((item) => item.id), ["worktree"]);
  assert.deepEqual(filterServers(items, { scope: "system", query: "", showUdp: true }).map((item) => item.id), ["unknown", "system", "udp"]);
});

test("la hiérarchie place une exposition inconnue dans les alertes", () => {
  assert.equal(categoryOf(items[3]), "alerts");
  assert.deepEqual(groupServers(items.slice(0, 4)).map((section) => section.key), ["alerts", "projects"]);
});

test("le résumé compte projets uniques et catégories opérationnelles", () => {
  assert.deepEqual(summarize(items), { alerts: 2, projects: 1, exposed: 5, system: 3, worktrees: 1, total: 6 });
});

test("les sockets IPv4 et IPv6 inconnues d'un même port forment un service", async () => {
  const runner = async (command) => {
    if (command === "ss") return { ok: true, stdout: [
      "tcp LISTEN 0 10 0.0.0.0:7070 0.0.0.0:*",
      "tcp LISTEN 0 10 [::]:7070 [::]:*",
    ].join("\n"), stderr: "" };
    return { ok: false, stdout: "", stderr: "indisponible" };
  };
  const discovered = await createLinuxAdapter({ runner }).discover();
  assert.equal(discovered.length, 1);
  assert.equal(discovered[0].id, "unknown:tcp:7070");
  assert.equal(discovered[0].endpoints.length, 2);
});

test("le tableau interactif contient une seule ligne navigable par serveur", () => {
  const choices = buildInteractiveChoices(items, defaultFilters());
  const serverChoices = choices.filter((choice) => choice.value?.type === "server");
  assert.equal(serverChoices.length, items.length);
  assert.deepEqual(
    serverChoices.map((choice) => choice.value.id).sort(),
    items.map((item) => item.id).sort(),
  );
  assert.equal(serverChoices.find((choice) => choice.value.id === "unknown").disabled, undefined);
});

test("la vue bascule dans un cycle déterministe sans sous-menu", () => {
  assert.equal(nextScope("relevant"), "exposed");
  assert.equal(nextScope("worktrees"), "orphans");
  assert.equal(nextScope(SCOPE_ORDER.at(-1)), "relevant");
});

test("les actions globales sont des raccourcis et non des éléments de menu", () => {
  const choices = buildInteractiveChoices(items, defaultFilters());
  assert.ok(choices.every((choice) => !["quit", "refresh", "elevation", "search", "udp", "scope"].includes(choice.value?.type)));
  assert.deepEqual(shortcutAction({ name: "q" }, SCOPE_ORDER), { type: "quit" });
  assert.deepEqual(shortcutAction({ name: "r" }, SCOPE_ORDER), { type: "refresh" });
  assert.deepEqual(shortcutAction({ name: "e" }, SCOPE_ORDER), { type: "elevation" });
  assert.deepEqual(shortcutAction({ sequence: "/" }, SCOPE_ORDER), { type: "search" });
  assert.deepEqual(shortcutAction({ name: "u" }, SCOPE_ORDER), { type: "udp" });
  assert.deepEqual(shortcutAction({ name: "3" }, SCOPE_ORDER), { type: "scope-direct", scope: SCOPE_ORDER[2] });
  assert.deepEqual(shortcutAction({ name: "left" }, SCOPE_ORDER), { type: "scope", direction: -1 });
});
