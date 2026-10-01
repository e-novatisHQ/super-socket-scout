# Security policy

Security fixes are provided for the latest release on Linux.

Please report suspected vulnerabilities privately with the repository's
**Security → Report a vulnerability** feature. Do not open a public issue that
includes credentials, tokens, private addresses, personal paths, process output
from a private network or an exploit. If private reporting is unavailable,
contact e-novatisHQ privately through its GitHub profile before sharing details.
No response-time SLA is currently offered.

The application treats inventory as read-only. Elevated discovery is opt-in,
uses only the files needed to identify sockets, and is never reused to send a
signal. Shutdown requires an explicit confirmation token, verifies process
identity and only sends `SIGTERM` to a known local process tree. System
processes are protected by default.

Command lines can contain secrets, so recognized secret values are redacted
before display; nevertheless, do not put secrets in command-line arguments.
GitHub workflows use pinned actions, read-only default permissions and do not
run privileged code from fork pull requests.
