import IdiosAPI
import Testing

@testable import IdiosModel

@Test func statusCarriesEveryCounterAndAClusterWithoutRuntimeState() throws {
    let status = try DaemonStatus(wire: fixture(Components.Schemas.Status.self, "status.json"))
    #expect(
        status
            == DaemonStatus(
                daemonRunning: true,
                writtenAt: Timestamp("2026-08-27T14:39:02.100000Z"),
                pid: 4242,
                version: "0.1.0",
                clusters: [
                    StatusCluster(
                        id: "1",
                        ready: true,
                        lastEventAt: Timestamp("2026-08-27T14:39:02.100000Z"),
                        skewSeconds: 1.5),
                    StatusCluster(id: "2", ready: false, lastEventAt: nil, skewSeconds: nil),
                ],
                writer: WriterStats(transactions: 1204, errors: 2, p99Ms: 3.5),
                handlers: HandlerStats(errors: 1, panics: 0),
                capture: CaptureStats(
                    queued: 9, completed: 7, dropped: 1, gaps: [.podDeleted: 1, .noOutput: 2]),
                closer: CloserStats(
                    lastTickAt: Timestamp("2026-08-27T14:38:00.000000Z"),
                    closed: 5,
                    attached: 3,
                    closedTotal: 14,
                    attachedTotal: 6,
                    opened: 1,
                    openedTotal: 9),
                openByCategory: [.oom: 1, .crash: 3],
                closedByReason: [.recovered: 2, .podDeleted: 1],
                artifactsByOutcome: ["file": 12, "no_output": 1],
                rowCounts: RowCounts(pods: 42, livePods: 38, transitions: 310, events: 88),
                latestSweepRuns: [
                    SweepRun(
                        id: "7",
                        ranAt: Timestamp("2026-08-27T14:00:00.000000Z"),
                        cutoff: Timestamp("2026-08-24T14:00:00.000000Z"),
                        tableName: "container_state_history",
                        rowsRemoved: 3,
                        filesRemoved: 1,
                        bytesRemoved: 262_144,
                        durationMs: 4,
                        error: "disk full")
                ],
                dbBytes: 1_048_576,
                walBytes: 32768,
                artifactFiles: 12,
                artifactBytes: 262_144,
                retentionDays: 3,
                sweepIntervalSeconds: 3600,
                stabilizationWindowSeconds: 600,
                stabilizationCheckIntervalSeconds: 30,
                earlyCaptureDebounceSeconds: 60,
                apiStreamThrottleSeconds: 1,
                schedulingGraceSeconds: 60,
                probeGraceSeconds: 60,
                stuckAfterSeconds: 900,
                attentionWindowSeconds: 86400))
}

@Test func kubeContextsCarryTheirServer() throws {
    let page = try Page<KubeContext>(
        wire: fixture(Components.Schemas.KubeContextsResponse.self, "kube_contexts.json"))
    #expect(
        page
            == Page(
                rows: [
                    KubeContext(
                        name: "orbstack", cluster: "orbstack", server: "https://127.0.0.1:26443")
                ],
                truncated: false))
}

@Test(
    arguments: [
        (
            "kube_namespaces.json",
            KubeNamespaces(names: ["default", "idios-smoke"], forbidden: false)
        ),
        ("kube_namespaces_forbidden.json", KubeNamespaces(names: [], forbidden: true)),
    ])
func namespaceListingSaysWhenTheClusterRefusedIt(name: String, want: KubeNamespaces) throws {
    let namespaces = try KubeNamespaces(
        wire: fixture(Components.Schemas.KubeNamespacesResponse.self, name))
    #expect(namespaces == want)
}

// The daemon groups every capture attempt, and the attempts that produced a
// file arrive as a bucket with no gap; the real status refused to decode.
@Test func aCaptureBucketWithoutAGapIsTheFileOutcomeAndIsNotAnError() throws {
    var wire = try fixture(Components.Schemas.Status.self, "status.json")
    wire.capture?.gaps?.insert(.init(count: 238), at: 0)
    let status = try DaemonStatus(wire: wire)
    #expect(status.capture.gaps == [.podDeleted: 1, .noOutput: 2])
}
