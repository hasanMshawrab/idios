import Testing

@testable import IdiosModel

private func counts(_ byCategory: [Category: Int32]) -> IncidentCounts {
    IncidentCounts(byState: [:], byCategory: byCategory)
}

@Test(
    arguments: [
        ([:], Int32(0)),
        ([IncidentState.open: Int32(16)], Int32(0)),
        (
            [
                IncidentState.recovered: Int32(1), .podDeleted: 121, .jobFinished: 55,
                .manual: 2, .dismissed: 3, .open: 16, .attention: 20,
            ],
            Int32(182)
        ),
    ])
func closedIsTheSumOfEveryClosingState(byState: [IncidentState: Int32], want: Int32) {
    #expect(closedCount(IncidentCounts(byState: byState, byCategory: [:])) == want)
}

@Test(
    arguments: [
        (
            nil, nil,
            CategoryRows(
                shown: [
                    .crash, .oom, .uncleanExit, .imagePull, .config, .probe, .scheduling,
                    .stuck, .nodePressure, .rescheduled, .jobFailed, .other,
                ], hidden: 0)
        ),
        (counts([:]), nil, CategoryRows(shown: [], hidden: 12)),
        (
            counts([.crash: 10, .jobFailed: 3, .oom: 1]), nil,
            CategoryRows(shown: [.crash, .oom, .jobFailed], hidden: 9)
        ),
        (
            counts([.crash: 10, .jobFailed: 3, .oom: 1]), Category.probe,
            CategoryRows(shown: [.crash, .oom, .probe, .jobFailed], hidden: 8)
        ),
        (counts([.other: 2]), nil, CategoryRows(shown: [.other], hidden: 11)),
    ] as [(IncidentCounts?, Category?, CategoryRows)])
func categoriesWithNoIncidentFoldIntoOneRow(
    counts: IncidentCounts?, active: Category?, want: CategoryRows
) {
    #expect(categoryRows(counts: counts, active: active) == want)
}

@Test(
    arguments: [
        (IncidentState.open, "Open", "open", nil),
        (.acknowledged, "Acknowledged", "acknowledged", "a"),
        (.recovered, "Recovered", "recovered", nil),
        (.podDeleted, "Pod deleted", "pod deleted", nil),
        (.jobFinished, "Job finished", "job finished", nil),
        (.manual, "Resolved", "resolved", "r"),
        (.dismissed, "Dismissed", "dismissed", "d"),
        (.attention, "Attention", "attention", nil),
    ] as [(IncidentState, String, String, String?)])
func stateWordsReadResolvedForManual(
    state: IncidentState, title: String, label: String, key: String?
) {
    #expect(state.title == title)
    #expect(state.label == label)
    #expect(state.key == key)
}

@Test(
    arguments: [
        (CloseReason.recovered, "recovered"),
        (.podDeleted, "pod deleted"),
        (.jobFinished, "job finished"),
        (.manual, "resolved"),
    ])
func aManualCloseReadsResolved(reason: CloseReason, want: String) {
    #expect(reason.label == want)
}

@Test(
    arguments: [
        (Int32(0), "0 s"),
        (Int32(90), "90 s"),
        (Int32(600), "10 min"),
        (Int32(5400), "90 min"),
        (Int32(3600), "1 h"),
        (Int32(86400), "24 h"),
    ])
func configuredSecondsReadInTheCoarsestWholeUnit(seconds: Int32, want: String) {
    #expect(humanDuration(seconds: seconds) == want)
}
