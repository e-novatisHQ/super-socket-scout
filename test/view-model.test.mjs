import assert from "node:assert/strict";
import test from "node:test";
import { categoryOf, defaultFilters, diagnosticOf, filterServers, groupServers, nextScope, SCOPE_ORDER, summarize } from "../src/ui/view-model.mjs";
import { createLinuxAdapter } from "../src/system/linux-adapter.mjs";
import { buildInteractiveChoices } from "../src/ui/terminal-ui.mjs";
import { helpLines, shortcutAction } from "../src/ui/server-prompt.mjs";

const items = [
  { id: "orphan", protocol: "tcp", port: 31000, exposed: true, pid: 1, orphan: true, system: false, worktree: false },
  { id: "project", protocol: "tcp", port: 5173, exposed: true, pid: 2, project: "/repo/app", orphan: false, system: false, worktree: false, confidence: "certain" },
  { id: "worktree", protocol: "tcp", port: 5174, exposed: false, pid: 3, project: "/repo/app", orphan: false, system: false, worktree: true, confidence: "certain" },
  { id: "unknown", protocol: "tcp", port: 7070, exposed: true, pid: null, orphan: false, system: true, worktree: false },
  { id: "system", protocol: "tcp", port: 22, exposed: true, pid: 4, orphan: false, system: true, worktree: false, confidence: "certain" },
  { id: "udp", protocol: "udp", port: 5353, exposed: true, pid: 5, orphan: false, system: true, worktree: false, confidence: "probable" },
];

test("la vue Attention ne montre que les décisions utiles et masque UDP", () => {
  assert.deepEqual(filterServers(items, defaultFilters()).map((item) => item.id), ["orphan", "unknown"]);
});

test("les vues projet, système et la recherche sont combinables", () => {
  assert.deepEqual(filterServers(items, { scope: "projects", query: "5174", showUdp: true }).map((item) => item.id), ["worktree"]);
  assert.deepEqual(filterServers(items, { scope: "system", query: "", showUdp: true }).map((item) => item.id), ["unknown", "system", "udp"]);
});

test("la hiérarchie décisionnelle distingue action, vérification et normal", () => {
  assert.equal(diagnosticOf(items[0]), "action");
  assert.equal(categoryOf(items[3]), "review");
  assert.deepEqual(groupServers(items.slice(0, 4)).map((section) => section.key), ["action", "review", "normal"]);
  assert.match(groupServers([items[0]])[0].label, /ORPHELINS/);
});

test("le résumé compte projets uniques et catégories opérationnelles", () => {
  assert.deepEqual(summarize(items), { actions: 1, review: 1, normal: 4, alerts: 2, projects: 1, projectServers: 2, exposed: 5, system: 3, worktrees: 1, total: 6 });
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
  assert.equal(nextScope("attention"), "projects");
  assert.equal(nextScope("projects"), "system");
  assert.equal(nextScope(SCOPE_ORDER.at(-1)), "attention");
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

test("l'aide répartit les raccourcis sur toute la largeur disponible", () => {
  assert.equal(helpLines(80).split("\n").length, 2);
  assert.match(helpLines(100), /détails {6}←→ vue/);
  assert.match(helpLines(100), /e sudo {6}r rafraîchir/);
  assert.equal(helpLines(130).split("\n").length, 1);
  assert.ok(helpLines(130).length <= 130);
  assert.equal(helpLines(50).split("\n").length, 4);
});
