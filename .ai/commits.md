# Rule: commits

Hard rule. One logical change per commit. The message states what the
commit does; the diff shows how.

1. Subject: imperative, lower case, at most 72 characters, no trailing
   period, prefixed with the package or area and a colon:
   `store: add Writer.Tx`, `clock: fix Parse rejecting whole seconds`,
   `docs: add phase 2 plan`, `build: add ascii check to make lint`.
2. Body only when the subject cannot carry the why. One to three plain
   sentences, wrapped at 72. No bullet lists of files changed; git has
   that.
3. No trailers of any kind: no `Co-Authored-By`, no `Signed-off-by`, no
   `Generated with`, no tool names. Authorship is the git user config.
4. Commit at every task checkpoint once `go build ./... && go test ./...
   && make ascii` is green. Never commit red.
5. Do not mix a rename or reformat with a behaviour change.
6. `git add` named paths, never `git add -A` or `git add .`; the
   `.gitignore` is a backstop, not the filter.
7. No `--amend`, `rebase`, `reset --hard` or force push without the user
   asking for it in that session.

Violation:

    Add tests and implementation for the clock package

    - added clock.go
    - added clock_test.go

    Co-Authored-By: Claude <noreply@anthropic.com>

Fix:

    clock: add Layout, Format and Parse

Fix with a body (the why is not in the subject):

    store: create db file before opening the driver

    The driver honours umask, so the file could come out 0644. Creating it
    first fixes the mode at 0600.
