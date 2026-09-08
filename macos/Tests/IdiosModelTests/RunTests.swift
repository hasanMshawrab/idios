import Testing

@testable import IdiosModel

private let t1 = "2026-08-27T14:39:00.000000Z"
private let t2 = "2026-08-27T14:37:00.000000Z"
private let t3 = "2026-08-27T14:35:00.000000Z"

/// RunLine is every field a run row draws, compared as one value.
struct RunLine: Hashable {
    var id: String
    var suffix: String?
    var leadID: String
    var podCount: Int
    var categories: [Category]
    var reasons: [String]
    var exitCodes: [Int32]
    var openCount: Int
    var podSuffixes: [String]
}

private func lines(_ folds: [RunFold]) -> [RunLine] {
    folds.map {
        RunLine(
            id: $0.id, suffix: $0.suffix, leadID: $0.lead.id, podCount: $0.podCount,
            categories: $0.categories, reasons: $0.reasons, exitCodes: $0.exitCodes,
            openCount: $0.openCount, podSuffixes: $0.podSuffixes)
    }
}

private func jobRow(_ id: String, job: String, lastSeen: String) -> Incident {
    row(
        id, pod: nil, category: .jobFailed, lastSeen: lastSeen, container: nil,
        workload: "report", lastReason: "BackoffLimitExceeded", exitCode: nil, job: job,
        podName: nil, kind: "CronJob")
}

private func podRow(
    _ id: String, pod: String, job: String, name: String, closed: Bool = false, lastSeen: String
) -> Incident {
    row(
        id, pod: pod, category: .crash, closed: closed, lastSeen: lastSeen, container: "api",
        workload: "report", lastReason: "Error", exitCode: 1, job: job, podName: name,
        kind: "CronJob")
}

// Each case is its own typed constant: the type checker times out inferring
// several inline tuples of nested Incident literals at once.

private let noRows: ([Incident], [RunLine]) = ([], [])

// A Job whose pods were all pruned keeps only its job_failed row, so the run
// has no pod to read a suffix or a name from.
private let jobRowAlone: ([Incident], [RunLine]) = (
    [jobRow("1", job: "j1", lastSeen: t1)],
    [
        RunLine(
            id: "run/j1", suffix: nil, leadID: "1", podCount: 0, categories: [.jobFailed],
            reasons: ["BackoffLimitExceeded"], exitCodes: [], openCount: 1, podSuffixes: [])
    ]
)

private let jobRowAndTwoPods: ([Incident], [RunLine]) = (
    [
        jobRow("1", job: "j1", lastSeen: t1),
        podRow("2", pod: "p1", job: "j1", name: "report-29807159-5dtpb", lastSeen: t2),
        podRow(
            "3", pod: "p2", job: "j1", name: "report-29807159-s966s", closed: true, lastSeen: t3),
    ],
    [
        RunLine(
            id: "run/j1", suffix: "29807159", leadID: "2", podCount: 2,
            categories: [.crash, .jobFailed], reasons: ["BackoffLimitExceeded", "Error"],
            exitCodes: [1], openCount: 2, podSuffixes: ["5dtpb", "s966s"])
    ]
)

private let twoRunsInOrder: ([Incident], [RunLine]) = (
    [
        podRow("1", pod: "p1", job: "j1", name: "report-29807159-5dtpb", lastSeen: t1),
        podRow("2", pod: "p2", job: "j2", name: "report-29807158-9xkq2", lastSeen: t2),
    ],
    [
        RunLine(
            id: "run/j1", suffix: "29807159", leadID: "1", podCount: 1, categories: [.crash],
            reasons: ["Error"], exitCodes: [1], openCount: 1, podSuffixes: ["5dtpb"]),
        RunLine(
            id: "run/j2", suffix: "29807158", leadID: "2", podCount: 1, categories: [.crash],
            reasons: ["Error"], exitCodes: [1], openCount: 1, podSuffixes: ["9xkq2"]),
    ]
)

// Two runs served interleaved: a run takes the place of its first served row,
// so the older run leads when its row came first.
private let twoRunsInterleaved: ([Incident], [RunLine]) = (
    [
        podRow("1", pod: "p2", job: "j2", name: "report-29807158-9xkq2", lastSeen: t2),
        podRow("2", pod: "p1", job: "j1", name: "report-29807159-5dtpb", lastSeen: t1),
        jobRow("3", job: "j2", lastSeen: t3),
    ],
    [
        RunLine(
            id: "run/j2", suffix: "29807158", leadID: "1", podCount: 1,
            categories: [.crash, .jobFailed], reasons: ["Error", "BackoffLimitExceeded"],
            exitCodes: [1], openCount: 2, podSuffixes: ["9xkq2"]),
        RunLine(
            id: "run/j1", suffix: "29807159", leadID: "2", podCount: 1, categories: [.crash],
            reasons: ["Error"], exitCodes: [1], openCount: 1, podSuffixes: ["5dtpb"]),
    ]
)

@Test(
    arguments: [
        noRows,
        jobRowAlone,
        jobRowAndTwoPods,
        twoRunsInOrder,
        twoRunsInterleaved,
    ])
func aJobsIncidentsFoldIntoOneRun(rows: [Incident], want: [RunLine]) {
    #expect(lines(runFolds(rows)) == want)
}

@Test(
    arguments: [
        ("report-29807159-5dtpb", "report", "CronJob", "29807159"),
        // No run segment: the name carries the pod's hash alone.
        ("report-5dtpb", "report", "CronJob", nil),
        // Another workload's pod: the prefix does not match.
        ("nightly-29807159-5dtpb", "report", "CronJob", nil),
        // A Job is its own run, so its pod names no run segment.
        ("retry-bmrcw", "retry", "Job", nil),
        ("", "report", "CronJob", nil),
    ] as [(String, String, String, String?)])
func theRunSuffixIsTheSegmentBetweenWorkloadAndHash(
    name: String, workload: String, kind: String, want: String?
) {
    #expect(runSuffix(podName: name, workloadName: workload, workloadKind: kind) == want)
}

private let cronJobRow = jobRow("1", job: "j1", lastSeen: t1)
private let cronPodRow = podRow(
    "2", pod: "p1", job: "j1", name: "report-29807159-5dtpb", lastSeen: t2)
private let deploymentRow = row("3", pod: "p9", category: .oom, lastSeen: t3, job: nil)

private let runAndFold: ([Incident], [GroupEntry]) = (
    [cronJobRow, cronPodRow, deploymentRow],
    [
        .run(
            RunFold(
                id: "run/j1", jobUID: "j1", suffix: "29807159", lead: cronPodRow,
                rows: [cronJobRow, cronPodRow])),
        .fold(PodFold(id: "3", podUID: "p9", lead: deploymentRow, siblings: [])),
    ]
)

// A CronJob's pod row with no job uid names no run, so it keeps its pod fold.
private let cronPodWithoutJobUID: ([Incident], [GroupEntry]) = (
    [
        row(
            "1", pod: "p1", category: .crash, lastSeen: t1, workload: "report", job: nil,
            podName: "report-29807159-5dtpb", kind: "CronJob")
    ],
    [
        .fold(
            PodFold(
                id: "1", podUID: "p1",
                lead: row(
                    "1", pod: "p1", category: .crash, lastSeen: t1, workload: "report", job: nil,
                    podName: "report-29807159-5dtpb", kind: "CronJob"),
                siblings: []))
    ]
)

// Three pods of one Deployment on one key still roll up: the run fold takes
// nothing away from the other kinds.
private let threeDeploymentPods: ([Incident], [GroupEntry]) = (
    [
        row("1", pod: "p1", category: .crash, lastSeen: t1),
        row("2", pod: "p2", category: .crash, lastSeen: t2),
        row("3", pod: "p3", category: .crash, lastSeen: t3),
    ],
    [
        .rollup(
            Rollup(
                id: "g/rollup/1/idios-smoke/Deployment/checkout-api/api/crash",
                containerName: "api", category: .crash,
                folds: [
                    PodFold(
                        id: "1", podUID: "p1",
                        lead: row("1", pod: "p1", category: .crash, lastSeen: t1), siblings: []),
                    PodFold(
                        id: "2", podUID: "p2",
                        lead: row("2", pod: "p2", category: .crash, lastSeen: t2), siblings: []),
                    PodFold(
                        id: "3", podUID: "p3",
                        lead: row("3", pod: "p3", category: .crash, lastSeen: t3), siblings: []),
                ]))
    ]
)

@Test(
    arguments: [
        runAndFold,
        cronPodWithoutJobUID,
        threeDeploymentPods,
    ])
func aJobsRowsBecomeRunsAndTheRestKeepTheirFold(rows: [Incident], want: [GroupEntry]) {
    #expect(groupEntries(rows, groupID: "g") == want)
}

private func runEntries(_ count: Int, fold: Bool) -> [GroupEntry] {
    let rows = (1...max(count, 1)).prefix(count).map {
        podRow(
            "\($0)", pod: "p\($0)", job: "j\($0)", name: "report-2980715\($0)-5dtpb",
            lastSeen: t1)
    }
    let runs = runFolds(Array(rows)).map { GroupEntry.run($0) }
    guard fold else { return runs }
    return runs + [.fold(PodFold(id: "d", podUID: "p9", lead: deploymentRow, siblings: []))]
}

@Test(
    arguments: [
        (0, false, ([], 0)),
        (3, false, (["run/j1", "run/j2", "run/j3"], 0)),
        (5, false, (["run/j1", "run/j2", "run/j3", "run/j4", "run/j5"], 0)),
        (6, false, (["run/j1", "run/j2", "run/j3", "run/j4", "run/j5"], 1)),
        (8, true, (["run/j1", "run/j2", "run/j3", "run/j4", "run/j5", "d"], 3)),
    ] as [(Int, Bool, ([String], Int))])
func theNewestFiveRunsShowAndTheRestAreCounted(
    count: Int, fold: Bool, want: ([String], Int)
) {
    let cut = shownRuns(runEntries(count, fold: fold), limit: 5)
    #expect((cut.shown.map(\.id), cut.more) == want)
}
