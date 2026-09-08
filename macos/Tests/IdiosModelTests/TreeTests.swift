import Testing

@testable import IdiosModel

/// BareLine is a merged row and the three answers it gives about opening it,
/// compared as one value.
struct BareLine: Hashable {
    var row: BarePodRow
    var podCount: Int
    var live: Workload?
    var needsChooser: Bool
}

private func lines(_ rows: [BarePodRow]) -> [BareLine] {
    rows.map {
        BareLine(row: $0, podCount: $0.podCount, live: $0.live, needsChooser: $0.needsChooser)
    }
}

/// bare builds one workload row of a pod no controller owns.
private func bare(uid: String, name: String?, live: Bool, open: Int32) -> Workload {
    Workload(
        clusterID: "1", namespace: "idios-smoke", workloadKind: "none", workloadName: "",
        incidentsByCategory: [:], openIncidents: open, occurrences: 0, imageTags: [],
        livePods: live ? 1 : 0, deletedPods: live ? 0 : 1, podUID: uid, podName: name)
}

private let liveWorker = bare(uid: "u1", name: "worker", live: true, open: 1)
private let deletedReport = bare(uid: "u2", name: "report", live: false, open: 1)
private let liveReport = bare(uid: "u3", name: "report", live: true, open: 2)
private let firstNightly = bare(uid: "u4", name: "nightly", live: false, open: 0)
private let secondNightly = bare(uid: "u5", name: "nightly", live: false, open: 3)
private let unnamedPod = bare(uid: "u6", name: nil, live: true, open: 0)

private let noRows: ([Workload], [BareLine]) = ([], [])

private let fourNames: ([Workload], [BareLine]) = (
    [
        liveWorker, deletedReport, liveReport, firstNightly, secondNightly, unnamedPod,
        // A controlled workload is not a bare pod.
        checkoutAPIWorkload,
    ],
    [
        BareLine(
            row: BarePodRow(
                id: "1/idios-smoke/none/worker", name: "worker", rows: [liveWorker],
                openIncidents: 1),
            podCount: 1, live: liveWorker, needsChooser: false),
        BareLine(
            row: BarePodRow(
                id: "1/idios-smoke/none/report", name: "report",
                rows: [liveReport, deletedReport], openIncidents: 3),
            podCount: 2, live: liveReport, needsChooser: true),
        BareLine(
            row: BarePodRow(
                id: "1/idios-smoke/none/nightly", name: "nightly",
                rows: [firstNightly, secondNightly], openIncidents: 3),
            podCount: 2, live: nil, needsChooser: true),
        BareLine(
            row: BarePodRow(
                id: "1/idios-smoke/none/u6", name: "", rows: [unnamedPod], openIncidents: 0),
            podCount: 1, live: unnamedPod, needsChooser: false),
    ]
)

@Test(arguments: [noRows, fourNames])
func barePodRowsMergeOneNameAndSayWhenItCannotOpenOne(rows: [Workload], want: [BareLine]) {
    #expect(lines(barePodRows(rows)) == want)
}

@Test(
    arguments: [
        ("Deployment", 1, "Deployments 1"),
        ("CronJob", 2, "CronJobs 2"),
        ("none", 5, "Bare pods 5"),
        ("StatefulSet", 0, "StatefulSets 0"),
    ] as [(String, Int, String)])
func kindCaptionIsThePluralWithItsCount(kind: String, count: Int, want: String) {
    #expect(kindCaption(kind: kind, count: count) == want)
}

/// controlled builds one workload row a controller owns.
private func controlled(cluster: String, namespace: String, kind: String, name: String)
    -> Workload
{
    Workload(
        clusterID: cluster, namespace: namespace, workloadKind: kind, workloadName: name,
        incidentsByCategory: [:], openIncidents: 0, occurrences: 0, imageTags: [],
        livePods: 1, deletedPods: 0, podUID: nil, podName: nil)
}

private let checkout = controlled(
    cluster: "1", namespace: "shop", kind: "Deployment", name: "checkout")
private let nightlyJob = controlled(
    cluster: "1", namespace: "batch", kind: "CronJob", name: "nightly")
private let strayPod = Workload(
    clusterID: "1", namespace: "batch", workloadKind: "none", workloadName: "",
    incidentsByCategory: [:], openIncidents: 0, occurrences: 0, imageTags: [],
    livePods: 1, deletedPods: 0, podUID: "u9", podName: "stray")

private let twoNamespaces = [
    ClusterGroup(
        clusterID: "1",
        namespaces: [
            NamespaceGroup(
                clusterID: "1", namespace: "batch",
                kinds: [
                    KindGroup(
                        clusterID: "1", namespace: "batch", kind: "CronJob",
                        rows: [nightlyJob]),
                    KindGroup(
                        clusterID: "1", namespace: "batch", kind: "none", rows: [strayPod]),
                ]),
            NamespaceGroup(
                clusterID: "1", namespace: "shop",
                kinds: [
                    KindGroup(
                        clusterID: "1", namespace: "shop", kind: "Deployment",
                        rows: [checkout])
                ]),
        ])
]

private let everyRow = [
    "cluster/1",
    "1/batch",
    "1/batch/CronJob", "1/batch/CronJob/nightly",
    "1/batch/none", "1/batch/none/stray",
    "1/shop",
    "1/shop/Deployment", "1/shop/Deployment/checkout",
]

@Test(
    arguments: [
        ([] as Set<String>, everyRow),
        (["cluster/1"], ["cluster/1"]),
        (
            ["1/batch"],
            [
                "cluster/1", "1/batch", "1/shop", "1/shop/Deployment",
                "1/shop/Deployment/checkout",
            ]
        ),
    ] as [(Set<String>, [String])])
func flatWorkloadTreeLeavesOutWhatACollapsedGroupHides(
    collapsed: Set<String>, want: [String]
) {
    #expect(flatWorkloadTree(twoNamespaces, collapsed: collapsed).map(\.id) == want)
}
