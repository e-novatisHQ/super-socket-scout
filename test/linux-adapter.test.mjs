import assert from "node:assert/strict";
import test from "node:test";
import { createLinuxAdapter, parseSs } from "../src/system/linux-adapter.mjs";

test("parse les écoutes TCP et UDP avec ou sans PID", () => {
  const items = parseSs([
    'tcp LISTEN 0 511 0.0.0.0:5173 0.0.0.0:* users:(("node",pid=42,fd=21))',
    'udp UNCONN 0 0 127.0.0.1:5353 0.0.0.0:* users:(("adb",pid=7,fd=4))',
    'tcp LISTEN 0 10 [::]:7070 [::]:*',
    'tcp ESTAB 0 0 127.0.0.1:1 127.0.0.1:2',
  ].join("\n"));
  assert.deepEqual(items, [
    { protocol: "tcp", address: "0.0.0.0", port: 5173, pid: 42, processName: "node" },
    { protocol: "udp", address: "127.0.0.1", port: 5353, pid: 7, processName: "adb" },
    { protocol: "tcp", address: "::", port: 7070, pid: null, processName: null },
  ]);
});

test("l'identification élevée utilise sudo uniquement pour la collecte", async () => {
  const calls = [];
  const runner = async (command, args = []) => {
    calls.push([command, ...args]);
    if (command === "/usr/bin/sudo" && args[0] === "-v") return { ok: true, stdout: "", stderr: "" };
    if (command === "/usr/bin/sudo" && args.includes("/usr/bin/ss")) {
      return { ok: true, stdout: 'tcp LISTEN 0 10 0.0.0.0:7070 0.0.0.0:* users:(("hidden-service",pid=4242,fd=3))\n', stderr: "" };
    }
    if (command === "/usr/bin/sudo" && args.includes("/usr/bin/readlink")) return { ok: true, stdout: "/srv/hidden\n", stderr: "" };
    if (command === "/usr/bin/sudo" && args.includes("/usr/bin/cat")) {
      const file = args.at(-1);
      if (file.endsWith("/cmdline")) return { ok: true, stdout: "hidden-service\0--serve\0", stderr: "" };
      if (file.endsWith("/status")) return { ok: true, stdout: "Uid:\t0\t0\t0\t0\nPPid:\t1\n", stderr: "" };
      if (file.endsWith("/stat")) return { ok: true, stdout: `4242 (hidden-service) S 1 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 99\n`, stderr: "" };
      return { ok: true, stdout: "0::/system.slice/hidden.service\n", stderr: "" };
    }
    if (command === "docker") return { ok: false, stdout: "", stderr: "absent" };
    return { ok: false, stdout: "", stderr: "inattendu" };
  };
  const adapter = createLinuxAdapter({ runner });
  assert.equal((await adapter.enablePrivilege()).ok, true);
  const [server] = await adapter.discover();
  assert.equal(adapter.isPrivileged(), true);
  assert.equal(server.pid, 4242);
  assert.equal(server.command, "hidden-service --serve");
  assert.equal(server.system, true);
  assert.equal(server.elevated, true);
  assert.ok(calls.some((call) => call.join(" ") === "/usr/bin/sudo -n /usr/bin/ss -H -lntup"));
  assert.ok(calls.every((call) => !call.includes("kill")));
  adapter.disablePrivilege();
  assert.equal(adapter.isPrivileged(), false);
});

test("un refus sudo laisse l'identification standard active", async () => {
  const adapter = createLinuxAdapter({ runner: async () => ({ ok: false, stdout: "", stderr: "mot de passe incorrect" }) });
  const result = await adapter.enablePrivilege();
  assert.equal(result.ok, false);
  assert.equal(adapter.isPrivileged(), false);
  assert.match(result.message, /mot de passe/);
});
