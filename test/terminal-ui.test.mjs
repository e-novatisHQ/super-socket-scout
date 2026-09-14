import assert from "node:assert/strict";
import test from "node:test";
import { buildInteractiveChoices, confirmStop, guidance, renderDashboardHeader } from "../src/ui/terminal-ui.mjs";

test("la confirmation interactive ne demande qu'un choix oui/non", async () => {
  const calls = [];
  const accepted = await confirmStop({
    confirmationToken: "STOP:42",
    actions: [{ pid: 42, description: "Arrêter le serveur de test" }],
  }, {
    confirmPrompt: async (options) => { calls.push(options); return true; },
  });

  assert.equal(accepted, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].default, false);
  assert.match(calls[0].message, /PID 42/);
  assert.doesNotMatch(calls[0].message, /STOP:42/);
});

test("le bandeau commence par une synthèse décisionnelle et quatre onglets", () => {
  let output = "";
  const items = [
    { id: "orphan", orphan: true, exposed: true, protocol: "tcp", system: false },
    { id: "normal", orphan: false, exposed: false, protocol: "tcp", system: false, project: "/repo/app", confidence: "certain" },
  ];
  renderDashboardHeader(items, [items[0]], { scope: "attention", query: "", showUdp: false, elevated: false }, { write: (value) => { output += value; } });
  const plain = output.replace(/\x1b\[[0-9;]*m/g, "");
  assert.match(plain, /1 action recommandée/);
  assert.match(plain, /1 sans anomalie/);
  assert.match(plain, /\|1 Attention\|.*2 Projets.*3 Système.*4 Tous/s);
  assert.match(plain, /  \|1 Attention\|   2 Projets   3 Système   4 Tous\n── {13}─/);
  assert.doesNotMatch(plain, /Vues\s*:/);
  assert.doesNotMatch(plain, /Worktrees.*Orphelins/);
  assert.match(plain, /1\/1 affichés/);
});

test("l'aide sélectionnée explique la décision attendue", () => {
  assert.match(guidance({ orphan: true }), /Dossier de travail disparu/);
  assert.match(guidance({ orphan: false, exposed: true, pid: null }), /e : mieux identifier/);
  assert.ok(guidance({ orphan: false, exposed: true, pid: null }).length < 80);
});

test("les colonnes sont alignées et le diagnostic n'est pas répété sur chaque ligne", () => {
  const choices = buildInteractiveChoices([
    { id: "ssh", serviceName: "SSH", protocol: "tcp", port: 22, exposed: true, pid: 1, system: true, confidence: "probable" },
  ], {});
  const plain = choices.map((choice) => choice.name.replace(/\x1b\[[0-9;]*m/g, ""));
  assert.equal(plain[1].startsWith("SERVICE"), true);
  assert.equal(plain[2].startsWith("SSH"), true);
  assert.doesNotMatch(plain[1], /DIAGNOSTIC/);
  assert.doesNotMatch(plain[2], /À VÉRIFIER/);
});

test("le tableau adapte ses informations à la largeur disponible", () => {
  const item = { id: "node", serviceName: "Application Node.js", runtime: "Node.js", protocol: "tcp", port: 5173, exposed: true, pid: 2, project: "/repo/un-projet-au-nom-tres-long", worktree: true, confidence: "certain" };
  const plain = (width) => buildInteractiveChoices([item], {}, width).map((choice) => choice.name.replace(/\x1b\[[0-9;]*m/g, ""));
  assert.doesNotMatch(plain(70)[1], /CONTEXTE|RUNTIME/);
  assert.match(plain(100)[1], /CONTEXTE/);
  assert.doesNotMatch(plain(100)[1], /RUNTIME/);
  assert.match(plain(130)[1], /RUNTIME/);
  assert.match(plain(130)[2], /un-projet-au-nom-tres-long · worktree/);
});

test("l'inventaire abrège les libellés répétés sans modifier les détails", () => {
  const choices = buildInteractiveChoices([
    { id: "node", serviceName: "Application Node.js", protocol: "tcp", port: 5173, exposed: true, pid: 2, confidence: "partial" },
    { id: "unknown", serviceName: "Service non identifié", protocol: "tcp", port: 7070, exposed: false, pid: null },
  ], {}, 100);
  const rows = choices.filter((choice) => choice.value?.type === "server").map((choice) => choice.name);
  assert.match(rows[0], /^App Node\.js/);
  assert.match(rows[0], /5173 ext/);
  assert.match(rows[1], /^Non identifié/);
  assert.match(rows[1], /7070 loc/);
});

test("un serveur orphelin est identifiable directement dans l'inventaire", () => {
  const choices = buildInteractiveChoices([
    { id: "orphan", serviceName: "Application Node.js", protocol: "tcp", port: 31000, exposed: true, pid: 8, orphan: true, worktree: true },
  ], {}, 100);
  const plain = choices.map((choice) => choice.name.replace(/\x1b\[[0-9;]*m/g, ""));
  assert.match(plain[0], /ORPHELINS — ACTION REQUISE/);
  assert.match(plain[2], /Worktree disparu/);
});
