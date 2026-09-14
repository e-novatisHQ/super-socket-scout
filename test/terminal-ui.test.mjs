import assert from "node:assert/strict";
import test from "node:test";
import { confirmStop } from "../src/ui/terminal-ui.mjs";

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
