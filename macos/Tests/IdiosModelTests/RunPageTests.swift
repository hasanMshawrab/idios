import Foundation
import Testing

@testable import IdiosModel

private let started = "2026-08-27T14:00:00.000000Z"
private let finished = "2026-08-27T14:05:00.000000Z"
private let seen = "2026-08-27T14:39:00.000000Z"

private func at(_ raw: String) -> Date {
    guard let date = Timestamp(raw).date else { fatalError("unparsable timestamp \(raw)") }
    return date
}

private func job(
    name: String = "report-28812345", cronjob: String? = "report", started: String? = nil,
    finished: String? = nil, deleted: String? = nil, conditionType: String? = nil,
    conditionReason: String? = nil, succeeded: Int32 = 0, failed: Int32 = 0,
    backoffLimit: Int32 = 1
) -> Job {
    Job(
        uid: "job-report-1", clusterID: "1", namespace: "idios-smoke", name: name,
        cronjobUID: cronjob == nil ? nil : "cronjob-report", cronjobName: cronjob, active: 0,
        succeeded: succeeded, failed: failed, backoffLimit: backoffLimit, completions: 1,
        parallelism: 1, activeDeadlineSeconds: nil, restartPolicy: "Never",
        conditionType: conditionType, conditionReason: conditionReason, conditionMessage: nil,
        createdAt: Timestamp("2026-08-27T13:59:00.000000Z"),
        startedAt: started.map(Timestamp.init), finishedAt: finished.map(Timestamp.init),
        firstSeenAt: Timestamp("2026-08-27T14:00:21.000000Z"),
        lastSeenAt: Timestamp("2026-08-27T14:00:22.000000Z"),
        deletedAt: deleted.map(Timestamp.init), complete: conditionType == "Complete")
}

private let titleCases: [(jobName: String?, cronjobName: String?, uid: String, want: String)] = [
    (nil, nil, "b3f1c2d4-5a6b-7c8d-9e0f-112233445566", "Job b3f1c2...445566"),
    ("nightly", nil, "job-nightly-1", "Job nightly"),
    ("report-28812345", "report", "job-report-1", "Run 28812345 of report"),
    ("oddly-named", "report", "job-report-2", "Run oddly-named of report"),
    ("nightly", "", "job-nightly-1", "Job nightly"),
]

// The title is "Run <suffix> of <CronJob name>" when a CronJob created the
// Job and "Job <name>" when nothing did.
@Test(arguments: titleCases)
func theRunTitleNamesTheCronJobOrTheJob(
    jobName: String?, cronjobName: String?, uid: String, want: String
) {
    #expect(runTitle(jobName: jobName, cronjobName: cronjobName, jobUID: uid) == want)
}

private let tagCases: [(job: Job?, want: String)] = [
    (nil, "NOT RECORDED"),
    (
        job(deleted: "2026-08-27T14:10:00.000000Z", conditionType: "Complete"),
        "DELETED"
    ),
    (job(), "RUNNING"),
    (job(conditionType: "Complete"), "COMPLETE"),
    (
        job(conditionType: "Failed", conditionReason: "BackoffLimitExceeded"),
        "FAILED, BACKOFF LIMIT EXCEEDED"
    ),
    (
        job(conditionType: "Failed", conditionReason: "DeadlineExceeded"),
        "FAILED, DEADLINE EXCEEDED"
    ),
    (
        job(conditionType: "Failed", conditionReason: "BackoffLimitExceeded", succeeded: 1),
        "FAILED, BACKOFF LIMIT EXCEEDED"
    ),
]

// The run's outcome is read from the condition, never from the counters: a
// Job that counted a success and then failed is a failed run.
@Test(arguments: tagCases)
func theRunTagIsTheConditionAndNeverTheCounters(job: Job?, want: String) {
    #expect(runStateTag(job) == want)
}

private func attempt(_ number: Int, exitCode: Int32? = 1) -> RunAttempt {
    RunAttempt(
        id: "\(number)", number: number, podUID: "p\(number)", podSuffix: "5dtpb",
        category: .crash, reason: "Error", exitCode: exitCode,
        openedAt: Timestamp("2026-08-27T14:01:00.000000Z"), incidentID: "\(number)",
        state: .open, outcome: nil)
}

private func wonAttempt(_ number: Int) -> RunAttempt {
    RunAttempt(
        id: "won\(number)", number: number, podUID: "p\(number)", podSuffix: "5dtpb",
        category: nil, reason: "", exitCode: nil,
        openedAt: Timestamp("2026-08-27T14:01:00.000000Z"), incidentID: nil, state: nil,
        outcome: .succeeded)
}

private func openRow(_ id: String) -> Incident {
    row(id, pod: "p\(id)", category: .crash, lastSeen: seen, workload: "report-28812345")
}

private func closedRow(_ id: String) -> Incident {
    row(
        id, pod: "p\(id)", category: .crash, closed: true, lastSeen: seen,
        workload: "report-28812345")
}

private let noCounts: (Job?, [RunAttempt], [Incident], Date, String) = (
    nil, [], [], at(seen), "0 attempts - 0 incidents, 0 open"
)

private let oneOfEach: (Job?, [RunAttempt], [Incident], Date, String) = (
    job(started: started, finished: finished), [attempt(1)], [openRow("1")], at(seen),
    "1 attempt - 1 incident, 1 open - ran 5m"
)

// Two pods that each opened an incident and a Job that then failed are two
// attempts and three incidents; the counts are never added together.
private let twoAttemptsThreeIncidents: (Job?, [RunAttempt], [Incident], Date, String) = (
    job(started: started, finished: finished),
    [attempt(1), attempt(2), giveUpLine("3", opened: seen)],
    [openRow("1"), closedRow("2"), closedRow("3")], at(seen),
    "2 attempts - 3 incidents, 1 open - ran 5m"
)

private let stillRunning: (Job?, [RunAttempt], [Incident], Date, String) = (
    job(started: started), [attempt(1)], [openRow("1")],
    at("2026-08-27T14:03:00.000000Z"), "1 attempt - 1 incident, 1 open - running for 3m"
)

private let neverStarted: (Job?, [RunAttempt], [Incident], Date, String) = (
    job(), [attempt(1)], [openRow("1")], at(seen), "1 attempt - 1 incident, 1 open"
)

// An attempt that succeeded is drawn like any other, so it is counted like
// any other: the word is attempts and not failed attempts.
private let aSucceededAttempt: (Job?, [RunAttempt], [Incident], Date, String) = (
    job(started: started, finished: finished, succeeded: 1, failed: 1),
    [attempt(1), wonAttempt(2)], [openRow("1")], at(seen),
    "2 attempts - 1 incident, 1 open - ran 5m"
)

// The counts line says attempts and incidents in separate words, and how
// long the run ran when its Job started.
@Test(
    arguments: [
        noCounts, oneOfEach, twoAttemptsThreeIncidents, stillRunning, neverStarted,
        aSucceededAttempt,
    ])
func theCountsLineSeparatesAttemptsFromIncidents(
    job: Job?, attempts: [RunAttempt], rows: [Incident], now: Date, want: String
) {
    #expect(runCountsLine(job: job, attempts: attempts, rows: rows, now: now) == want)
}

private let jobName = "report-28812345"
private let older = "2026-08-27T14:01:00.000000Z"
private let newer = "2026-08-27T14:02:00.000000Z"

private func attemptRow(_ id: String, pod: String, podName: String?, opened: String) -> Incident {
    row(
        id, pod: pod, category: .crash, lastSeen: seen, workload: jobName, podName: podName,
        kind: "Job", opened: opened)
}

private func giveUpRow(_ id: String, opened: String) -> Incident {
    row(
        id, pod: nil, category: .jobFailed, lastSeen: seen, workload: jobName,
        lastReason: "BackoffLimitExceeded", exitCode: nil, podName: nil, kind: "CronJob",
        opened: opened)
}

private func podLine(
    id: String, number: Int, pod: String, suffix: String, opened: String, incident: String
) -> RunAttempt {
    RunAttempt(
        id: id, number: number, podUID: pod, podSuffix: suffix, category: .crash,
        reason: "Error", exitCode: 137, openedAt: Timestamp(opened), incidentID: incident,
        state: .open, outcome: nil)
}

private func outcomeLine(
    _ uid: String, number: Int, suffix: String, created: String, outcome: AttemptOutcome
) -> RunAttempt {
    RunAttempt(
        id: uid, number: number, podUID: uid, podSuffix: suffix, category: nil, reason: "",
        exitCode: nil, openedAt: Timestamp(created), incidentID: nil, state: nil,
        outcome: outcome)
}

private func giveUpLine(_ id: String, opened: String) -> RunAttempt {
    RunAttempt(
        id: id, number: 0, podUID: nil, podSuffix: "", category: .jobFailed,
        reason: "BackoffLimitExceeded", exitCode: nil, openedAt: Timestamp(opened),
        incidentID: id, state: .open, outcome: nil)
}

private func attemptPod(
    _ uid: String, name: String, created: String, phase: String = "Failed",
    worstState: ContainerState? = .terminated
) -> PodRow {
    PodRow(
        uid: uid, clusterID: "1", namespace: "idios-smoke", name: name, nodeName: "node-a",
        phase: phase, statusReason: nil, statusMessage: nil, qosClass: "BestEffort",
        controllerKind: "Job", controllerName: jobName, controllerUID: "job-report-1",
        workloadKind: "CronJob", workloadName: "report", createdAt: Timestamp(created),
        startedAt: Timestamp(created), firstSeenAt: Timestamp(created),
        lastSeenAt: Timestamp(created), deletionRequestedAt: nil, deletedAt: nil,
        deletionSource: nil, deletionReason: nil, openIncidents: 0, containerCount: 1,
        worstState: worstState)
}

private typealias AttemptCase = (pods: [PodRow], rows: [Incident], want: [RunAttempt])

private let nothingKept: AttemptCase = ([], [], [])

// The phase is the only success a pod row knows, so a pod that ended
// Succeeded says so whatever its containers last reported.
private let aPodThatSucceeded: AttemptCase = (
    [attemptPod("p2", name: "\(jobName)-s966s", created: older, phase: "Succeeded")], [],
    [outcomeLine("p2", number: 1, suffix: "s966s", created: older, outcome: .succeeded)]
)

private let aPodThatFailedQuietly: AttemptCase = (
    [attemptPod("p2", name: "\(jobName)-s966s", created: older)], [],
    [
        outcomeLine(
            "p2", number: 1, suffix: "s966s", created: older, outcome: .state(.terminated))
    ]
)

private let aPodWithNoContainerKept: AttemptCase = (
    [
        attemptPod(
            "p2", name: "\(jobName)-s966s", created: older, phase: "Pending", worstState: nil)
    ], [],
    [outcomeLine("p2", number: 1, suffix: "s966s", created: older, outcome: .phase("Pending"))]
)

// A pod the sweep removed leaves its incident behind, and that attempt was
// still made: it keeps its line at the time the incident opened.
private let aSweptPodKeepsItsIncident: AttemptCase = (
    [], [attemptRow("2", pod: "p2", podName: "\(jobName)-s966s", opened: older)],
    [podLine(id: "2", number: 1, pod: "p2", suffix: "s966s", opened: older, incident: "2")]
)

// An attempt is a pod, so a pod that opened two incidents is one line and the
// first of them is the one it names.
private let onePodTwoIncidents: AttemptCase = (
    [attemptPod("p2", name: "\(jobName)-s966s", created: older)],
    [
        attemptRow("9", pod: "p2", podName: "\(jobName)-s966s", opened: newer),
        attemptRow("8", pod: "p2", podName: "\(jobName)-s966s", opened: older),
    ],
    [podLine(id: "p2", number: 1, pod: "p2", suffix: "s966s", opened: older, incident: "8")]
)

private let twoPodsOldestFirst: AttemptCase = (
    [
        attemptPod("p2", name: "\(jobName)-s966s", created: newer),
        attemptPod("p3", name: "\(jobName)-5dtpb", created: older),
    ],
    [
        attemptRow("2", pod: "p2", podName: "\(jobName)-s966s", opened: newer),
        attemptRow("3", pod: "p3", podName: "\(jobName)-5dtpb", opened: older),
    ],
    [
        podLine(id: "p3", number: 1, pod: "p3", suffix: "5dtpb", opened: older, incident: "3"),
        podLine(id: "p2", number: 2, pod: "p2", suffix: "s966s", opened: newer, incident: "2"),
    ]
)

private let aSucceededPodAmongFailures: AttemptCase = (
    [
        attemptPod("p2", name: "\(jobName)-s966s", created: newer, phase: "Succeeded"),
        attemptPod("p3", name: "\(jobName)-5dtpb", created: older),
    ],
    [attemptRow("3", pod: "p3", podName: "\(jobName)-5dtpb", opened: older)],
    [
        podLine(id: "p3", number: 1, pod: "p3", suffix: "5dtpb", opened: older, incident: "3"),
        outcomeLine("p2", number: 2, suffix: "s966s", created: newer, outcome: .succeeded),
    ]
)

private let twoPodsAndTheJob: AttemptCase = (
    [
        attemptPod("p2", name: "\(jobName)-s966s", created: newer),
        attemptPod("p3", name: "\(jobName)-5dtpb", created: older),
    ],
    [
        giveUpRow("1", opened: newer),
        attemptRow("2", pod: "p2", podName: "\(jobName)-s966s", opened: newer),
        attemptRow("3", pod: "p3", podName: "\(jobName)-5dtpb", opened: older),
    ],
    [
        podLine(id: "p3", number: 1, pod: "p3", suffix: "5dtpb", opened: older, incident: "3"),
        podLine(id: "p2", number: 2, pod: "p2", suffix: "s966s", opened: newer, incident: "2"),
        giveUpLine("1", opened: newer),
    ]
)

private let podNamedOtherwise: AttemptCase = (
    [attemptPod("p2", name: "worker-s966s", created: older)],
    [attemptRow("2", pod: "p2", podName: "worker-s966s", opened: older)],
    [
        podLine(
            id: "p2", number: 1, pod: "p2", suffix: "worker-s966s", opened: older, incident: "2")
    ]
)

private let podWithNoName: AttemptCase = (
    [], [attemptRow("2", pod: "p2", podName: nil, opened: older)],
    [podLine(id: "2", number: 1, pod: "p2", suffix: "pod unknown", opened: older, incident: "2")]
)

// One line per pod of the run, oldest first, whether it opened an incident or
// not, and the Job's own row closes the card when the Job itself failed.
@Test(
    arguments: [
        nothingKept, aPodThatSucceeded, aPodThatFailedQuietly, aPodWithNoContainerKept,
        aSweptPodKeepsItsIncident, onePodTwoIncidents, twoPodsOldestFirst,
        aSucceededPodAmongFailures, twoPodsAndTheJob, podNamedOtherwise, podWithNoName,
    ])
func attemptsAreOnePerPodOldestFirstAndTheJobRowCloses(
    pods: [PodRow], rows: [Incident], want: [RunAttempt]
) {
    #expect(runAttempts(pods: pods, rows: rows, jobName: jobName) == want)
}

private typealias VerdictCase = (
    job: Job?, attempts: [RunAttempt], capture: RunCapture, imageTag: String?,
    sameReasonRuns: Int?, want: [String]
)

private let notRecorded: VerdictCase = (
    nil, [], RunCapture(attemptsRead: 0, attemptsWithLog: 0, lastLine: nil), nil, nil,
    ["The run is not recorded; what is left is 0 attempts."]
)

private let failedRun: VerdictCase = (
    job(started: started, finished: finished, conditionType: "Failed",
        conditionReason: "BackoffLimitExceeded", failed: 2),
    [attempt(1), attempt(2)],
    RunCapture(attemptsRead: 2, attemptsWithLog: 2, lastLine: "connection refused"), nil, nil,
    [
        "Failed after 2 attempts in 5m.",
        "The backoff limit is 1; the Job counted 2 failed pods.",
        "Every attempt exits 1.",
        "Every attempt captured a log. The last line is \"connection refused\".",
    ]
)

// The attempt that succeeded is one of the lines, so the sentence counts the
// lines and the failed ones are what is left.
private let completeRun: VerdictCase = (
    job(started: started, finished: finished, conditionType: "Complete", succeeded: 1, failed: 1,
        backoffLimit: 2),
    [attempt(1, exitCode: 1), wonAttempt(2)],
    RunCapture(attemptsRead: 1, attemptsWithLog: 1, lastLine: nil), nil, nil,
    [
        "Complete after 1 failed attempt and 1 that succeeded in 5m.",
        "The backoff limit is 2; the Job counted 1 failed pods.",
        "Every failed attempt exits 1.",
        "Every attempt captured a log.",
    ]
)

// A pod pruned before idios saw it is counted by the Job and has no line, so
// the counter is kept exactly where it exceeds the lines.
private let completeWithAPrunedSuccess: VerdictCase = (
    job(started: started, finished: finished, conditionType: "Complete", succeeded: 2, failed: 0,
        backoffLimit: 2),
    [wonAttempt(1)], RunCapture(attemptsRead: 0, attemptsWithLog: 0, lastLine: nil), nil, nil,
    [
        "Complete after 0 failed attempts and 2 that succeeded in 5m.",
        "The backoff limit is 2; the Job counted 0 failed pods.",
    ]
)

private let oneAttemptCapturedNothing: VerdictCase = (
    job(started: started, conditionType: nil, failed: 1), [attempt(1), attempt(2)],
    RunCapture(attemptsRead: 2, attemptsWithLog: 1, lastLine: nil), nil, nil,
    [
        "Still going, 2 attempts since 14:00.",
        "The backoff limit is 1; the Job counted 1 failed pods.",
        "Every attempt exits 1.",
        "1 of 2 attempts captured a log.",
    ]
)

// A page that has read no attempt has not found out whether anything was
// captured, so it says nothing rather than "nothing was captured".
private let nothingRead: VerdictCase = (
    job(started: started, finished: finished, conditionType: "Failed",
        conditionReason: "BackoffLimitExceeded", failed: 1),
    [attempt(1)], RunCapture(attemptsRead: 0, attemptsWithLog: 0, lastLine: nil), nil, nil,
    [
        "Failed after 1 attempt in 5m.",
        "The backoff limit is 1; the Job counted 1 failed pods.",
        "Every attempt exits 1.",
    ]
)

private let everySentence: VerdictCase = (
    job(started: started, finished: finished, conditionType: "Failed",
        conditionReason: "BackoffLimitExceeded", failed: 2),
    [attempt(1), attempt(2, exitCode: 137)],
    RunCapture(attemptsRead: 2, attemptsWithLog: 1, lastLine: "out of memory"), "1.4.2", 18,
    [
        "Failed after 2 attempts in 5m.",
        "The backoff limit is 1; the Job counted 2 failed pods.",
        "Attempts exit 1, 137.",
        "1 of 2 attempts captured a log. The last line is \"out of memory\".",
        "Image tag 1.4.2.",
        "18 earlier runs of this CronJob failed the same way.",
    ]
)

// What idios does not know is said, not hidden: each sentence is there only
// when the page read the fields it needs.
@Test(
    arguments: [
        notRecorded, failedRun, completeRun, completeWithAPrunedSuccess,
        oneAttemptCapturedNothing, nothingRead, everySentence,
    ])
func theRunVerdictSaysOnlyWhatThePageRead(
    job: Job?, attempts: [RunAttempt], capture: RunCapture, imageTag: String?,
    sameReasonRuns: Int?, want: [String]
) {
    #expect(
        runVerdictSentences(
            job: job, attempts: attempts, capture: capture, imageTag: imageTag,
            sameReasonRuns: sameReasonRuns, now: at(seen)) == want)
}
