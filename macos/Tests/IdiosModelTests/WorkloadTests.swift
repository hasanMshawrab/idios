import IdiosAPI
import Testing

@testable import IdiosModel

let checkoutAPIWorkload = Workload(
    clusterID: "1",
    namespace: "idios-smoke",
    workloadKind: "Deployment",
    workloadName: "checkout-api",
    incidentsByCategory: [.crash: 3, .oom: 1],
    openIncidents: 2,
    occurrences: 7,
    imageTags: [TagCount(tag: "1.4.2", count: 3), TagCount(tag: nil, count: 1)],
    livePods: 4,
    deletedPods: 2,
    podUID: nil,
    podName: nil)

let failedReportJob = Job(
    uid: "job-report-1",
    clusterID: "1",
    namespace: "idios-smoke",
    name: "report-28812345",
    cronjobUID: "cj-report",
    cronjobName: "report",
    active: 0,
    succeeded: 0,
    failed: 3,
    backoffLimit: 2,
    completions: 1,
    parallelism: 1,
    activeDeadlineSeconds: 900,
    restartPolicy: "Never",
    conditionType: "Failed",
    conditionReason: "BackoffLimitExceeded",
    conditionMessage: "Job has reached the specified backoff limit",
    createdAt: Timestamp("2026-08-27T13:45:00.000000Z"),
    startedAt: Timestamp("2026-08-27T13:45:02.000000Z"),
    finishedAt: Timestamp("2026-08-27T13:57:00.000000Z"),
    firstSeenAt: Timestamp("2026-08-27T14:00:21.000000Z"),
    lastSeenAt: Timestamp("2026-08-27T14:00:22.000000Z"),
    deletedAt: Timestamp("2026-08-27T14:41:00.000000Z"),
    complete: false)

// Two bare pods in one namespace once collapsed into a single tree row: their
// rows share kind "none" and an empty name, and the id ignored the pod.
@Test func controllerLessRowsKeepOneIdentityPerPod() {
    func bare(uid: String, name: String) -> Workload {
        Workload(
            clusterID: "1", namespace: "idios-smoke", workloadKind: "none",
            workloadName: "", incidentsByCategory: [:], openIncidents: 0,
            occurrences: 0, imageTags: [], livePods: 1, deletedPods: 0,
            podUID: uid, podName: name)
    }
    #expect(bare(uid: "uid-a", name: "bare-a").id != bare(uid: "uid-b", name: "bare-b").id)
    #expect(checkoutAPIWorkload.id == "1/idios-smoke/Deployment/checkout-api")
}

@Test func workloadRowsCarryTheirAggregatesAndDigestPinnedTags() throws {
    let page = try Page<Workload>(
        wire: fixture(Components.Schemas.WorkloadsResponse.self, "workloads.json"))
    #expect(
        page
            == Page(
                rows: [
                    checkoutAPIWorkload,
                    Workload(
                        clusterID: "2",
                        namespace: "idios-smoke",
                        workloadKind: "ReplicaSet",
                        workloadName: "checkout-worker-6b8d9c5f7",
                        incidentsByCategory: [:],
                        openIncidents: 0,
                        occurrences: 0,
                        imageTags: [],
                        livePods: 1,
                        deletedPods: 0,
                        podUID: nil,
                        podName: nil),
                    Workload(
                        clusterID: "1",
                        namespace: "idios-smoke",
                        workloadKind: "none",
                        workloadName: "",
                        incidentsByCategory: [:],
                        openIncidents: 1,
                        occurrences: 4,
                        imageTags: [],
                        livePods: 1,
                        deletedPods: 0,
                        podUID: "pod-debug",
                        podName: "debug-shell"),
                ],
                truncated: true))
}

@Test func aWorkloadWithoutAControllerHasAnEmptyWorkloadName() throws {
    var wire = try fixture(Components.Schemas.WorkloadsResponse.self, "workloads.json")
    wire.workloads?[0].workloadKind = "none"
    wire.workloads?[0].workloadName = nil
    let workload = try Workload(wire: try #require(wire.workloads?[0]))
    #expect(workload.workloadKind == "none")
    #expect(workload.workloadName == "")
}

@Test func workloadDetailCarriesRolloutsHoursAndPods() throws {
    let detail = try WorkloadDetail(
        wire: fixture(Components.Schemas.WorkloadDetail.self, "workload_detail.json"))
    #expect(
        detail
            == WorkloadDetail(
                workload: checkoutAPIWorkload,
                rollouts: [
                    Rollout(
                        replicasetUID: "rs-checkout-1",
                        replicasetName: "checkout-api-7d9f8b6c4",
                        revision: "8",
                        images: ["registry.example.com/web:1.4.2"],
                        firstSeenAt: Timestamp("2026-08-27T13:00:00.000000Z"),
                        lastSeenAt: Timestamp("2026-08-27T14:39:02.100000Z"),
                        deletedAt: Timestamp("2026-08-27T14:40:00.000000Z"),
                        incidents: 3,
                        createdAt: Timestamp("2026-08-27T12:59:50.000000Z"),
                        replicas: 5,
                        readyReplicas: 4,
                        availableReplicas: 2),
                    Rollout(
                        replicasetUID: "rs-checkout-0",
                        replicasetName: "checkout-api-6c8e7a5b3",
                        revision: "7",
                        images: ["registry.example.com/web:1.4.2"],
                        firstSeenAt: Timestamp("2026-08-27T11:00:00.000000Z"),
                        lastSeenAt: Timestamp("2026-08-27T12:59:49.000000Z"),
                        deletedAt: Timestamp("2026-08-27T13:05:00.000000Z"),
                        incidents: 1,
                        createdAt: Timestamp("2026-08-27T10:59:40.000000Z"),
                        replicas: nil,
                        readyReplicas: nil,
                        availableReplicas: nil),
                ],
                restartsByHour: [
                    HourBucket(
                        hour: Timestamp("2026-08-27T14:00:00.000000Z"),
                        restarts: 7,
                        reconstructed: true)
                ],
                pods: [crashPodRow],
                podsTruncated: true))
}

// presentation.md: the detail's job row tells a retry that went on to
// succeed apart from a run that failed outright or one still going, and
// only the condition says so.
@Test func runOutcomeLabelReadsTheJobsCondition() {
    func job(complete: Bool, conditionType: String?) -> Job {
        Job(
            uid: "job-x", clusterID: "1", namespace: "idios-smoke", name: "report-1",
            cronjobUID: nil, cronjobName: nil, active: 0, succeeded: 0, failed: 1,
            backoffLimit: 2, completions: 1, parallelism: 1, activeDeadlineSeconds: nil,
            restartPolicy: "Never", conditionType: conditionType, conditionReason: nil,
            conditionMessage: nil, createdAt: Timestamp("2026-08-27T13:45:00.000000Z"),
            startedAt: nil, finishedAt: nil,
            firstSeenAt: Timestamp("2026-08-27T14:00:21.000000Z"),
            lastSeenAt: Timestamp("2026-08-27T14:00:22.000000Z"), deletedAt: nil,
            complete: complete)
    }
    #expect(job(complete: false, conditionType: "Failed").runOutcomeLabel == "run failed")
    #expect(
        job(complete: true, conditionType: "Complete").runOutcomeLabel
            == "run succeeded after retry")
    #expect(job(complete: false, conditionType: nil).runOutcomeLabel == "run still going")
}

@Test func jobRowsCarryTheirConditionAndCounters() throws {
    let page = try Page<Job>(wire: fixture(Components.Schemas.JobsResponse.self, "jobs.json"))
    #expect(
        page
            == Page(
                rows: [
                    failedReportJob,
                    Job(
                        uid: "job-report-2",
                        clusterID: "1",
                        namespace: "idios-smoke",
                        name: "report-28812350",
                        cronjobUID: nil,
                        cronjobName: nil,
                        active: 0,
                        succeeded: 1,
                        failed: 0,
                        backoffLimit: 0,
                        completions: 0,
                        parallelism: 0,
                        activeDeadlineSeconds: nil,
                        restartPolicy: "Never",
                        conditionType: "Complete",
                        conditionReason: nil,
                        conditionMessage: nil,
                        createdAt: Timestamp("2026-08-27T13:45:00.000000Z"),
                        startedAt: nil,
                        finishedAt: nil,
                        firstSeenAt: Timestamp("2026-08-27T14:00:23.000000Z"),
                        lastSeenAt: Timestamp("2026-08-27T14:00:24.000000Z"),
                        deletedAt: nil,
                        complete: true),
                ],
                truncated: true,
                total: 22,
                failedTotal: 4))
}

private let counterCases: [(Int32, Int32, Int32, Int32, Int32, String)] = [
    (1, 1, 0, 6, 5, "succeeded 0 of 1 - parallelism 1 - failed 6 of backoff limit 5"),
    (0, 0, 1, 0, 0, "succeeded 1 - failed 0 of backoff limit 0"),
    (1, 1, 1, 0, 5, "succeeded 1 of 1 - parallelism 1 - failed 0 of backoff limit 5"),
    (5, 2, 3, 1, 4, "succeeded 3 of 5 - parallelism 2 - failed 1 of backoff limit 4"),
]

// The counters are context only: what the spec asked for and what the status
// counted, never the run's outcome, which its condition alone decides.
@Test(arguments: counterCases)
func jobCountersSayWhatWasAskedForAndWhatWasCounted(
    completions: Int32, parallelism: Int32, succeeded: Int32, failed: Int32, backoffLimit: Int32,
    want: String
) {
    let job = Job(
        uid: "job-nightly-1", clusterID: "1", namespace: "idios-smoke", name: "nightly-1",
        cronjobUID: nil, cronjobName: nil, active: 0, succeeded: succeeded, failed: failed,
        backoffLimit: backoffLimit, completions: completions, parallelism: parallelism,
        activeDeadlineSeconds: nil, restartPolicy: "Never", conditionType: nil,
        conditionReason: nil, conditionMessage: nil,
        createdAt: Timestamp("2026-08-27T13:45:00.000000Z"), startedAt: nil, finishedAt: nil,
        firstSeenAt: Timestamp("2026-08-27T14:00:21.000000Z"),
        lastSeenAt: Timestamp("2026-08-27T14:00:22.000000Z"), deletedAt: nil, complete: false)
    #expect(jobCounters(job) == want)
}
