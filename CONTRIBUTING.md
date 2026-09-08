# Contributing

idios is pre-release and developed by one person. Pull requests are by
invitation: an unsolicited one is likely to be closed without review, not
out of rudeness but because the design is still moving and unreviewed
changes cost more than they save. Open an issue first and say what you
want to change.

Bug reports and questions are welcome as issues. A security report goes
through the private channel in [SECURITY.md](SECURITY.md), never an issue.

## If you were invited

Branches are named `feat/`, `fix/`, `docs/`, `chore/`, `refactor/` or
`m<N>/`, followed by a short topic: `fix/watcher-reconnect`.

Commit subjects are `area: imperative subject`, where the area is the
package or surface the change touches (`store:`, `app:`, `docs:`). The
body explains why, never what. No trailers and no generated attribution.

Every commit must be green. Before pushing:

    go build ./... && go test ./... && make ascii

Changes under `api/proto` also need `make generate-check`, and changes
under `macos/` need `make app-test && make app`.

Everything written in this repository is ASCII. The rules the code is held
to live in `.ai/`, and the design documents in `docs/design/` say what the
system is meant to do; read the relevant one before changing behaviour.

main takes no direct pushes. Every change arrives as a pull request, is
squashed on merge, and is merged by the maintainer.
