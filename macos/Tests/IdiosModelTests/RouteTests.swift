import Testing

@testable import IdiosModel

private let cases: [(path: String, want: Route?)] = [
    ("", nil),
    ("bogus", nil),
    ("incidents/nonsense", nil),
    ("incidents/open/extra", nil),
    ("pod", nil),
    ("pod/u1/bogus", nil),
    ("workload/only/three/parts", nil),
    ("incidents", .incidents(nil)),
    ("incidents/attention", .incidents(.attention)),
    ("incidents/pod_deleted", .incidents(.podDeleted)),
    ("incident/412", .incident("412")),
    ("timeline/412", .timeline("412")),
    ("pod/u1", .pod("u1", .containers)),
    ("pod/u1/podjson", .pod("u1", .podJSON)),
    ("pod/u1/conditions", .pod("u1", .conditions)),
    ("run", nil),
    ("run/a/b", nil),
    ("run/abc", .run("abc")),
    ("workloads", .workloads),
    (
        "workload/orbstack/shop/Deployment/checkout-api",
        .workload(cluster: "orbstack", namespace: "shop", kind: "Deployment", name: "checkout-api")
    ),
    ("status", .status),
    ("addcluster", .addCluster),
    ("clusters", .clusters),
    ("menubar", .menubar),
]

@Test(arguments: cases)
func routePathsMapOntoTheScreensTheyName(path: String, want: Route?) {
    #expect(Route(path: path) == want)
}

private let lastSeen = "2026-08-27T14:39:00.000000Z"

private let openedCases: [(incident: Incident, want: Route)] = [
    (
        row("1", pod: nil, category: .jobFailed, lastSeen: lastSeen, job: "job-report-1"),
        .run("job-report-1")
    ),
    (row("2", pod: nil, category: .jobFailed, lastSeen: lastSeen, job: nil), .incident("2")),
    (row("3", pod: nil, category: .jobFailed, lastSeen: lastSeen, job: ""), .incident("3")),
    (
        row("4", pod: "p1", category: .crash, lastSeen: lastSeen, job: "job-report-1"),
        .incident("4")
    ),
    (row("5", pod: "p1", category: .crash, lastSeen: lastSeen, job: nil), .incident("5")),
]

// The Run page is reached from every job-subject incident, and only the
// subject decides: a pod row carries its Job's uid too.
@Test(arguments: openedCases)
func aJobSubjectIncidentOpensItsRunAndEveryOtherOpensItsPod(incident: Incident, want: Route) {
    #expect(incidentRoute(incident) == want)
}
