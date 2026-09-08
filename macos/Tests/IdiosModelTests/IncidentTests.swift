import IdiosAPI
import Testing

@testable import IdiosModel

let crashIncident = Incident(
    id: "412",
    clusterID: "1",
    namespace: "idios-smoke",
    subjectKind: .pod,
    podUID: "pod-crash",
    jobUID: "job-report-1",
    containerName: "api",
    workloadKind: "Deployment",
    workloadName: "checkout-api",
    category: .crash,
    firstReason: "CrashLoopBackOff",
    lastReason: "Error",
    lastMessage: "back-off 5m0s restarting failed container",
    image: "registry.example.com/web:1.4.2",
    imageTag: "1.4.2",
    imageID: "registry.example.com/web@sha256:1111",
    occurrences: 7,
    openedAt: Timestamp("2026-08-27T14:03:11.482913Z"),
    lastSeenAt: Timestamp("2026-08-27T14:39:02.100000Z"),
    closedAt: Timestamp("2026-08-27T14:40:00.000000Z"),
    closeReason: .podDeleted,
    acknowledgedAt: Timestamp("2026-08-27T14:20:00.000000Z"),
    dismissedAt: Timestamp("2026-08-27T14:41:00.000000Z"),
    note: "known bad deploy",
    state: .dismissed,
    podName: "checkout-api-7d9f8b6c4-x2kqp",
    podDeletedAt: Timestamp("2026-08-27T14:39:30.000000Z"),
    podDeletionReason: .rollout,
    containerCount: 3,
    exitCode: 137,
    signal: 9,
    nodeName: "node-a")

/// relatedProbeIncident is the other open incident of the same pod that the
/// detail carries alongside its own.
let relatedProbeIncident = Incident(
    id: "415",
    clusterID: "1",
    namespace: "idios-smoke",
    subjectKind: .pod,
    podUID: "pod-crash",
    jobUID: nil,
    containerName: "api",
    workloadKind: "Deployment",
    workloadName: "checkout-api",
    category: .probe,
    firstReason: "Unhealthy",
    lastReason: "Unhealthy",
    lastMessage: nil,
    image: nil,
    imageTag: nil,
    imageID: nil,
    occurrences: 2,
    openedAt: Timestamp("2026-08-27T14:30:00.000000Z"),
    lastSeenAt: Timestamp("2026-08-27T14:38:00.000000Z"),
    closedAt: nil,
    closeReason: nil,
    acknowledgedAt: nil,
    dismissedAt: nil,
    note: nil,
    state: .open,
    podName: "checkout-api-7d9f8b6c4-x2kqp",
    podDeletedAt: nil,
    podDeletionReason: nil,
    containerCount: 3,
    exitCode: nil,
    signal: nil,
    nodeName: "node-a")

// step A fills a per-container grafana_url on GetPod only; the same
// container on GetIncident carries none.
let incidentApiContainer = Container(
    id: "51",
    podUID: "pod-crash",
    name: "api",
    kind: .app,
    image: "registry.example.com/web:1.4.2",
    imageTag: "1.4.2",
    imageID: "registry.example.com/web@sha256:1111",
    containerID: "containerd://aaa",
    cpuRequest: "250m",
    cpuLimit: "1",
    memRequest: "256Mi",
    memLimit: "512Mi",
    cpuRequestMillis: 250,
    cpuLimitMillis: 1000,
    memRequestBytes: 268_435_456,
    memLimitBytes: 536_870_912,
    state: .terminated,
    reason: "Error",
    exitCode: 137,
    signal: 9,
    ready: false,
    restartCount: 7,
    runningSince: Timestamp("2026-08-27T14:30:00.000000Z"),
    lastTerminatedReason: "OOMKilled",
    lastTerminatedExitCode: 137,
    lastTerminatedSignal: 9,
    lastTerminatedAt: Timestamp("2026-08-27T14:38:00.000000Z"),
    updatedAt: Timestamp("2026-08-27T14:39:02.100000Z"),
    grafanaURL: nil)

let jobIncident = Incident(
    id: "413",
    clusterID: "2",
    namespace: "idios-smoke",
    subjectKind: .job,
    podUID: nil,
    jobUID: nil,
    containerName: nil,
    workloadKind: "CronJob",
    workloadName: "report",
    category: .jobFailed,
    firstReason: "BackoffLimitExceeded",
    lastReason: "BackoffLimitExceeded",
    lastMessage: nil,
    image: nil,
    imageTag: nil,
    imageID: nil,
    occurrences: 1,
    openedAt: Timestamp("2026-08-27T14:03:11.482913Z"),
    lastSeenAt: Timestamp("2026-08-27T14:03:11.482913Z"),
    closedAt: nil,
    closeReason: nil,
    acknowledgedAt: nil,
    dismissedAt: nil,
    note: nil,
    state: .open,
    podName: nil,
    podDeletedAt: nil,
    podDeletionReason: nil,
    containerCount: 0,
    exitCode: nil,
    signal: nil,
    nodeName: "")

// presentation.md: node_name rides the incident row, stamped at open.
// crashIncident's pod was placed and carries node-a; jobIncident's minimal
// fixture sends no nodeName at all and the model takes its zero.
@Test(
    arguments: [
        ("incident_row.json", crashIncident),
        ("incident_row_minimal.json", jobIncident),
    ])
func incidentRowsUnwrapWhatIsGuaranteedAndZeroWhatIsAbsent(name: String, want: Incident) throws {
    let incident = try Incident(wire: fixture(Components.Schemas.IncidentRow.self, name))
    #expect(incident == want)
}

@Test func incidentListCarriesItsRowsAndTheTruncationFlag() throws {
    let page = try Page<Incident>(
        wire: fixture(Components.Schemas.IncidentsResponse.self, "incidents.json"))
    #expect(page == Page(rows: [crashIncident, jobIncident], truncated: true))
}

@Test func incidentCountsBecomeOneEntryPerStateAndCategoryFolder() throws {
    let counts = try IncidentCounts(
        wire: fixture(Components.Schemas.IncidentCounts.self, "incident_counts.json"))
    #expect(
        counts
            == IncidentCounts(
                byState: [.open: 4, .acknowledged: 1, .podDeleted: 2, .attention: 5],
                byCategory: [.crash: 3, .oom: 2, .uncleanExit: 1]))
}

// The events of the detail are the subject pod's whole stream: a row that
// attached to this incident carries its id, one that attached to nothing
// carries none.
@Test func incidentDetailCarriesEveryPartOfTheScreen() throws {
    let detail = try IncidentDetail(
        wire: fixture(Components.Schemas.IncidentDetail.self, "incident_detail.json"))
    #expect(
        detail
            == IncidentDetail(
                incident: crashIncident,
                pod: crashPod,
                containers: [incidentApiContainer],
                artifacts: [crashArtifact],
                events: [unhealthyEvent, killingEvent],
                conditions: [readyCondition],
                // Decision 5: a pod incident carrying a job_uid now fills the
                // same job row a job incident gets.
                job: failedReportJob,
                lastPodName: nil,
                grafanaURL:
                    "https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22idios-smoke%5C%22%2C+pod%3D%5C%22checkout-api-7d9f8b6c4-x2kqp%5C%22%2C+container%3D%5C%22api%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221787839091482%22%2C%22to%22%3A%221787841900000%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D",
                relatedIncidents: [relatedProbeIncident]))
}

// A Job is the subject of its own incident: the screen has a Job row and the
// name of the pod that ran it, and no kubelet rows at all.
@Test func aJobIncidentDetailCarriesTheJobAndItsLastPod() throws {
    let detail = try IncidentDetail(
        wire: fixture(Components.Schemas.IncidentDetail.self, "incident_detail_job.json"))
    #expect(
        detail
            == IncidentDetail(
                incident: Incident(
                    id: "414",
                    clusterID: "1",
                    namespace: "idios-smoke",
                    subjectKind: .job,
                    podUID: nil,
                    jobUID: "job-report-1",
                    containerName: nil,
                    workloadKind: "CronJob",
                    workloadName: "report",
                    category: .jobFailed,
                    firstReason: "BackoffLimitExceeded",
                    lastReason: "BackoffLimitExceeded",
                    lastMessage: "Job has reached the specified backoff limit",
                    image: nil,
                    imageTag: nil,
                    imageID: nil,
                    occurrences: 1,
                    openedAt: Timestamp("2026-08-27T14:00:22.000000Z"),
                    lastSeenAt: Timestamp("2026-08-27T14:00:22.000000Z"),
                    closedAt: nil,
                    closeReason: nil,
                    acknowledgedAt: nil,
                    dismissedAt: nil,
                    note: nil,
                    state: .open,
                    podName: nil,
                    podDeletedAt: nil,
                    podDeletionReason: nil,
                    containerCount: 0,
                    exitCode: nil,
                    signal: nil,
                    nodeName: ""),
                pod: nil,
                containers: [],
                artifacts: [],
                events: [],
                conditions: [],
                job: failedReportJob,
                lastPodName: "report-28812345-b7t2m",
                grafanaURL: nil,
                relatedIncidents: []))
}

@Test func aRowWithoutTheFieldsAnIncidentAlwaysHasIsRefused() throws {
    var wire = try fixture(Components.Schemas.IncidentRow.self, "incident_row.json")
    wire.category = nil
    #expect(throws: ModelError.missing(field: "category")) { try Incident(wire: wire) }
}

// A pod with no controller arrives with workloadKind "none" and no
// workloadName at all; the daemon sent exactly that and the list refused it.
@Test func aPodWithoutAControllerHasAnEmptyWorkloadName() throws {
    var wire = try fixture(Components.Schemas.IncidentRow.self, "incident_row_minimal.json")
    wire.workloadKind = "none"
    wire.workloadName = nil
    let incident = try Incident(wire: wire)
    #expect(incident.workloadKind == "none")
    #expect(incident.workloadName == "")
}
