# Rule: comments say why, never what

Hard rule. The code says what it does. A comment exists only to carry
information the code cannot: why this way, what external fact forces it, or
what was rejected.

1. Allowed: the reason for a non-obvious choice; a fact from outside the
   code (kubelet behaviour, an SQLite quirk, an API field that lies); a
   rejected alternative and why; a known limitation.
2. Exported identifiers get the one-line doc comment Go tooling expects
   (`// Format renders t in Layout.`). Nothing more unless rule 1 applies.
3. Forbidden: restating the next line; section banners; "// now we ...";
   listing a struct's fields in prose; "implements X" on a method whose
   receiver obviously implements X; commenting a constant with its value.
4. Deletion test: if removing the comment loses nothing a competent Go
   reader would not get from the code in ten seconds, remove it.
5. No `TODO` without an owner and a reason. Prefer doing it or writing it
   down in the plan.

Violation:

    // Lock the mutex.
    w.mu.Lock()
    // Now implements Clock.
    func (Real) Now() time.Time { return time.Now() }

Allowed:

    // busy_timeout first: journal_mode fails on a briefly locked file.
    var basePragmas = []string{"busy_timeout(5000)", "journal_mode(WAL)", ...}

    // Killing fires on every rollout; a healthy pod's last lines are not
    // evidence, so the early capture is dropped unless an incident exists.
