# Rule: build the task, nothing around it

Hard rule. Implement what the task asks. Do not add what a future task
might want.

1. Add only what the current task names or what its test needs. A stub,
   interface, or empty package the plan explicitly calls for is in scope,
   even with one or zero implementations today.
2. Do not add what no task names because a later one "will probably want
   it". If you believe a seam is missing from the plan, say so in the
   report; do not build it.
3. No wrappers around standard library or well-known packages unless the
   wrapper removes real duplication that exists now.
4. Do not refactor code outside the task. If something nearby is wrong,
   say so in the report; do not touch it.
5. Prefer the smaller change that passes the test. Delete code the task
   made unused.
6. Do not generalise error handling, logging, or retries beyond what the
   task specifies.

Violation (nothing in the task named these):

    type Formatter interface { Format(time.Time) string }   // one implementation
    func NewWriterWithOptions(opts ...Option) *Writer      // no options exist

In scope (the plan named them as seams for a later task):

    type Writer struct{ db *sql.DB; mu sync.Mutex }        // Tx arrives next task
    type CaptureSink interface{ Enqueue(CaptureRequest) }  // pool arrives next phase
