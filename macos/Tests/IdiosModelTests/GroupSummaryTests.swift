import Testing

@testable import IdiosModel

private let t1 = "2026-08-27T14:39:00.000000Z"
private let t2 = "2026-08-27T14:37:00.000000Z"
private let t3 = "2026-08-27T14:35:00.000000Z"

private let o1 = "2026-08-27T07:32:00.000000Z"
private let o2 = "2026-08-27T07:34:00.000000Z"
private let o3 = "2026-08-27T07:36:00.000000Z"

private func runRow(_ id: String, job: String, opened: String) -> Incident {
    row(
        id, pod: nil, category: .jobFailed, lastSeen: t1, container: nil, workload: "report",
        lastReason: "BackoffLimitExceeded", exitCode: nil, job: job, podName: nil,
        kind: "CronJob", opened: opened)
}

private func podRow(
    _ id: String, pod: String, category: Category, closed: Bool = false, tag: String?
) -> Incident {
    row(
        id, pod: pod, category: category, closed: closed, lastSeen: t1, job: nil,
        podName: "checkout-api-7d9f8b6c4-\(pod)", imageTag: tag)
}

// Each case is its own typed constant: the type checker times out inferring
// several inline tuples of nested Incident literals at once.

private let noRows: (String, [Incident], GroupFacts?, String) = ("Deployment", [], nil, "")

private let threeRunsWithFacts: (String, [Incident], GroupFacts?, String) = (
    "CronJob",
    [
        runRow("1", job: "j1", opened: o1),
        runRow("2", job: "j2", opened: o2),
        runRow("3", job: "j3", opened: o3),
    ],
    GroupFacts(livePods: nil, runsTotal: 63, runsFailed: 62),
    "every 2m, 62 of 63 runs failed, since 07:32"
)

// Two runs are a gap, not yet a rhythm, and with no jobs page the header
// counts the runs it can see.
private let twoRunsNoFacts: (String, [Incident], GroupFacts?, String) = (
    "CronJob",
    [runRow("1", job: "j1", opened: o1), runRow("2", job: "j2", opened: o2)],
    nil,
    "2 runs failed, since 07:32"
)

private let oneRunNoFacts: (String, [Incident], GroupFacts?, String) = (
    "CronJob",
    [runRow("1", job: "j1", opened: o1)],
    nil,
    "1 run failed, since 07:32"
)

private let threeLoopingPods: (String, [Incident], GroupFacts?, String) = (
    "Deployment",
    [
        podRow("1", pod: "p1", category: .crash, tag: "1.36"),
        podRow("2", pod: "p2", category: .crash, tag: "1.36"),
        podRow("3", pod: "p3", category: .crash, tag: "1.36"),
    ],
    GroupFacts(livePods: 3, runsTotal: nil, runsFailed: nil),
    "3 of 3 pods looping, tag 1.36"
)

private let twoOomPodsTwoTags: (String, [Incident], GroupFacts?, String) = (
    "Deployment",
    [
        podRow("1", pod: "p1", category: .oom, tag: "1.36"),
        podRow("2", pod: "p2", category: .oom, tag: "1.37"),
    ],
    nil,
    "2 pods out of memory, tags 1.36, 1.37"
)

// A pod whose image never resolved has no tag to name.
private let oneImagePullPod: (String, [Incident], GroupFacts?, String) = (
    "Deployment",
    [podRow("1", pod: "p1", category: .imagePull, tag: nil)],
    nil,
    "1 pod cannot pull the image"
)

private let threeClosedPods: (String, [Incident], GroupFacts?, String) = (
    "Deployment",
    [
        podRow("1", pod: "p1", category: .crash, closed: true, tag: "1.36"),
        podRow("2", pod: "p2", category: .crash, closed: true, tag: "1.36"),
        podRow("3", pod: "p3", category: .crash, closed: true, tag: "1.36"),
    ],
    nil,
    "3 pods, nothing open"
)

@Test(
    arguments: [
        noRows,
        threeRunsWithFacts,
        twoRunsNoFacts,
        oneRunNoFacts,
        threeLoopingPods,
        twoOomPodsTwoTags,
        oneImagePullPod,
        threeClosedPods,
    ])
func aHeaderSaysTheProblemInOneSentence(
    kind: String, rows: [Incident], facts: GroupFacts?, want: String
) {
    #expect(groupSummary(kind: kind, rows: rows, facts: facts) == want)
}

private let noBadgeRows: ([Incident], GroupBadge) = ([], GroupBadge(text: "0 closed", tone: .closed))

private let twoOpenOneClosed: ([Incident], GroupBadge) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1),
        row("2", pod: "p2", category: .crash, lastSeen: t2),
        row("3", pod: "p3", category: .crash, closed: true, lastSeen: t3),
    ],
    GroupBadge(text: "2 open", tone: .open)
)

private let twoOpenBothAcknowledged: ([Incident], GroupBadge) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1, acknowledged: true),
        row("2", pod: "p2", category: .crash, lastSeen: t2, acknowledged: true),
    ],
    GroupBadge(text: "2 open", tone: .acknowledged)
)

private let oneAcknowledgedOneNot: ([Incident], GroupBadge) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1, acknowledged: true),
        row("2", pod: "p2", category: .crash, lastSeen: t2),
    ],
    GroupBadge(text: "2 open", tone: .open)
)

private let threeClosed: ([Incident], GroupBadge) = (
    [
        row("1", pod: "p1", category: .crash, closed: true, lastSeen: t1),
        row("2", pod: "p2", category: .crash, closed: true, lastSeen: t2),
        row("3", pod: "p3", category: .crash, closed: true, lastSeen: t3),
    ],
    GroupBadge(text: "3 closed", tone: .closed)
)

@Test(
    arguments: [
        noBadgeRows,
        twoOpenOneClosed,
        twoOpenBothAcknowledged,
        oneAcknowledgedOneNot,
        threeClosed,
    ])
func theHeaderBadgeCountsOpenRowsAndTakesTheWorstTone(rows: [Incident], want: GroupBadge) {
    #expect(groupBadge(rows) == want)
}

@Test(
    arguments: [
        ([], nil),
        ([o1, o2], nil),
        ([o1, o2, o3], "2m"),
        (["2026-08-27T07:00:00.000000Z", "2026-08-27T08:00:00.000000Z",
          "2026-08-27T09:00:00.000000Z"], "1h"),
        // Gaps of 2m, 28m and 2m: the median ignores the run that was late.
        (["2026-08-27T07:00:00.000000Z", "2026-08-27T07:02:00.000000Z",
          "2026-08-27T07:30:00.000000Z", "2026-08-27T07:32:00.000000Z"], "2m"),
        ([o3, o1, o2], "2m"),
    ] as [([String], String?)])
func aRhythmNeedsThreeRunsAndReadsTheMedianGap(raw: [String], want: String?) {
    #expect(runCadence(openedAt: raw.map(Timestamp.init)) == want)
}

private let noGroups: ([[Incident]], ListSummary) = (
    [], ListSummary(problems: 0, openIncidents: 0, workloads: 0, barePods: 0, newest: nil))

private let cronGroupAndBarePod: ([[Incident]], ListSummary) = (
    [
        [
            row(
                "1", pod: nil, category: .jobFailed, lastSeen: t1, container: nil,
                workload: "report", job: "j1", podName: nil, kind: "CronJob"),
            row(
                "2", pod: "p1", category: .crash, closed: true, lastSeen: t3, workload: "report",
                job: "j1", podName: "report-29807159-5dtpb", kind: "CronJob"),
        ],
        [row("3", pod: "p9", category: .oom, lastSeen: t2, workload: "", kind: "none")],
    ],
    ListSummary(
        problems: 2, openIncidents: 2, workloads: 1, barePods: 1, newest: Timestamp(t1))
)

@Test(arguments: [noGroups, cronGroupAndBarePod])
func theSummaryLineCountsProblemsOpenRowsAndWorkloads(
    groups: [[Incident]], want: ListSummary
) {
    #expect(listSummary(groups: groups) == want)
}
