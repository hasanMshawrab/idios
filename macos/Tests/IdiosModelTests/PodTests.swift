import IdiosAPI
import Testing

@testable import IdiosModel

let crashPod = Pod(
    uid: "pod-crash",
    clusterID: "1",
    namespace: "idios-smoke",
    name: "checkout-api-7d9f8b6c4-x2kqp",
    nodeName: "node-a",
    phase: "Running",
    statusReason: "Evicted",
    statusMessage: "The node was low on resource: memory.",
    qosClass: "Burstable",
    controllerKind: "ReplicaSet",
    controllerName: "checkout-api-7d9f8b6c4",
    controllerUID: "rs-checkout-1",
    workloadKind: "Deployment",
    workloadName: "checkout-api",
    createdAt: Timestamp("2026-08-27T13:00:00.000000Z"),
    startedAt: Timestamp("2026-08-27T13:00:05.000000Z"),
    firstSeenAt: Timestamp("2026-08-27T13:00:10.000000Z"),
    lastSeenAt: Timestamp("2026-08-27T14:39:02.100000Z"),
    deletionRequestedAt: Timestamp("2026-08-27T14:39:20.000000Z"),
    deletedAt: Timestamp("2026-08-27T14:39:30.000000Z"),
    deletionSource: .watch,
    deletionReason: .rollout)

let crashPodRow = PodRow(
    uid: "pod-crash",
    clusterID: "1",
    namespace: "idios-smoke",
    name: "checkout-api-7d9f8b6c4-x2kqp",
    nodeName: "node-a",
    phase: "Running",
    statusReason: "Evicted",
    statusMessage: "The node was low on resource: memory.",
    qosClass: "Burstable",
    controllerKind: "ReplicaSet",
    controllerName: "checkout-api-7d9f8b6c4",
    controllerUID: "rs-checkout-1",
    workloadKind: "Deployment",
    workloadName: "checkout-api",
    createdAt: Timestamp("2026-08-27T13:00:00.000000Z"),
    startedAt: Timestamp("2026-08-27T13:00:05.000000Z"),
    firstSeenAt: Timestamp("2026-08-27T13:00:10.000000Z"),
    lastSeenAt: Timestamp("2026-08-27T14:39:02.100000Z"),
    deletionRequestedAt: Timestamp("2026-08-27T14:39:20.000000Z"),
    deletedAt: Timestamp("2026-08-27T14:39:30.000000Z"),
    deletionSource: .watch,
    deletionReason: .rollout,
    openIncidents: 2,
    containerCount: 3,
    worstState: .terminated)

let workerPodRow = PodRow(
    uid: "pod-side",
    clusterID: "2",
    namespace: "idios-smoke",
    name: "checkout-worker-6b8d9c5f7-q4nlz",
    nodeName: nil,
    phase: "Pending",
    statusReason: nil,
    statusMessage: nil,
    qosClass: nil,
    controllerKind: "ReplicaSet",
    controllerName: "checkout-worker-6b8d9c5f7",
    controllerUID: "rs-worker-1",
    workloadKind: "Deployment",
    workloadName: "checkout-worker",
    createdAt: Timestamp("2026-08-27T14:00:00.000000Z"),
    startedAt: nil,
    firstSeenAt: Timestamp("2026-08-27T14:00:01.000000Z"),
    lastSeenAt: Timestamp("2026-08-27T14:00:01.000000Z"),
    deletionRequestedAt: nil,
    deletedAt: nil,
    deletionSource: nil,
    deletionReason: nil,
    openIncidents: 0,
    containerCount: 0,
    worstState: nil)

let evictedPodRow = PodRow(
    uid: "pod-evicted",
    clusterID: "1",
    namespace: "idios-smoke",
    name: "checkout-api-7d9f8b6c4-m8vqr",
    nodeName: "node-b",
    phase: "Failed",
    statusReason: "Evicted",
    statusMessage: "The node was low on resource: ephemeral-storage.",
    qosClass: "BestEffort",
    controllerKind: "ReplicaSet",
    controllerName: "checkout-api-7d9f8b6c4",
    controllerUID: "rs-checkout-1",
    workloadKind: "Deployment",
    workloadName: "checkout-api",
    createdAt: Timestamp("2026-08-27T12:00:00.000000Z"),
    startedAt: Timestamp("2026-08-27T12:00:04.000000Z"),
    firstSeenAt: Timestamp("2026-08-27T12:00:06.000000Z"),
    lastSeenAt: Timestamp("2026-08-27T12:58:00.000000Z"),
    deletionRequestedAt: nil,
    deletedAt: Timestamp("2026-08-27T12:58:10.000000Z"),
    deletionSource: .reconcile,
    deletionReason: .evicted,
    openIncidents: 1,
    containerCount: 1,
    worstState: .terminated)

let apiContainer = Container(
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
    grafanaURL:
        "https://logs.example.grafana.net/explore?schemaVersion=1&panes=%7B%22a%22%3A%7B%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22expr%22%3A%22%7Bnamespace%3D%5C%22idios-smoke%5C%22%2C+pod%3D%5C%22checkout-api-7d9f8b6c4-x2kqp%5C%22%2C+container%3D%5C%22api%5C%22%7D%22%2C%22queryType%22%3A%22range%22%2C%22datasource%22%3A%7B%22type%22%3A%22loki%22%2C%22uid%22%3A%22grafanacloud-logs%22%7D%2C%22direction%22%3A%22backward%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%221787840700000%22%2C%22to%22%3A%221787841842100%22%7D%2C%22panelsState%22%3A%7B%22logs%22%3A%7B%22sortOrder%22%3A%22Descending%22%7D%7D%2C%22compact%22%3Afalse%7D%7D")

let readyCondition = PodCondition(
    id: "61",
    podUID: "pod-crash",
    type: "Ready",
    status: "False",
    reason: "ContainersNotReady",
    message: "containers with unready status: [api]",
    k8sTransitionAt: Timestamp("2026-08-27T14:38:01.000000Z"),
    observedAt: Timestamp("2026-08-27T14:39:02.100000Z"))

@Test func podRowsKeepWhatTheDaemonSentAndZeroWhatItDidNot() throws {
    let page = try Page<PodRow>(wire: fixture(Components.Schemas.PodsResponse.self, "pods.json"))
    #expect(page == Page(rows: [crashPodRow, workerPodRow, evictedPodRow], truncated: true))
}

@Test func podDetailCarriesThePodAndEveryPartOfIt() throws {
    let detail = try PodDetail(wire: fixture(Components.Schemas.PodDetail.self, "pod_detail.json"))
    #expect(
        detail
            == PodDetail(
                pod: crashPodRow,
                containers: [apiContainer],
                conditions: [readyCondition],
                incidents: [crashIncident],
                artifacts: [crashArtifact],
                siblings: [
                    SiblingPod(
                        uid: "pod-sib-1",
                        name: "checkout-api-7d9f8b6c4-k9tld",
                        phase: "Running",
                        deletedAt: nil,
                        restartCount: 3,
                        ready: false,
                        worstOpenCategory: .imagePull),
                    SiblingPod(
                        uid: "pod-sib-2",
                        name: "checkout-api-7d9f8b6c4-p8cg4",
                        phase: "Running",
                        deletedAt: nil,
                        restartCount: 0,
                        ready: true,
                        worstOpenCategory: nil),
                    SiblingPod(
                        uid: "pod-sib-3",
                        name: "checkout-api-7d9f8b6c4-ngq6w",
                        phase: "Running",
                        deletedAt: nil,
                        restartCount: 0,
                        ready: true,
                        worstOpenCategory: nil),
                    SiblingPod(
                        uid: "pod-sib-4",
                        name: "checkout-api-7d9f8b6c4-tmf48",
                        phase: "Running",
                        deletedAt: nil,
                        restartCount: 0,
                        ready: true,
                        worstOpenCategory: nil),
                    SiblingPod(
                        uid: "pod-sib-5",
                        name: "checkout-api-7d9f8b6c4-b7t2m",
                        phase: "Failed",
                        deletedAt: Timestamp("2026-08-27T14:39:30.000000Z"),
                        restartCount: 2,
                        ready: false,
                        worstOpenCategory: nil)
                ],
                siblingTotal: 8))
}

@Test func historyCarriesReconstructedTransitionsAndConditions() throws {
    let history = try PodHistory(
        wire: fixture(Components.Schemas.HistoryResponse.self, "history.json"))
    #expect(
        history
            == PodHistory(
                transitions: [
                    ContainerTransition(
                        id: "91",
                        podUID: "pod-crash",
                        containerName: "api",
                        incidentID: "412",
                        image: "registry.example.com/web:1.4.2",
                        imageID: "registry.example.com/web@sha256:1111",
                        containerID: "containerd://aaa",
                        state: .terminated,
                        reason: "Error",
                        exitCode: 137,
                        signal: 9,
                        restartCount: 7,
                        category: .oom,
                        k8sStartedAt: Timestamp("2026-08-27T14:30:00.000000Z"),
                        k8sFinishedAt: Timestamp("2026-08-27T14:38:00.000000Z"),
                        observedAt: Timestamp("2026-08-27T14:38:02.000000Z"),
                        gapReconstructed: true)
                ],
                conditions: [readyCondition]))
}
