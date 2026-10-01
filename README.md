# Super Socket Scout

[![CI](https://github.com/e-novatisHQ/super-socket-scout/actions/workflows/ci.yml/badge.svg)](https://github.com/e-novatisHQ/super-socket-scout/actions/workflows/ci.yml)
[![CodeQL](https://github.com/e-novatisHQ/super-socket-scout/actions/workflows/codeql.yml/badge.svg)](https://github.com/e-novatisHQ/super-socket-scout/actions/workflows/codeql.yml)
[![Release](https://img.shields.io/github/v/release/e-novatisHQ/super-socket-scout)](https://github.com/e-novatisHQ/super-socket-scout/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Super Socket Scout** (`sss`) is a Linux terminal application for
understanding which local servers are listening, who owns them, and whether
they need attention. It maps TCP and UDP sockets to processes and, when the
kernel exposes enough context, to Git projects and worktrees. Its name is a nod
to Linux’s `ss` socket-inspection command.

The interface and detailed documentation are currently in French.

## Features

- Responsive terminal inventory with attention, project, system and full views.
- Explainable service identification from Docker mappings, command signatures,
  systemd cgroups and standard ports; confidence and evidence remain visible.
- Process, runtime, address, port, Git branch, worktree and orphan detection.
- Read-only elevated discovery to reveal otherwise hidden socket owners without
  running the application as root.
- Explicit, guarded graceful shutdown planning; system processes stay protected.
- Non-interactive `status`, `check`, `history`, `run`, `stop` and shell
  completion commands for scripts and development workflows.
- A standalone Go binary plus the original Node.js implementation and tests.

## Requirements and support

**Linux** with `/proc`, `ss`, `ps` and `git` available in `PATH`. Go 1.24 or
later is required only to build the standalone binary. Node.js 20.17 or later
is required only for the reference implementation and its tests. No macOS or
Windows support is claimed for this release.

Socket ownership depends on Linux permissions and kernel information. Some
processes may remain unattributed even with elevated discovery; an unknown
socket is never treated as a safe target for shutdown.

## Install

Download the binary for your architecture from
[GitHub Releases](https://github.com/e-novatisHQ/super-socket-scout/releases),
verify it against `SHA256SUMS`, then put `sss` on your `PATH`.

To build from a local checkout:

```sh
make build
./bin/sss
```

`make install` installs to `~/.local/bin` by default. Set `PREFIX` to choose
another prefix; this never changes shell configuration. Build release binaries
and checksums with `make release VERSION=x.y.z`.

For the Node.js reference implementation:

```sh
npm ci
npm run tui
```

## Use

Start the interactive terminal UI:

```sh
sss
```

The initial **Attention** view shows only items that warrant action or
verification. Use `←`/`→` or `1` to `4` to switch views, `/` to search, `u` to
toggle UDP, `r` to refresh, `?` for help and `q` to quit. Press Enter to open a
server’s evidence and guarded actions.

Useful non-interactive commands:

```sh
sss status
sss status --json
sss status --sudo
sss check --port 5173 --port 8000 --json
sss history --json
sss run --name storefront -- npm run dev
sss completion bash
```

`check` returns `2` when a requested port is occupied. `history` records
appearances and disappearances without storing command lines. `run` launches a
command without a shell and associates its network descendants with the given
name for its lifetime.

Stopping a process is deliberately explicit:

```sh
sss stop --server 'process:1234:987654' --yes --confirm 'STOP:1234'
```

Without `--yes`, or with an incorrect confirmation token, the command only
prints the shutdown plan and exits with code `4`. Exit codes are: `0` success,
`1` technical error, `2` port conflict, `3` invalid input, `4` confirmation
required and `130` interruption.

See the [architecture](docs/architecture.md) and the
[JSON v1 contract](docs/json-contract-v1.md) for integration details.

## Safety model

- Inventory and shutdown planning are read-only.
- A target combines socket and PID, and process start time is revalidated before
  signalling it.
- Shutdown sends `SIGTERM` only to the identified launcher and local descendants;
  it never sends `SIGKILL`.
- `sudo` is opt-in and restricted to read-only inventory. It is never reused to
  signal a process.
- System processes are excluded by default, and sockets without a verifiable PID
  stay visible but cannot be selected.
- Recognized passwords, tokens and keys are redacted before output.

## Development

```sh
make test
make build
```

`make test` runs both Go and Node.js test suites. See
[CONTRIBUTING](CONTRIBUTING.md) for the contribution workflow and the supported
validation commands.

## License

[MIT](LICENSE), © e-novatisHQ and contributors. Third-party dependencies retain
their own licenses. This project is an independent Linux workstation tool.
