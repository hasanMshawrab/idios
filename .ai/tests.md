# Rule: tests earn their place

Hard rule. A test exists because a specific behaviour would otherwise break
unnoticed. Volume is not a goal; count of tests is never reported as
progress.

1. Every test traces to one of: a scenario listed in the design docs, a
   rule stated there ("the one exception", "seen in the wild", "never"),
   or a bug that was actually hit. If you cannot name the source, do not
   write the test.
2. One test per behaviour. Variants of the same behaviour are rows in a
   table-driven test, not separate functions.
3. Assert the exact result. Compare whole structs or whole row sets
   (`cmp.Diff`, or a typed struct equality), not "not nil", "len > 0", or
   one field of many.
4. Edge cases come first, not last. Nil pointers from the API, empty
   slices, zero timestamps, the ambiguous reason, the counter that jumped
   by more than one, the delete with unknown final state. A happy-path
   test alone is not coverage.
5. Do not test the language or the library: that a struct holds what was
   put in it, that an interface is satisfied, that a constant equals its
   value, that `time.Format` formats.
6. Before finishing a task, apply the deletion test to every test written:
   "if this test were removed, is there a real bug that no other test
   would catch?" If the answer is no, delete it.
7. Test names say the behaviour, not the function:
   `TestRestartJumpMarksGapReconstructed`, not `TestDiffPod3`.

Violation:

    func TestNewFakeReturnsFake(t *testing.T) { ... }   // tests the constructor
    func TestFormatCase1 / Case2 / Case3                 // three functions, one table

Fix:

    func TestFormatIsFixedWidthUTC(t *testing.T) {
        cases := []struct{ in time.Time; want string }{ ... }
        ...
    }
