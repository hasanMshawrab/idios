# Security

idios is pre-release software. It runs locally, reads a kubeconfig you
point it at, and stores what it records in a local SQLite database. It
listens on `127.0.0.1` only and has no authentication, so anything with an
account on the machine can read the API.

## Reporting a vulnerability

Report privately through GitHub: open the repository's Security tab and
choose "Report a vulnerability". Do not open a public issue.

Include what an attacker gains, the steps to reproduce it, and the version
or commit you saw it on. Expect a first reply within a week. There is no
release channel yet, so a fix lands on main and ships with the next build.

## What is in scope

The daemon's HTTP API, the MCP server, the capture path, and what leaves
the machine. Pod objects reach an AI agent only after `sanitize.PodJSON`
redacts them; a way past that redaction is a vulnerability and worth
reporting.

Container logs are captured verbatim and are not redacted. Treat the data
directory as sensitive: it holds whatever your workloads logged.
