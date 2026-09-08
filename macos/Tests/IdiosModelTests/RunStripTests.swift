import Testing

@testable import IdiosModel

private let opened = "2026-08-27T07:00:00.000000Z"

private func at(_ minute: String) -> String { "2026-08-27T\(minute):00.000000Z" }

/// job builds a run's Job row; the uid is the name, so an incident names its
/// run by the name the strip reads its suffix from.
private func job(
    name: String, complete: Bool, condition: String?, reason: String?, started: String?,
    finished: String?, failed: Int32
) -> Job {
    Job(
        uid: name, clusterID: "1", namespace: "idios-smoke", name: name, cronjobUID: nil,
        cronjobName: "nightly", active: 0, succeeded: 0, failed: failed, backoffLimit: 0,
        completions: 0, parallelism: 0, activeDeadlineSeconds: nil, restartPolicy: nil,
        conditionType: condition, conditionReason: reason, conditionMessage: nil,
        createdAt: Timestamp(opened), startedAt: started.map(Timestamp.init),
        finishedAt: finished.map(Timestamp.init), firstSeenAt: Timestamp(opened),
        lastSeenAt: Timestamp(opened), deletedAt: nil, complete: complete)
}

/// incident builds one row of a run, carrying only what the cell reads.
private func incident(
    id: String, jobUID: String?, podUID: String?, podName: String?, state: IncidentState
) -> Incident {
    Incident(
        id: id, clusterID: "1", namespace: "idios-smoke", subjectKind: .pod, podUID: podUID,
        jobUID: jobUID, containerName: nil, workloadKind: "CronJob", workloadName: "nightly",
        category: .crash, firstReason: "Error", lastReason: "Error", lastMessage: nil,
        image: nil, imageTag: nil, imageID: nil, occurrences: 0, openedAt: Timestamp(opened),
        lastSeenAt: Timestamp(opened),
        closedAt: state == .podDeleted ? Timestamp(at("07:59")) : nil,
        closeReason: state == .podDeleted ? .podDeleted : nil,
        acknowledgedAt: state == .acknowledged ? Timestamp(at("07:59")) : nil, dismissedAt: nil,
        note: nil, state: state, podName: podName, podDeletedAt: nil, podDeletionReason: nil,
        containerCount: 0, exitCode: nil, signal: nil, nodeName: "node-a")
}

private func cell(
    _ uid: String, name: String, outcome: RunOutcome, started: String?, finished: String? = nil,
    condition: String, reason: String? = nil, attempts: Int32 = 0, incidents: [String] = [],
    pod: String? = nil, podName: String? = nil
) -> RunCell {
    RunCell(
        id: uid, jobUID: uid, name: name, outcome: outcome, startedAt: started.map(Timestamp.init),
        finishedAt: finished.map(Timestamp.init), condition: condition, reason: reason,
        exitCode: nil, attempts: attempts, backoffLimit: 0, incidentIDs: incidents, podUID: pod,
        podName: podName)
}

// Each case is its own typed constant: the type checker times out inferring
// several inline tuples of nested literals at once.

private let noRuns: ([Job], [Incident], [RunCell]) = ([], [], [])

private let everyOutcome: ([Job], [Incident], [RunCell]) = (
    [
        job(
            name: "nightly-29807163", complete: false, condition: nil, reason: nil,
            started: at("07:40"), finished: nil, failed: 0),
        job(
            name: "nightly-29807162", complete: true, condition: "Complete", reason: nil,
            started: at("07:30"), finished: at("07:31"), failed: 0),
        job(
            name: "nightly-29807161", complete: false, condition: "Failed",
            reason: "BackoffLimitExceeded", started: at("07:20"), finished: at("07:22"),
            failed: 2),
        job(
            name: "nightly-29807160", complete: false, condition: "Failed",
            reason: "DeadlineExceeded", started: at("07:10"), finished: at("07:12"), failed: 1),
        job(
            name: "nightly-29807159", complete: false, condition: "Failed",
            reason: "BackoffLimitExceeded", started: at("07:00"), finished: at("07:02"),
            failed: 3),
    ],
    [
        incident(
            id: "i6", jobUID: "nightly-29807164", podUID: "p6",
            podName: "nightly-29807164-abcde", state: .open),
        incident(
            id: "i3", jobUID: "nightly-29807161", podUID: "p3",
            podName: "nightly-29807161-s966s", state: .podDeleted),
        incident(
            id: "i2", jobUID: "nightly-29807160", podUID: "p2",
            podName: "nightly-29807160-9xkq2", state: .acknowledged),
        incident(
            id: "i1", jobUID: "nightly-29807159", podUID: "p1",
            podName: "nightly-29807159-5dtpb", state: .open),
        // A row that names no run is not a run.
        incident(id: "i0", jobUID: nil, podUID: "p0", podName: "nightly-shell", state: .open),
    ],
    [
        cell(
            "nightly-29807159", name: "29807159", outcome: .failed(.open), started: at("07:00"),
            finished: at("07:02"), condition: "Failed", reason: "BackoffLimitExceeded",
            attempts: 3, incidents: ["i1"], pod: "p1", podName: "nightly-29807159-5dtpb"),
        cell(
            "nightly-29807160", name: "29807160", outcome: .failed(.acknowledged),
            started: at("07:10"), finished: at("07:12"), condition: "Failed",
            reason: "DeadlineExceeded", attempts: 1, incidents: ["i2"], pod: "p2",
            podName: "nightly-29807160-9xkq2"),
        cell(
            "nightly-29807161", name: "29807161", outcome: .failed(.closed), started: at("07:20"),
            finished: at("07:22"), condition: "Failed", reason: "BackoffLimitExceeded",
            attempts: 2, incidents: ["i3"], pod: "p3", podName: "nightly-29807161-s966s"),
        cell(
            "nightly-29807162", name: "29807162", outcome: .complete, started: at("07:30"),
            finished: at("07:31"), condition: "Complete"),
        cell(
            "nightly-29807163", name: "29807163", outcome: .running, started: at("07:40"),
            condition: "running"),
        cell(
            "nightly-29807164", name: "29807164", outcome: .swept, started: nil,
            condition: "not recorded", incidents: ["i6"], pod: "p6",
            podName: "nightly-29807164-abcde"),
    ]
)

@Test(arguments: [noRuns, everyOutcome])
func runCellsReadEveryRunsOutcomeOldestFirst(
    jobs: [Job], incidents: [Incident], want: [RunCell]
) {
    #expect(runCells(jobs: jobs, incidents: incidents, cronjobName: "nightly") == want)
}

private func minutes(_ placed: [(Int, RunCell)]) -> [RunCell?] {
    var row = [RunCell?](repeating: nil, count: 60)
    for (minute, cell) in placed { row[minute] = cell }
    return row
}

private let firstOfTheMinute = cell(
    "j1", name: "29807159", outcome: .complete, started: at("07:32"), condition: "Complete")
private let secondOfTheMinute = cell(
    "j2", name: "29807160", outcome: .complete, started: at("07:32"), condition: "Complete")
private let lateInTheHour = cell(
    "j3", name: "29807161", outcome: .complete, started: at("07:59"), condition: "Complete")
private let twoHoursOn = cell(
    "j4", name: "29807162", outcome: .complete, started: at("09:05"), condition: "Complete")
private let neverStarted = cell(
    "j5", name: "29807163", outcome: .swept, started: nil, condition: "not recorded")

private let noCells: ([RunCell], RunMatrix) = ([], RunMatrix(rows: []))

private let threeHours: ([RunCell], RunMatrix) = (
    [firstOfTheMinute, secondOfTheMinute, lateInTheHour, twoHoursOn, neverStarted],
    RunMatrix(rows: [
        RunMatrixRow(
            id: "2026-08-27T07:00:00Z", hour: Timestamp("2026-08-27T07:00:00Z"),
            minutes: minutes([(32, secondOfTheMinute), (59, lateInTheHour)])),
        RunMatrixRow(
            id: "2026-08-27T08:00:00Z", hour: Timestamp("2026-08-27T08:00:00Z"),
            minutes: minutes([])),
        RunMatrixRow(
            id: "2026-08-27T09:00:00Z", hour: Timestamp("2026-08-27T09:00:00Z"),
            minutes: minutes([(5, twoHoursOn)])),
    ])
)

@Test(arguments: [noCells, threeHours])
func runMatrixKeepsEveryHourAndTheMinuteEachRunStarted(cells: [RunCell], want: RunMatrix) {
    #expect(runMatrix(cells) == want)
}

private func failedRun(_ uid: String, reason: String?) -> RunCell {
    cell(uid, name: uid, outcome: .failed(.open), started: nil, condition: "Failed", reason: reason)
}

private let sameReasonRuns = (1...6).map { failedRun("j\($0)", reason: $0 == 3 ? "B" : "A") }
private let succeededBetween = [
    failedRun("j1", reason: "A"),
    cell("j2", name: "j2", outcome: .complete, started: nil, condition: "Complete", reason: "A"),
    failedRun("j3", reason: "A"),
]

private let noRows: ([RunCell], [RunTableRow]) = ([], [])

private let threeFolds: ([RunCell], [RunTableRow]) = (
    sameReasonRuns,
    [
        RunTableRow(
            id: "j6", run: sameReasonRuns[5], folded: [sameReasonRuns[4], sameReasonRuns[3]]),
        RunTableRow(id: "j3", run: sameReasonRuns[2], folded: []),
        RunTableRow(id: "j2", run: sameReasonRuns[1], folded: [sameReasonRuns[0]]),
    ]
)

// A run that completed carries no failure to share, so the failures on either
// side of it stay apart even when their reason is the same.
private let completeRunBreaksTheFold: ([RunCell], [RunTableRow]) = (
    succeededBetween,
    [
        RunTableRow(id: "j3", run: succeededBetween[2], folded: []),
        RunTableRow(id: "j2", run: succeededBetween[1], folded: []),
        RunTableRow(id: "j1", run: succeededBetween[0], folded: []),
    ]
)

@Test(arguments: [noRows, threeFolds, completeRunBreaksTheFold])
func runTableRowsFoldConsecutiveRunsWithTheSameReason(cells: [RunCell], want: [RunTableRow]) {
    #expect(runTableRows(cells) == want)
}
