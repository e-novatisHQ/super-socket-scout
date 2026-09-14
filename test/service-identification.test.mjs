import assert from "node:assert/strict";
import test from "node:test";
import { identifyService, parseDockerContainers } from "../src/system/service-identification.mjs";

function server(overrides = {}) {
  return {
    endpoints: [{ protocol: "tcp", address: "0.0.0.0", port: 3000 }],
    command: null, processName: null, pid: null, system: false, cgroup: null,
    ...overrides,
  };
}

test("une signature de commande donne une identification certaine", () => {
  const result = identifyService(server({
    pid: 42,
    processName: "node",
    command: "node /repo/node_modules/.bin/vite dev --host 0.0.0.0",
  }));
  assert.equal(result.serviceName, "Vite");
  assert.equal(result.confidence, "certain");
  assert.match(result.evidence[0], /signature de commande/);
});

test("ADB reste le service et le projet demeure un contexte séparé", () => {
  const result = identifyService(server({
    pid: 7,
    processName: "adb",
    project: "/repo/TVconfig",
    command: "adb -L tcp:5041 fork-server server",
  }));
  assert.equal(result.serviceName, "ADB server");
  assert.equal(result.confidence, "certain");
});

test("un port connu sans PID est probable et explique sa limite", () => {
  const result = identifyService(server({ endpoints: [{ protocol: "tcp", address: "0.0.0.0", port: 22 }] }));
  assert.equal(result.serviceName, "SSH");
  assert.equal(result.confidence, "probable");
  assert.match(result.unknownReason, /PID masqué/);
});

test("Docker prime sur une heuristique de port", () => {
  const containers = parseDockerContainers('{"Names":"portainer","Image":"portainer/portainer-ce:lts","Ports":"8000/tcp, 127.0.0.1:9443->9443/tcp"}');
  const result = identifyService(server({ endpoints: [{ protocol: "tcp", address: "127.0.0.1", port: 9443 }] }), containers);
  assert.equal(result.serviceName, "portainer");
  assert.equal(result.manager, "Docker");
  assert.equal(result.confidence, "certain");
});

test("un service réellement inconnu ne prétend pas être identifié", () => {
  const result = identifyService(server({ endpoints: [{ protocol: "tcp", address: "0.0.0.0", port: 35587 }] }));
  assert.equal(result.serviceName, "Service non identifié");
  assert.equal(result.confidence, "unknown");
});
