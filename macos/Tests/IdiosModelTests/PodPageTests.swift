import Testing

@testable import IdiosModel

// A pod route's tab picks the Pod card pane it opens; the left column tabs
// (containers, incidents, events) all open Events since that is where the
// list lives.
@Test(
    arguments: [
        (PodTab.containers, PodPane.events),
        (PodTab.incidents, PodPane.events),
        (PodTab.events, PodPane.events),
        (PodTab.conditions, PodPane.conditions),
        (PodTab.logs, PodPane.files),
        (PodTab.podJSON, PodPane.podJSON),
    ])
func podRoutesOpenThePodCardOnThePaneTheyNamed(tab: PodTab, want: PodPane) {
    #expect(tab.pane == want)
}

private let t1 = "2026-08-27T14:39:00.000000Z"
private let t2 = "2026-08-27T14:37:00.000000Z"
private let t3 = "2026-08-27T14:35:00.000000Z"

// The segmented control's segments read in the fold order, so the lit
// segment on arrival is the row the list would lead with.
@Test(
    arguments: [
        ([], []),
        (
            [
                row("1", pod: "p1", category: .crash, closed: true, lastSeen: t1),
                row("2", pod: "p1", category: .probe, lastSeen: t3),
            ],
            [
                row("2", pod: "p1", category: .probe, lastSeen: t3),
                row("1", pod: "p1", category: .crash, closed: true, lastSeen: t1),
            ]
        ),
        (
            [
                row("1", pod: "p1", category: .probe, lastSeen: t1),
                row("2", pod: "p1", category: .crash, lastSeen: t3),
            ],
            [
                row("2", pod: "p1", category: .crash, lastSeen: t3),
                row("1", pod: "p1", category: .probe, lastSeen: t1),
            ]
        ),
        (
            [
                row("1", pod: "p1", category: .crash, lastSeen: t2),
                row("2", pod: "p1", category: .crash, lastSeen: t1),
            ],
            [
                row("2", pod: "p1", category: .crash, lastSeen: t1),
                row("1", pod: "p1", category: .crash, lastSeen: t2),
            ]
        ),
        (
            [
                row("1", pod: "p1", category: .crash, lastSeen: t1),
                row("2", pod: "p1", category: .crash, lastSeen: t1),
            ],
            [
                row("2", pod: "p1", category: .crash, lastSeen: t1),
                row("1", pod: "p1", category: .crash, lastSeen: t1),
            ]
        ),
    ] as [([Incident], [Incident])])
func segmentsReadInTheFoldOrder(rows: [Incident], want: [Incident]) {
    #expect(segmentOrder(rows) == want)
}

/// container builds a container of apiContainer's fields but the name and
/// kind the containerOrder table varies.
private func container(_ name: String, kind: ContainerKind) -> Container {
    Container(
        id: apiContainer.id, podUID: apiContainer.podUID, name: name, kind: kind,
        image: apiContainer.image, imageTag: apiContainer.imageTag, imageID: apiContainer.imageID,
        containerID: apiContainer.containerID, cpuRequest: apiContainer.cpuRequest,
        cpuLimit: apiContainer.cpuLimit, memRequest: apiContainer.memRequest,
        memLimit: apiContainer.memLimit, cpuRequestMillis: apiContainer.cpuRequestMillis,
        cpuLimitMillis: apiContainer.cpuLimitMillis, memRequestBytes: apiContainer.memRequestBytes,
        memLimitBytes: apiContainer.memLimitBytes, state: apiContainer.state,
        reason: apiContainer.reason, exitCode: apiContainer.exitCode, signal: apiContainer.signal,
        ready: apiContainer.ready, restartCount: apiContainer.restartCount,
        runningSince: apiContainer.runningSince,
        lastTerminatedReason: apiContainer.lastTerminatedReason,
        lastTerminatedExitCode: apiContainer.lastTerminatedExitCode,
        lastTerminatedSignal: apiContainer.lastTerminatedSignal,
        lastTerminatedAt: apiContainer.lastTerminatedAt, updatedAt: apiContainer.updatedAt,
        grafanaURL: apiContainer.grafanaURL)
}

// The left column's containers read init, then app, then sidecar,
// ephemeral last, by name inside a kind.
@Test(
    arguments: [
        ([], []),
        (
            [
                container("trace-agent", kind: .sidecar),
                container("api", kind: .app),
                container("init-db", kind: .`init`),
            ],
            [
                container("init-db", kind: .`init`),
                container("api", kind: .app),
                container("trace-agent", kind: .sidecar),
            ]
        ),
        (
            [container("b", kind: .sidecar), container("a", kind: .sidecar)],
            [container("a", kind: .sidecar), container("b", kind: .sidecar)]
        ),
    ] as [([Container], [Container])])
func containersReadInitThenAppThenSidecar(containers: [Container], want: [Container]) {
    #expect(containerOrder(containers) == want)
}

// eventContainer reads the container name out of the kubelet's field_path
// spelling, or nil for a pod-level event.
@Test(
    arguments: [
        (nil, nil),
        ("", nil),
        ("spec.containers{api}", "api"),
        ("spec.initContainers{init-db}", "init-db"),
        ("spec.ephemeralContainers{debug}", "debug"),
        ("spec.containers{}", nil),
        ("status", nil),
    ] as [(String?, String?)])
func eventFieldPathsNameTheirContainer(fieldPath: String?, want: String?) {
    #expect(eventContainer(fieldPath: fieldPath) == want)
}

/// event builds an event of unhealthyEvent's fields but the id and field
/// path the container events table varies.
private func event(_ id: String, fieldPath: String?) -> Event {
    Event(
        id: id, clusterID: unhealthyEvent.clusterID, eventUID: unhealthyEvent.eventUID,
        namespace: unhealthyEvent.namespace, type: unhealthyEvent.type,
        involvedKind: unhealthyEvent.involvedKind, involvedName: unhealthyEvent.involvedName,
        involvedUID: unhealthyEvent.involvedUID, fieldPath: fieldPath,
        reason: unhealthyEvent.reason, message: unhealthyEvent.message,
        sourceComponent: unhealthyEvent.sourceComponent, count: unhealthyEvent.count,
        firstTS: unhealthyEvent.firstTS, lastTS: unhealthyEvent.lastTS,
        category: unhealthyEvent.category, incidentID: unhealthyEvent.incidentID)
}

// A container's pane keeps its own events plus every pod-level event, in
// served order.
@Test(
    arguments: [
        ([], []),
        (
            [
                event("api-1", fieldPath: "spec.containers{api}"),
                event("pod-1", fieldPath: nil),
                event("side-1", fieldPath: "spec.containers{trace-agent}"),
            ],
            [
                event("api-1", fieldPath: "spec.containers{api}"),
                event("pod-1", fieldPath: nil),
            ]
        ),
    ] as [([Event], [Event])])
func aContainerPaneShowsItsOwnEventsAndThePods(events: [Event], want: [Event]) {
    #expect(containerEvents(events, container: "api") == want)
}

/// readyRow builds a condition with the type, status and observed time the
/// readiness table varies; every other field copies readyCondition.
private func readyRow(_ type: String, status: String, at: String) -> PodCondition {
    PodCondition(
        id: "\(type)-\(at)", podUID: readyCondition.podUID, type: type, status: status,
        reason: readyCondition.reason, message: readyCondition.message,
        k8sTransitionAt: readyCondition.k8sTransitionAt,
        observedAt: Timestamp("2026-08-27T14:\(at):00.000000Z"))
}

// The tag is debounced on the readiness the history recorded, so it counts
// the changes rather than the readings, in observed order and only of the
// Ready condition.
@Test(
    arguments: [
        ([], 0),
        ([readyRow("Ready", status: "True", at: "30")], 0),
        ([readyRow("PodScheduled", status: "True", at: "30"),
          readyRow("Initialized", status: "False", at: "31")], 0),
        ([readyRow("Ready", status: "True", at: "30"),
          readyRow("Ready", status: "True", at: "31"),
          readyRow("Ready", status: "False", at: "32")], 1),
        ([readyRow("Ready", status: "True", at: "30"),
          readyRow("Ready", status: "False", at: "31"),
          readyRow("Ready", status: "True", at: "32"),
          readyRow("Ready", status: "False", at: "33")], 3),
        ([readyRow("Ready", status: "False", at: "33"),
          readyRow("Ready", status: "True", at: "30"),
          readyRow("Ready", status: "True", at: "32")], 1),
    ] as [([PodCondition], Int)])
func readinessFlipsCountsTheReadyConditionsChanges(conditions: [PodCondition], want: Int) {
    #expect(readinessFlips(conditions) == want)
}

// The header's state tag: DELETED beats everything, then a readiness that
// keeps flipping; a pod with no Ready condition read shows only its phase.
@Test(
    arguments: [
        ("Running", true, true, 0, "DELETED"),
        ("Running", true, false, 9, "DELETED"),
        ("Running", false, true, 0, "RUNNING, READY"),
        ("Running", false, false, 3, "RUNNING, NOT READY"),
        ("Running", false, false, 4, "RUNNING, LOOPING"),
        ("Running", false, true, 9, "RUNNING, LOOPING"),
        ("Pending", false, nil, 0, "PENDING"),
        ("Succeeded", false, false, 0, "SUCCEEDED, NOT READY"),
    ] as [(String, Bool, Bool?, Int, String)])
func podStateTagReadsLoopingPastThreeReadinessFlips(
    phase: String, deleted: Bool, ready: Bool?, flips: Int, want: String
) {
    #expect(podStateTag(phase: phase, deleted: deleted, ready: ready, flips: flips) == want)
}

/// containerLine builds a container of apiContainer's fields but the
/// state, reason, exit code and restart count the container line table
/// varies.
private func containerLine(
    state: ContainerState, ready: Bool = false, reason: String?, exitCode: Int32?,
    restartCount: Int32
) -> Container {
    Container(
        id: apiContainer.id, podUID: apiContainer.podUID, name: apiContainer.name,
        kind: apiContainer.kind, image: apiContainer.image, imageTag: apiContainer.imageTag,
        imageID: apiContainer.imageID, containerID: apiContainer.containerID,
        cpuRequest: apiContainer.cpuRequest, cpuLimit: apiContainer.cpuLimit,
        memRequest: apiContainer.memRequest, memLimit: apiContainer.memLimit,
        cpuRequestMillis: apiContainer.cpuRequestMillis,
        cpuLimitMillis: apiContainer.cpuLimitMillis, memRequestBytes: apiContainer.memRequestBytes,
        memLimitBytes: apiContainer.memLimitBytes, state: state, reason: reason,
        exitCode: exitCode, signal: apiContainer.signal, ready: ready,
        restartCount: restartCount, runningSince: apiContainer.runningSince,
        lastTerminatedReason: apiContainer.lastTerminatedReason,
        lastTerminatedExitCode: apiContainer.lastTerminatedExitCode,
        lastTerminatedSignal: apiContainer.lastTerminatedSignal,
        lastTerminatedAt: apiContainer.lastTerminatedAt, updatedAt: apiContainer.updatedAt,
        grafanaURL: apiContainer.grafanaURL)
}

// The container card's line says state, then reason or readiness, then
// what the kubelet counted.
@Test(
    arguments: [
        (
            containerLine(state: .running, ready: true, reason: nil, exitCode: nil, restartCount: 0),
            "running - ready - 0 restarts"
        ),
        (
            containerLine(state: .running, ready: false, reason: nil, exitCode: nil, restartCount: 1),
            "running - not ready - 1 restart"
        ),
        (
            containerLine(
                state: .waiting, reason: "CrashLoopBackOff", exitCode: nil, restartCount: 8),
            "waiting - CrashLoopBackOff - 8 restarts"
        ),
        (
            containerLine(state: .waiting, reason: nil, exitCode: nil, restartCount: 0),
            "waiting - 0 restarts"
        ),
        (
            containerLine(state: .terminated, reason: "Completed", exitCode: 0, restartCount: 0),
            "terminated - Completed - exit 0"
        ),
        (
            containerLine(state: .terminated, reason: "Error", exitCode: 1, restartCount: 1),
            "terminated - Error - exit 1"
        ),
        (
            containerLine(state: .terminated, reason: nil, exitCode: nil, restartCount: 3),
            "terminated - 3 restarts"
        ),
    ] as [(Container, String)])
func theContainerLineSaysStateReasonAndCount(container: Container, want: String) {
    #expect(containerStateLine(container) == want)
}

// The rail's first sibling row is this page's own pod, then the daemon's
// siblings in served order.
private let sib1 = SiblingPod(
    uid: "pod-sib-1", name: "checkout-api-7d9f8b6c4-k9tld", phase: "Running", deletedAt: nil,
    restartCount: 3, ready: false, worstOpenCategory: .imagePull)
private let sib2 = SiblingPod(
    uid: "pod-sib-2", name: "checkout-api-7d9f8b6c4-p8cg4", phase: "Running", deletedAt: nil,
    restartCount: 0, ready: true, worstOpenCategory: nil)
private let sib3 = SiblingPod(
    uid: "pod-sib-3", name: "checkout-api-7d9f8b6c4-ngq6w", phase: "Running", deletedAt: nil,
    restartCount: 0, ready: true, worstOpenCategory: nil)
private let sib4 = SiblingPod(
    uid: "pod-sib-4", name: "checkout-api-7d9f8b6c4-tmf48", phase: "Running", deletedAt: nil,
    restartCount: 0, ready: true, worstOpenCategory: nil)
private let sib5 = SiblingPod(
    uid: "pod-sib-5", name: "checkout-api-7d9f8b6c4-b7t2m", phase: "Failed",
    deletedAt: Timestamp("2026-08-27T14:39:30.000000Z"), restartCount: 2, ready: false,
    worstOpenCategory: nil)

@Test(
    arguments: [
        (
            false, [] as [SiblingPod],
            [
                SiblingRow(
                    // crashPodRow carries a deletedAt, so this pod's own row
                    // reads deleted true; the fixture, not the argument,
                    // decides it.
                    uid: crashPodRow.uid, name: crashPodRow.name, isThisPod: true, deleted: true,
                    ready: false, category: nil)
            ]
        ),
        (
            true, [sib1, sib2, sib3, sib4, sib5],
            [
                SiblingRow(
                    uid: crashPodRow.uid, name: crashPodRow.name, isThisPod: true, deleted: true,
                    ready: true, category: nil),
                SiblingRow(
                    uid: sib1.uid, name: sib1.name, isThisPod: false, deleted: false, ready: false,
                    category: .imagePull),
                SiblingRow(
                    uid: sib2.uid, name: sib2.name, isThisPod: false, deleted: false, ready: true,
                    category: nil),
                SiblingRow(
                    uid: sib3.uid, name: sib3.name, isThisPod: false, deleted: false, ready: true,
                    category: nil),
                SiblingRow(
                    uid: sib4.uid, name: sib4.name, isThisPod: false, deleted: false, ready: true,
                    category: nil),
                SiblingRow(
                    uid: sib5.uid, name: sib5.name, isThisPod: false, deleted: true, ready: false,
                    category: nil),
            ]
        ),
    ] as [(Bool, [SiblingPod], [SiblingRow])])
func theRailListsThisPodThenTheDaemonsSiblings(
    ready: Bool, siblings: [SiblingPod], want: [SiblingRow]
) {
    #expect(siblingRows(pod: crashPodRow, ready: ready, siblings: siblings) == want)
}

// The rail's more line counts what the total carries beyond this pod and
// the shown siblings.
@Test(
    arguments: [
        (0, 0, 0),
        (1, 0, 0),
        (8, 5, 2),
        (3, 5, 0),
        (6, 5, 0),
    ] as [(Int32, Int, Int)])
func theMoreLineCountsWhatTheRailDoesNotShow(total: Int32, shown: Int, want: Int) {
    #expect(moreSiblings(total: total, shown: shown) == want)
}

private let podPageNow = Timestamp("2026-08-27T14:39:00.000000Z").date ?? .distantPast

/// runContainer copies apiContainer with the two fields running since reads.
private func runContainer(state: ContainerState, since: String?) -> Container {
    Container(
        id: apiContainer.id, podUID: apiContainer.podUID, name: apiContainer.name,
        kind: apiContainer.kind, image: apiContainer.image, imageTag: apiContainer.imageTag,
        imageID: apiContainer.imageID, containerID: apiContainer.containerID,
        cpuRequest: apiContainer.cpuRequest, cpuLimit: apiContainer.cpuLimit,
        memRequest: apiContainer.memRequest, memLimit: apiContainer.memLimit,
        cpuRequestMillis: apiContainer.cpuRequestMillis,
        cpuLimitMillis: apiContainer.cpuLimitMillis,
        memRequestBytes: apiContainer.memRequestBytes,
        memLimitBytes: apiContainer.memLimitBytes,
        state: state, reason: apiContainer.reason, exitCode: apiContainer.exitCode,
        signal: apiContainer.signal, ready: apiContainer.ready,
        restartCount: apiContainer.restartCount, runningSince: since.map(Timestamp.init),
        lastTerminatedReason: apiContainer.lastTerminatedReason,
        lastTerminatedExitCode: apiContainer.lastTerminatedExitCode,
        lastTerminatedSignal: apiContainer.lastTerminatedSignal,
        lastTerminatedAt: apiContainer.lastTerminatedAt, updatedAt: apiContainer.updatedAt,
        grafanaURL: apiContainer.grafanaURL)
}

private let runningSinceCases: [(ContainerState, String?, String?)] = [
    (.waiting, "2026-08-27T12:39:00.000000Z", nil),
    (.running, nil, nil),
    (.running, "2026-08-27T12:39:00.000000Z", "since 12:39 (2h 0m)"),
    // A terminated container's running_since is the previous run's, and the
    // card is about the container that is running now.
    (.terminated, "2026-08-27T12:39:00.000000Z", nil),
]

@Test(arguments: runningSinceCases)
func runningSinceTextOnlyForAContainerThatIsRunning(
    state: ContainerState, since: String?, want: String?
) {
    #expect(runningSinceText(runContainer(state: state, since: since), now: podPageNow) == want)
}
