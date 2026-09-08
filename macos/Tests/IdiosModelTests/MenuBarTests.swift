import Testing

@testable import IdiosModel

@Test(
    arguments: [
        (0, true, Int32?(86400), String?(nil)),
        (2, true, Int32?(86400), String?("2 more problems closed in the last 24 h")),
        (1, true, Int32?(86400), String?("1 more problem closed in the last 24 h")),
        (2, true, Int32?(nil), String?("2 more problems closed")),
        (3, false, Int32?(86400), String?("3 more problems")),
        (2, true, Int32?(600), String?("2 more problems closed in the last 10 min")),
    ])
func menuBarTailCountsTheProblemsItCouldNotDraw(
    hidden: Int, allClosed: Bool, windowSeconds: Int32?, want: String?
) {
    #expect(menuBarTail(hidden: hidden, allClosed: allClosed, windowSeconds: windowSeconds) == want)
}
