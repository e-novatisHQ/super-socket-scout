import assert from "node:assert/strict";
import test from "node:test";
import { EXIT, inventory, stopServers } from "../src/orchestrator.mjs";
import { createServerAdapter } from "../src/domain/server-adapter.mjs";

function fixture(overrides = {}) {
  let stops = 0;
  const item = {
    id: "tcp:0.0.0.0:3000:42", label: "TCP 0.0.0.0:3000", protocol: "tcp",
    address: "0.0.0.0", port: 3000, pid: 42, startTime: "100", command: "node server.mjs",
    system: false, worktree: true, orphan: false, status: "available", ...overrides,
  };
  const system = {
    discover: async () => [item],
    stop: async (target) => { stops += 1; return { id: target.id, status: "success" }; },
  };
  return { adapter: createServerAdapter({ system }), item, get stops() { return stops; } };
}

test("inventaire nominal", async () => {
  const f = fixture();
  assert.deepEqual(await inventory(f.adapter), [f.item]);
});

test("un jeton incorrect ne produit aucune mutation", async () => {
  const f = fixture();
  const result = await stopServers({ adapter: f.adapter, ids: [f.item.id], yes: true, confirmToken: "STOP:wrong" });
  assert.equal(result.code, EXIT.CONFIRMATION);
  assert.equal(f.stops, 0);
  assert.equal(result.plan.confirmationToken, "STOP:42");
});

test("confirmation exacte arrête la cible", async () => {
  const f = fixture();
  const result = await stopServers({ adapter: f.adapter, ids: [f.item.id], yes: true, confirmToken: "STOP:42" });
  assert.equal(result.code, EXIT.SUCCESS);
  assert.equal(f.stops, 1);
});

test("un processus système est protégé par défaut", async () => {
  const f = fixture({ system: true });
  const result = await stopServers({ adapter: f.adapter, ids: [f.item.id], yes: true, confirmToken: "STOP:42" });
  assert.equal(result.code, EXIT.CONFLICT);
  assert.equal(f.stops, 0);
});

test("identifiant inconnu est refusé", async () => {
  const f = fixture();
  const result = await stopServers({ adapter: f.adapter, ids: ["unknown"], yes: true, confirmToken: "STOP:42" });
  assert.equal(result.code, EXIT.INVALID);
  assert.equal(f.stops, 0);
});
