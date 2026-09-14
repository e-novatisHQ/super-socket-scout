import assert from "node:assert/strict";
import test from "node:test";
import { parseArguments } from "../src/cli.mjs";

test("--sudo est accepté pour status uniquement", () => {
  assert.equal(parseArguments(["status", "--sudo"]).sudo, true);
  assert.throws(() => parseArguments(["stop", "--sudo", "--all"]), /réservé à la commande status/);
});
