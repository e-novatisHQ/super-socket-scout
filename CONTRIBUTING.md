# Contributing

Bug reports and focused pull requests are welcome. The terminal interface is
currently in French; documentation and contributions may be in English or
French.

## Development

Install Go 1.24+, Node.js 20.17+, GNU Make and the Linux tools listed in the
README. Tests use fixtures and fake command output; they do not need privileged
access to another contributor’s processes.

```sh
npm ci
make test
make build
```

Keep the two implementations coherent: `cmd/sss/` is the standalone
Go executable, while `src/` contains the Node.js reference implementation.
Changes to observable CLI behavior should include coverage in the corresponding
Go and/or Node.js tests and update the JSON contract when it changes.

Before opening a pull request:

- Add a regression test for a behavior change, including its failure path.
- Run `make test` and `make build`.
- Update user-facing documentation for behavior, safety or compatibility changes.
- Never commit credentials, tokens, private addresses, personal paths or
  unsanitized process output.
- Keep shutdown behavior conservative: never bypass confirmation, PID identity
  checks or system-process protection.

A pull request should explain the trigger, resulting behavior, validation and
known limitations. No CLA is required; contributions are submitted under MIT.
Participation follows the [code of conduct](CODE_OF_CONDUCT.md).
