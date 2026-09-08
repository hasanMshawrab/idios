# Rule: code is the truth, docs are not referenced from it

Hard rule. Design docs and plan files describe intent at the time they were
written. They move, get renamed, get deleted. Code must stand on its own.

1. Never reference a document, section, or plan from code, comments, test
   names, or error messages: no `// see storage doc 6.1`, no
   `// process doc Section 14`, no `// plan Task 7`.
2. Write the reason itself, inline, in the words needed and no more. The
   doc may have a paragraph; the code gets the one sentence that matters.
3. When code and a doc disagree, the code is what runs. Fix the code if it
   is wrong; otherwise fix or delete the doc. Never leave both standing.
4. Plan files under `docs/plans/` are build scaffolding. They are not
   updated after their phase ships and are never read to understand the
   running system.
5. Design docs may cite code (a package or function name) when useful;
   the dependency runs one way only.
6. Every file states what is, not what changed. No "previously", "down
   from", "removed from the first draft", "now uses". Delete the old fact
   and write the new one. The same applies to test names, log messages
   and error strings.

Violation:

    // Dead-instance index rule, see storage doc Section 4.
    idx := rc - 1

Fix:

    // restart_count labels the newest container; while the newest one is
    // running, the dead one is the previous index.
    idx := rc - 1
