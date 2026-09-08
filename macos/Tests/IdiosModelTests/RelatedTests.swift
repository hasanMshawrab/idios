import Testing

@testable import IdiosModel

private let thisPodRow = row(
    "1", pod: "pod-crash", category: .crash, lastSeen: "2026-08-27T14:39:00.000000Z")
private let jobRow = row(
    "2", pod: nil, category: .jobFailed, lastSeen: "2026-08-27T14:38:00.000000Z")
private let otherPodRow = row(
    "3", pod: "pod-retry", category: .crash, lastSeen: "2026-08-27T14:37:00.000000Z")

private let relatedCases: [([Incident], String?, [Incident])] = [
    ([], "pod-crash", []),
    ([thisPodRow], "pod-crash", []),
    ([thisPodRow, jobRow, otherPodRow], "pod-crash", [jobRow, otherPodRow]),
    ([thisPodRow, jobRow, otherPodRow], nil, [thisPodRow, jobRow, otherPodRow]),
]

// The Related list is the Job's own row and the retries that ran in other
// pods; this pod's own incidents are already the cards beside it.
@Test(arguments: relatedCases)
func relatedRowsAreTheJobRowAndTheRetriesInOtherPods(
    rows: [Incident], podUID: String?, want: [Incident]
) {
    #expect(relatedElsewhere(rows, podUID: podUID) == want)
}

private let nodeCases: [(String?, String, Bool, String)] = [
    (nil, "", false, "not scheduled"),
    (nil, "node-a", false, "node-a (at open; the pod is no longer placed)"),
    ("node-a", "node-a", false, "node-a"),
    ("node-a", "", false, "node-a"),
    ("node-a", "node-b", false, "node-a (opened on node-b)"),
    ("node-a", "node-b", true, "was on node-a (opened on node-b)"),
]

// An incident carries the node the pod was on when it opened, which is not
// where the pod is once it has been rescheduled.
@Test(arguments: nodeCases)
func nodeLineSaysWhereThePodIsAndWhereItOpened(
    podNode: String?, incidentNode: String, deleted: Bool, want: String
) {
    #expect(nodeLine(podNode: podNode, incidentNode: incidentNode, deleted: deleted) == want)
}
