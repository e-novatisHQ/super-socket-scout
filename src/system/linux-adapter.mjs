import { readFile, readlink, stat } from "node:fs/promises";
import path from "node:path";
import { run as defaultRun } from "./command.mjs";
import { identifyService, parseDockerContainers } from "./service-identification.mjs";

const SYSTEM_COMMANDS = new Set([
  "systemd", "systemd-resolve", "sshd", "cupsd", "avahi-daemon",
  "NetworkManager", "rootlesskit", "containerd", "dockerd", "kdeconnectd",
]);

function splitAddress(value) {
  const bracketed = value.match(/^\[(.*)\]:(\d+)$/);
  if (bracketed) return { address: bracketed[1], port: Number(bracketed[2]) };
  const index = value.lastIndexOf(":");
  return { address: value.slice(0, index), port: Number(value.slice(index + 1)) };
}

export function parseSs(text) {
  const sockets = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const columns = trimmed.split(/\s+/);
    const protocol = columns[0];
    const state = columns[1];
    if (!(["tcp", "udp"].includes(protocol))) continue;
    if (protocol === "tcp" && state !== "LISTEN") continue;
    if (protocol === "udp" && state !== "UNCONN") continue;
    const local = splitAddress(columns[4]);
    if (!Number.isInteger(local.port)) continue;
    const processText = columns.slice(6).join(" ");
    const processMatch = processText.match(/\(\("([^"]+)",pid=(\d+)/);
    sockets.push({
      protocol,
      address: local.address,
      port: local.port,
      pid: processMatch ? Number(processMatch[2]) : null,
      processName: processMatch?.[1] ?? null,
    });
  }
  return sockets;
}

function isExposed(address) {
  return !["127.0.0.1", "127.0.0.53%lo", "127.0.0.54", "::1"].includes(address);
}

async function safeRead(file, transform = (value) => value) {
  try { return transform(await readFile(file, "utf8")); } catch { return null; }
}

async function safeReadlink(file) {
  try { return await readlink(file); } catch { return null; }
}

const PRIVILEGED_FILES = new Set(["cmdline", "status", "stat", "cgroup"]);

async function processRead(pid, file, runner, privileged, transform = (value) => value) {
  if (!privileged) return safeRead(`/proc/${pid}/${file}`, transform);
  if (!Number.isInteger(pid) || pid <= 0 || !PRIVILEGED_FILES.has(file)) return null;
  const result = await runner("/usr/bin/sudo", ["-n", "/usr/bin/cat", `/proc/${pid}/${file}`]);
  return result.ok ? transform(result.stdout) : null;
}

async function processReadlink(pid, runner, privileged) {
  if (!privileged) return safeReadlink(`/proc/${pid}/cwd`);
  if (!Number.isInteger(pid) || pid <= 0) return null;
  const result = await runner("/usr/bin/sudo", ["-n", "/usr/bin/readlink", `/proc/${pid}/cwd`]);
  return result.ok ? result.stdout.trim() : null;
}

async function findGitRoot(cwd) {
  if (!cwd || cwd.endsWith(" (deleted)")) return null;
  let current = cwd;
  while (current !== path.dirname(current)) {
    try {
      await stat(path.join(current, ".git"));
      return current;
    } catch { current = path.dirname(current); }
  }
  return null;
}

async function gitMetadata(root, runner) {
  if (!root) return { project: null, branch: null, worktree: false };
  const [top, common, branch] = await Promise.all([
    runner("git", ["-C", root, "rev-parse", "--show-toplevel"]),
    runner("git", ["-C", root, "rev-parse", "--git-common-dir"]),
    runner("git", ["-C", root, "branch", "--show-current"]),
  ]);
  const checkout = top.ok ? top.stdout.trim() : root;
  const commonPath = common.ok
    ? path.resolve(root, common.stdout.trim())
    : path.join(root, ".git");
  const worktree = common.ok && path.resolve(commonPath) !== path.join(checkout, ".git");
  return {
    project: worktree ? path.dirname(commonPath) : checkout,
    branch: branch.ok ? branch.stdout.trim() || "HEAD détachée" : null,
    worktree,
  };
}

async function processMetadata(pid, runner, { privileged = false } = {}) {
  if (!pid) return {};
  const [cwdRaw, cmdline, status, statLine, cgroup] = await Promise.all([
    processReadlink(pid, runner, privileged),
    processRead(pid, "cmdline", runner, privileged, (value) => value.replaceAll("\0", " ").trim()),
    processRead(pid, "status", runner, privileged),
    processRead(pid, "stat", runner, privileged),
    processRead(pid, "cgroup", runner, privileged),
  ]);
  const cwdDeleted = cwdRaw?.endsWith(" (deleted)") ?? false;
  const cwd = cwdRaw?.replace(/ \(deleted\)$/, "") ?? null;
  const uid = Number(status?.match(/^Uid:\s+(\d+)/m)?.[1]);
  const ppid = Number(status?.match(/^PPid:\s+(\d+)/m)?.[1]);
  const command = cmdline || null;
  const executable = command?.split(" ")[0]?.split("/").at(-1) ?? null;
  const gitRoot = await findGitRoot(cwdRaw);
  const git = await gitMetadata(gitRoot, runner);
  const codexPath = cwd?.includes("/.codex/worktrees/") ?? false;
  const orphan = cwdDeleted || (codexPath && !gitRoot);
  const statFields = statLine ? statLine.slice(statLine.lastIndexOf(")") + 2).split(" ") : [];
  return {
    cwd, command, uid: Number.isInteger(uid) ? uid : null,
    ppid: Number.isInteger(ppid) ? ppid : null, cgroup,
    startTime: statFields[19] ?? null,
    system: uid === 0 || SYSTEM_COMMANDS.has(executable),
    orphan,
    ...git,
    worktree: git.worktree || codexPath,
  };
}

const LAUNCHER_NAMES = new Set(["node", "npm", "npx", "pnpm", "yarn", "bun", "sh", "bash", "tsx"]);

async function findStopRoot(target, runner) {
  let root = { pid: target.pid, ...(await processMetadata(target.pid, runner)) };
  for (let depth = 0; depth < 8 && root.ppid > 1; depth += 1) {
    const parent = { pid: root.ppid, ...(await processMetadata(root.ppid, runner)) };
    const name = parent.command?.split(" ")[0]?.split("/").at(-1);
    if (!parent.cwd || parent.cwd !== root.cwd || parent.uid !== root.uid || !LAUNCHER_NAMES.has(name)) break;
    root = parent;
  }
  return root;
}

async function descendants(pid, runner) {
  const result = await runner("ps", ["-eo", "pid=,ppid="]);
  if (!result.ok) return [];
  const byParent = new Map();
  for (const line of result.stdout.trim().split("\n")) {
    const [child, parent] = line.trim().split(/\s+/).map(Number);
    if (!byParent.has(parent)) byParent.set(parent, []);
    byParent.get(parent).push(child);
  }
  const found = [];
  const visit = (parent) => {
    for (const child of byParent.get(parent) ?? []) { visit(child); found.push(child); }
  };
  visit(pid);
  return found;
}

function stableId(socket) {
  return `${socket.protocol}:${socket.address}:${socket.port}:${socket.pid ?? "unknown"}`;
}

export function createLinuxAdapter({ runner = defaultRun } = {}) {
  let privileged = false;
  return {
    isPrivileged() { return privileged; },

    async enablePrivilege() {
      const result = await runner("/usr/bin/sudo", ["-v"], { timeout: 120_000 });
      if (!result.ok) {
        privileged = false;
        return { ok: false, message: result.stderr.trim() || "Authentification sudo refusée" };
      }
      privileged = true;
      return { ok: true, message: "Identification élevée activée pour cette session" };
    },

    disablePrivilege() {
      privileged = false;
      return { ok: true, message: "Identification standard rétablie" };
    },

    async discover() {
      const [result, dockerResult] = await Promise.all([
        privileged
          ? runner("/usr/bin/sudo", ["-n", "/usr/bin/ss", "-H", "-lntup"])
          : runner("ss", ["-H", "-lntup"]),
        runner("docker", ["ps", "--format", "{{json .}}"]),
      ]);
      if (!result.ok) throw new Error(`Impossible d'interroger les sockets : ${result.stderr.trim()}`);
      const parsed = parseSs(result.stdout).filter((socket) =>
        socket.protocol === "tcp"
        || ["0.0.0.0", "::", "*"].includes(socket.address)
        || socket.port < 1024,
      );
      const raw = [...new Map(parsed.map((socket) => [stableId(socket), socket])).values()];
      const metadata = new Map();
      await Promise.all([...new Set(raw.map((item) => item.pid).filter(Boolean))].map(async (pid) => {
        metadata.set(pid, await processMetadata(pid, runner, { privileged }));
      }));
      const enriched = raw.map((socket) => {
        const process = metadata.get(socket.pid) ?? {};
        return {
          ...socket, ...process,
          id: stableId(socket),
          label: `${socket.protocol.toUpperCase()} ${socket.address}:${socket.port}`,
          exposed: isExposed(socket.address),
          system: socket.pid ? process.system : true,
          status: socket.pid ? "available" : "disabled",
          elevated: privileged,
          disabledReason: socket.pid ? null : privileged
            ? "PID non visible même avec l'identification élevée"
            : "PID non visible avec les permissions actuelles",
        };
      });
      const grouped = new Map();
      for (const item of enriched) {
        const key = item.pid
          ? `process:${item.pid}:${item.startTime ?? "unknown"}`
          : `unknown:${item.protocol}:${item.port}`;
        if (!grouped.has(key)) {
          grouped.set(key, { ...item, id: key, endpoints: [] });
        }
        const server = grouped.get(key);
        server.endpoints.push({ protocol: item.protocol, address: item.address, port: item.port, exposed: item.exposed });
        server.exposed ||= item.exposed;
      }
      for (const server of grouped.values()) {
        server.endpoints.sort((a, b) => Number(b.exposed) - Number(a.exposed) || a.port - b.port);
        const primary = server.endpoints[0];
        server.protocol = primary.protocol;
        server.address = primary.address;
        server.port = primary.port;
        server.label = `${primary.protocol.toUpperCase()} ${primary.address}:${primary.port}${server.endpoints.length > 1 ? ` (+${server.endpoints.length - 1})` : ""}`;
        Object.assign(server, identifyService(server, dockerResult.ok ? parseDockerContainers(dockerResult.stdout) : []));
      }
      return [...grouped.values()].sort((a, b) => Number(b.exposed) - Number(a.exposed) || a.port - b.port);
    },

    async stop(target, { signal = "SIGTERM" } = {}) {
      const current = await processMetadata(target.pid, runner, { privileged: false });
      if (!current.startTime || current.startTime !== target.startTime) {
        return { id: target.id, status: "conflict", message: "Le PID a changé depuis l'inventaire" };
      }
      try {
        const root = await findStopRoot(target, runner);
        const pids = [...await descendants(root.pid, runner), root.pid];
        for (const pid of pids) {
          try { process.kill(pid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
        }
        return {
          id: target.id, status: "success",
          message: `${signal} envoyé à l'arbre ${root.pid} (${pids.length} processus)`,
        };
      } catch (error) {
        return { id: target.id, status: "failed", message: error.message };
      }
    },
  };
}
