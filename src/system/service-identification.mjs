const KNOWN_PORTS = new Map([
  ["tcp:22", { name: "SSH", runtime: "OpenSSH" }],
  ["tcp:53", { name: "DNS", runtime: "résolveur système" }],
  ["udp:53", { name: "DNS", runtime: "résolveur système" }],
  ["tcp:631", { name: "CUPS", runtime: "service d'impression" }],
  ["udp:546", { name: "Client DHCPv6", runtime: "réseau système" }],
  ["tcp:1716", { name: "KDE Connect", runtime: "service de bureau" }],
  ["udp:1716", { name: "KDE Connect", runtime: "service de bureau" }],
  ["tcp:5355", { name: "LLMNR", runtime: "résolution locale" }],
  ["udp:5355", { name: "LLMNR", runtime: "résolution locale" }],
]);

const SIGNATURES = [
  { pattern: /admin-tools-orchestrator-test.*fake-npm|fake-npm.*admin-tools-orchestrator-test/i, name: "Test orchestrateur", runtime: "Node.js", manager: "test local" },
  { pattern: /(?:^|\/)vite(?:\s|$)|vite dev/i, name: "Vite", runtime: "Node.js", manager: "serveur de développement" },
  { pattern: /next dev/i, name: "Next.js", runtime: "Node.js", manager: "serveur de développement" },
  { pattern: /svelte-kit|sveltekit/i, name: "SvelteKit", runtime: "Node.js", manager: "serveur de développement" },
  { pattern: /\badb\s+-L\s+tcp:/i, name: "ADB server", runtime: "Android Debug Bridge", manager: "processus utilisateur" },
  { pattern: /\bkdeconnectd\b/i, name: "KDE Connect", runtime: "service de bureau", manager: "session utilisateur" },
  { pattern: /(?:^|\/)sshd(?:\s|$)/i, name: "SSH", runtime: "OpenSSH", manager: "systemd" },
  { pattern: /(?:^|\/)cupsd(?:\s|$)/i, name: "CUPS", runtime: "service d'impression", manager: "systemd" },
  { pattern: /systemd-resolved/i, name: "DNS / LLMNR", runtime: "résolution système", manager: "systemd" },
  { pattern: /\brootlesskit\b.*\bdocker/i, name: "Docker rootless", runtime: "Docker", manager: "service utilisateur" },
  { pattern: /\btsx\s+watch\b/i, name: "Watcher TypeScript", runtime: "Node.js/tsx", manager: "serveur de développement" },
  { pattern: /\buvicorn\b/i, name: "Uvicorn", runtime: "Python", manager: "serveur applicatif" },
  { pattern: /manage\.py\s+runserver/i, name: "Django", runtime: "Python", manager: "serveur de développement" },
];

export function parseDockerContainers(text) {
  const containers = [];
  for (const line of text.split("\n").filter(Boolean)) {
    try {
      const value = JSON.parse(line);
      const ports = [];
      for (const match of String(value.Ports ?? "").matchAll(/(?:(?:\d+(?:\.\d+){3}|\[[^\]]+\]):)?(\d+)->(\d+)\/(tcp|udp)/g)) {
        ports.push({ hostPort: Number(match[1]), containerPort: Number(match[2]), protocol: match[3] });
      }
      containers.push({ name: value.Names, image: value.Image, ports });
    } catch { /* Une ligne Docker invalide ne doit pas casser l'inventaire réseau. */ }
  }
  return containers;
}

function systemdUnit(cgroup) {
  return cgroup?.match(/(?:^|\/)([^/]+\.service)(?:\/|$)/m)?.[1] ?? null;
}

export function identifyService(server, containers = []) {
  const evidence = [];
  const container = containers.find((candidate) => candidate.ports.some((mapping) =>
    server.endpoints.some((endpoint) => endpoint.port === mapping.hostPort && endpoint.protocol === mapping.protocol),
  ));
  if (container) {
    return {
      serviceName: container.name || "Conteneur Docker",
      runtime: container.image || "Docker",
      manager: "Docker",
      confidence: "certain",
      evidence: [`port publié par le conteneur ${container.name}`, `image ${container.image}`],
      unknownReason: null,
    };
  }

  const command = server.command ?? "";
  const signature = SIGNATURES.find((candidate) => candidate.pattern.test(command));
  if (signature) {
    evidence.push(`signature de commande : ${signature.name}`);
    return {
      serviceName: signature.name,
      runtime: signature.runtime,
      manager: signature.manager,
      confidence: "certain",
      evidence,
      unknownReason: null,
    };
  }

  const unit = systemdUnit(server.cgroup);
  if (unit) evidence.push(`cgroup systemd : ${unit}`);
  const portMatch = server.endpoints.map((endpoint) => ({
    endpoint,
    known: KNOWN_PORTS.get(`${endpoint.protocol}:${endpoint.port}`),
  })).find((candidate) => candidate.known);
  if (portMatch) {
    evidence.push(`port standard ${portMatch.endpoint.port}/${portMatch.endpoint.protocol}`);
    return {
      serviceName: portMatch.known.name,
      runtime: portMatch.known.runtime,
      manager: unit ? `systemd · ${unit}` : server.system ? "service système" : "non déterminé",
      confidence: "probable",
      evidence,
      unknownReason: server.pid ? null : "PID masqué par les permissions actuelles",
    };
  }

  if (server.processName) {
    evidence.push(`nom du processus : ${server.processName}`);
    return {
      serviceName: server.processName === "node" ? "Application Node.js" : server.processName,
      runtime: server.processName === "node" ? "Node.js" : null,
      manager: unit ? `systemd · ${unit}` : "processus direct",
      confidence: "partial",
      evidence,
      unknownReason: "aucune signature de service reconnue",
    };
  }

  return {
    serviceName: "Service non identifié",
    runtime: null,
    manager: "propriétaire masqué",
    confidence: "unknown",
    evidence: ["socket observé, PID non accessible"],
    unknownReason: "PID masqué par les permissions actuelles",
  };
}
