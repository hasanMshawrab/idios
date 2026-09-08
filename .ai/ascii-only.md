# Rule: ASCII only

Hard rule. One standing exception: the check mark (U+2713) is allowed inside
Markdown tables in docs. Generated code under `internal/apigen` is written
by sebuf, not by anyone here, and the check skips it. Anything else needs
the user's approval for a named file.

Everything you write must be printable 7-bit ASCII (0x20-0x7E) plus newline
and tab: docs, code, comments, commit messages, file names, and chat replies.

No em/en dashes, curly quotes, ellipsis chars, arrows, check marks, bullets,
box-drawing, emoji, accented letters, or math symbols. Use `-`, `--`, `"`,
`'`, `...`, `->`, `[x]`, `*`, `x`, and plain words instead.

Check before finishing an edit:

    hack/ascii-check <file>...

No arguments checks every tracked file. Any output is a violation. Do not
use `grep -P` for this: macOS grep has no `-P` and fails silently in
scripts and hooks. Convert lines you touch in existing files; do not
sweep the repo unless asked.

To bulk-convert with perl, write the characters as `\x{...}` escapes so the
script itself stays ASCII. If you paste raw non-ASCII literals into the
pattern instead, you must also pass `-Mutf8`, or they silently match nothing.

    perl -CSD -i -pe 's/\x{2014}/--/g; s/\x{2192}/->/g' <file>
