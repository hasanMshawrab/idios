import Testing

@testable import IdiosModel

private let opened = "2026-08-27T14:00:00.000000Z"

/// incident builds an incident row with the fields the palette matches on;
/// every other field is its zero and the times are fixed.
private func incident(
    id: String, podName: String?, workloadKind: String, workloadName: String,
    namespace: String = "idios-smoke", container: String? = nil, reason: String = "Error",
    tag: String? = nil, node: String = "", jobUID: String? = nil,
    lastSeen: String = "2026-08-27T14:39:00.000000Z"
) -> Incident {
    Incident(
        id: id,
        clusterID: "1",
        namespace: namespace,
        subjectKind: podName == nil ? .job : .pod,
        podUID: podName.map { "u-\($0)" },
        jobUID: jobUID,
        containerName: container,
        workloadKind: workloadKind,
        workloadName: workloadName,
        category: podName == nil ? .jobFailed : .crash,
        firstReason: reason,
        lastReason: reason,
        lastMessage: nil,
        image: nil,
        imageTag: tag,
        imageID: nil,
        occurrences: 1,
        openedAt: Timestamp(opened),
        lastSeenAt: Timestamp(lastSeen),
        closedAt: nil,
        closeReason: nil,
        acknowledgedAt: nil,
        dismissedAt: nil,
        note: nil,
        state: .open,
        podName: podName,
        podDeletedAt: nil,
        podDeletionReason: nil,
        containerCount: 1,
        exitCode: nil,
        signal: nil,
        nodeName: node)
}

/// pod builds a pod row with the fields the palette matches on.
private func pod(
    uid: String, name: String, namespace: String = "idios-smoke", node: String?,
    workloadKind: String, workloadName: String
) -> PodRow {
    PodRow(
        uid: uid,
        clusterID: "1",
        namespace: namespace,
        name: name,
        nodeName: node,
        phase: "Running",
        statusReason: nil,
        statusMessage: nil,
        qosClass: nil,
        controllerKind: nil,
        controllerName: nil,
        controllerUID: nil,
        workloadKind: workloadKind,
        workloadName: workloadName,
        createdAt: Timestamp(opened),
        startedAt: nil,
        firstSeenAt: Timestamp(opened),
        lastSeenAt: Timestamp(opened),
        deletionRequestedAt: nil,
        deletedAt: nil,
        deletionSource: nil,
        deletionReason: nil,
        openIncidents: 1,
        containerCount: 1,
        worstState: nil)
}

/// workload builds a workload row with the fields the palette matches on.
private func workload(
    kind: String, name: String, namespace: String = "idios-smoke", tags: [String?]
) -> Workload {
    Workload(
        clusterID: "1",
        namespace: namespace,
        workloadKind: kind,
        workloadName: name,
        incidentsByCategory: [:],
        openIncidents: 1,
        occurrences: 1,
        imageTags: tags.map { TagCount(tag: $0, count: 1) },
        livePods: 1,
        deletedPods: 0,
        podUID: nil,
        podName: nil)
}

private func sec(
    _ section: SearchSection, _ hits: [SearchHit], more: Int = 0
) -> SearchSectionHits {
    SearchSectionHits(section: section, hits: hits, more: more)
}

// The one fixture set the section and prefix tests both read: a crashing
// Deployment pod, a failed CronJob run of two rows, and an OOM on a pod no
// controller owns.
private let checkoutPod = pod(
    uid: "u-checkout-api-7d9f-x2kqp", name: "checkout-api-7d9f-x2kqp", node: "node-a",
    workloadKind: "Deployment", workloadName: "checkout-api")
private let workerPod = pod(
    uid: "u-worker", name: "worker", node: nil, workloadKind: "none", workloadName: "")

private let crash = incident(
    id: "187", podName: "checkout-api-7d9f-x2kqp", workloadKind: "Deployment",
    workloadName: "checkout-api", container: "app", reason: "Error", tag: "1.36",
    node: "node-a", lastSeen: "2026-08-27T14:39:00.000000Z")
private let jobFailure = incident(
    id: "188", podName: nil, workloadKind: "CronJob", workloadName: "report",
    reason: "BackoffLimitExceeded", jobUID: "j1", lastSeen: "2026-08-27T14:38:00.000000Z")
private let runPod = incident(
    id: "189", podName: "report-29807159-5dtpb", workloadKind: "CronJob",
    workloadName: "report", container: "api", reason: "Error", jobUID: "j1",
    lastSeen: "2026-08-27T14:37:00.000000Z")
private let oom = incident(
    id: "190", podName: "worker", workloadKind: "none", workloadName: "", container: "app",
    reason: "OOMKilled", lastSeen: "2026-08-27T14:36:00.000000Z")

private let checkoutWorkload = workload(
    kind: "Deployment", name: "checkout-api", tags: ["1.36"])
private let reportWorkload = workload(kind: "CronJob", name: "report", tags: ["2.1"])

private let fixturePods = [checkoutPod, workerPod]
private let fixtureIncidents = [crash, jobFailure, runPod, oom]
private let fixtureWorkloads = [checkoutWorkload, reportWorkload]
private let reportRun = runFolds(fixtureIncidents.filter { $0.jobUID != nil })[0]

private let checkoutRoute = Route.workload(
    cluster: "1", namespace: "idios-smoke", kind: "Deployment", name: "checkout-api")
private let reportRoute = Route.workload(
    cluster: "1", namespace: "idios-smoke", kind: "CronJob", name: "report")

private let showCheckout = SearchHit.command(
    SearchCommand(
        id: "workload/1/idios-smoke/Deployment/checkout-api",
        title: "Show Deployment checkout-api in Workloads", key: nil,
        action: .openWorkloadPods(checkoutRoute)))
private let showReport = SearchHit.command(
    SearchCommand(
        id: "workload/1/idios-smoke/CronJob/report",
        title: "Show CronJob report in Workloads", key: nil,
        action: .openWorkloadRuns(reportRoute)))

private func fixtureResults(_ raw: String) -> [SearchSectionHits] {
    searchResults(
        query: SearchQuery(parsing: raw), pods: fixturePods, incidents: fixtureIncidents,
        workloads: fixtureWorkloads, commands: [], recent: [])
}

/// The palette reads what was typed: a "#" and digits name an incident, the
/// four prefixes name a field, the rest is free text, and everything is
/// lowercased because matching ignores case.
@Test(
    arguments: [
        (
            "",
            SearchQuery(
                text: "", incidentID: nil, namespace: nil, node: nil, tag: nil, reason: nil)
        ),
        (
            "   ",
            SearchQuery(
                text: "", incidentID: nil, namespace: nil, node: nil, tag: nil, reason: nil)
        ),
        (
            "#187",
            SearchQuery(
                text: "", incidentID: "187", namespace: nil, node: nil, tag: nil, reason: nil)
        ),
        (
            "187",
            SearchQuery(
                text: "187", incidentID: nil, namespace: nil, node: nil, tag: nil, reason: nil)
        ),
        (
            "#abc",
            SearchQuery(
                text: "#abc", incidentID: nil, namespace: nil, node: nil, tag: nil, reason: nil)
        ),
        (
            "ns:",
            SearchQuery(
                text: "", incidentID: nil, namespace: nil, node: nil, tag: nil, reason: nil)
        ),
        (
            "ns:Idios crash",
            SearchQuery(
                text: "crash", incidentID: nil, namespace: "idios", node: nil, tag: nil,
                reason: nil)
        ),
        (
            "tag:1.36 node:node-a",
            SearchQuery(
                text: "", incidentID: nil, namespace: nil, node: "node-a", tag: "1.36",
                reason: nil)
        ),
        (
            "ns:a ns:b",
            SearchQuery(
                text: "", incidentID: nil, namespace: "b", node: nil, tag: nil, reason: nil)
        ),
        (
            "Report",
            SearchQuery(
                text: "report", incidentID: nil, namespace: nil, node: nil, tag: nil, reason: nil)
        ),
    ] as [(String, SearchQuery)])
func searchQueryReadsPrefixesAndTheIncidentID(raw: String, want: SearchQuery) {
    #expect(SearchQuery(parsing: raw) == want)
}

private let bySuffix: (String, [SearchSectionHits]) = (
    "x2kqp", [sec(.pods, [.pod(checkoutPod)]), sec(.incidents, [.incident(crash)])]
)

private let byJobName: (String, [SearchSectionHits]) = (
    "report",
    [
        sec(.runs, [.run(reportRun)]), sec(.workloads, [.workload(reportWorkload)]),
        sec(.incidents, [.incident(jobFailure), .incident(runPod)]),
        sec(.commands, [showReport]),
    ]
)

private let byIncidentID: (String, [SearchSectionHits]) = (
    "#187", [sec(.incidents, [.incident(crash)])]
)

private let byNamespace: (String, [SearchSectionHits]) = (
    "idios-smoke",
    [
        sec(.pods, [.pod(checkoutPod), .pod(workerPod)]), sec(.runs, [.run(reportRun)]),
        sec(.workloads, [.workload(checkoutWorkload), .workload(reportWorkload)]),
        sec(
            .incidents,
            [.incident(crash), .incident(jobFailure), .incident(runPod), .incident(oom)]),
        sec(.commands, [showCheckout, showReport]),
    ]
)

private let byContainer: (String, [SearchSectionHits]) = (
    "app", [sec(.incidents, [.incident(crash), .incident(oom)])]
)

private let byReason: (String, [SearchSectionHits]) = (
    "OOMKilled", [sec(.incidents, [.incident(oom)])]
)

private let byTag: (String, [SearchSectionHits]) = (
    "1.36",
    [
        sec(.workloads, [.workload(checkoutWorkload)]), sec(.incidents, [.incident(crash)]),
        sec(.commands, [showCheckout]),
    ]
)

private let byNode: (String, [SearchSectionHits]) = (
    "node-a", [sec(.pods, [.pod(checkoutPod)]), sec(.incidents, [.incident(crash)])]
)

/// One typed name reaches whatever carries it: an incident id, a pod name
/// down to its suffix, a Job name, a workload, a namespace, a container, a
/// reason, an image tag and a node, each in its own section.
@Test(
    arguments: [
        bySuffix, byJobName, byIncidentID, byNamespace, byContainer, byReason, byTag, byNode,
    ])
func searchResultsSectionsEveryKindOfName(raw: String, want: [SearchSectionHits]) {
    #expect(fixtureResults(raw) == want)
}

private let everyNamespace: (String, [SearchSectionHits]) = (
    "ns:idios-smoke",
    [
        sec(.pods, [.pod(checkoutPod), .pod(workerPod)]), sec(.runs, [.run(reportRun)]),
        sec(.workloads, [.workload(checkoutWorkload), .workload(reportWorkload)]),
        sec(
            .incidents,
            [.incident(crash), .incident(jobFailure), .incident(runPod), .incident(oom)]),
    ]
)

private let oneNode: (String, [SearchSectionHits]) = (
    "node:node-a", [sec(.pods, [.pod(checkoutPod)]), sec(.incidents, [.incident(crash)])]
)

private let oneTag: (String, [SearchSectionHits]) = (
    "tag:1.36",
    [sec(.workloads, [.workload(checkoutWorkload)]), sec(.incidents, [.incident(crash)])]
)

private let oneReason: (String, [SearchSectionHits]) = (
    "reason:oom", [sec(.incidents, [.incident(oom)])]
)

private let noSuchNamespace: (String, [SearchSectionHits]) = ("ns:other", [])

/// A field prefix narrows the sections whose rows carry that field and empties
/// the ones that do not: no workload has a node, no pod has an image tag or a
/// reason, and no section but Incidents answers a field at all when nothing
/// matches.
@Test(arguments: [everyNamespace, oneNode, oneTag, oneReason, noSuchNamespace])
func searchPrefixesNarrowByFieldAndEmptyTheSectionsWithoutIt(
    raw: String, want: [SearchSectionHits]
) {
    #expect(fixtureResults(raw) == want)
}

private let apiWorkload = workload(kind: "Deployment", name: "api", tags: [])
private let apiGatewayWorkload = workload(kind: "Deployment", name: "api-gateway", tags: [])
private let checkoutAPIWorkloadRow = workload(kind: "Deployment", name: "checkout-api", tags: [])

private func showWorkload(_ name: String, _ id: String) -> SearchHit {
    .command(
        SearchCommand(
            id: "workload/1/idios-smoke/Deployment/\(id)",
            title: "Show Deployment \(name) in Workloads", key: nil,
            action: .openWorkloadPods(
                .workload(
                    cluster: "1", namespace: "idios-smoke", kind: "Deployment", name: name))))
}

/// RankCase is one query with the rows it is served and the sections it ranks
/// them into.
struct RankCase: Sendable {
    let raw: String
    let pods: [PodRow]
    let incidents: [Incident]
    let workloads: [Workload]
    let want: [SearchSectionHits]
}

private let rankedNames = RankCase(
    raw: "api", pods: [], incidents: [],
    workloads: [checkoutAPIWorkloadRow, apiGatewayWorkload, apiWorkload],
    want: [
        sec(
            .workloads,
            [
                .workload(apiWorkload), .workload(apiGatewayWorkload),
                .workload(checkoutAPIWorkloadRow),
            ]),
        sec(
            .commands,
            [
                showWorkload("api", "api"), showWorkload("api-gateway", "api-gateway"),
                showWorkload("checkout-api", "checkout-api"),
            ]),
    ])

private let numberedID = incident(
    id: "12", podName: "checkout-12x", workloadKind: "Deployment", workloadName: "checkout-api")

private let idBeatsTheName = RankCase(
    raw: "#12",
    pods: [
        pod(
            uid: "u-checkout-12x", name: "checkout-12x", node: nil, workloadKind: "Deployment",
            workloadName: "checkout-api")
    ], incidents: [numberedID], workloads: [],
    want: [sec(.incidents, [.incident(numberedID)])])

/// The row whose name is what was typed comes first, then the ones it starts,
/// then the ones that merely contain it; an incident id answers with the
/// incident alone.
@Test(arguments: [rankedNames, idBeatsTheName])
func searchRanksExactBeforePrefixBeforeContains(testCase: RankCase) {
    let got = searchResults(
        query: SearchQuery(parsing: testCase.raw), pods: testCase.pods,
        incidents: testCase.incidents, workloads: testCase.workloads, commands: [], recent: [])
    #expect(got == testCase.want)
}

/// A palette answers a name rather than listing a workload's history, so a
/// section stops at its cap and counts what it cut.
@Test func searchCapsASectionAndCountsTheRest() {
    let rows = (1...8).map {
        incident(
            id: "\(200 - $0)", podName: "checkout-api-7d9f-\($0)", workloadKind: "Deployment",
            workloadName: "checkout-api", container: "app")
    }
    let got = searchResults(
        query: SearchQuery(parsing: "checkout-api"), pods: [], incidents: rows, workloads: [],
        commands: [], recent: [])
    #expect(got == [sec(.incidents, Array(rows.prefix(6)).map(SearchHit.incident), more: 2)])
}

private let recentIncidents = SearchCommand(
    id: "incidents", title: "Incidents", key: nil, action: .open(.incidents(nil)))
private let recentStatus = SearchCommand(
    id: "status", title: "Status", key: nil, action: .open(.status))

/// listed is a recent page as the palette draws it, under an id no command
/// shares.
private func listed(_ command: SearchCommand) -> SearchHit {
    .command(
        SearchCommand(
            id: "recent/\(command.id)", title: command.title, key: command.key,
            action: command.action))
}

/// An empty query is the menu: the pages just visited, then every command
/// with the key that also reaches it; a page whose command is in the list
/// too is two rows, not one.
@Test func searchEmptyQueryListsRecentThenEveryCommand() {
    let commands = commandList(views: [.attention, .open])
    #expect(
        commands == [
            SearchCommand(
                id: "incidents", title: "Incidents", key: "cmd-1", action: .open(.incidents(nil))),
            SearchCommand(
                id: "workloads", title: "Workloads", key: "cmd-2", action: .open(.workloads)),
            SearchCommand(id: "status", title: "Status", key: "cmd-3", action: .open(.status)),
            SearchCommand(id: "back", title: "Back", key: "cmd-[", action: .back),
            SearchCommand(
                id: "view/attention", title: "Show Attention", key: nil,
                action: .open(.incidents(.attention))),
            SearchCommand(
                id: "view/open", title: "Show Open", key: nil, action: .open(.incidents(.open))),
        ])

    let withRecent = searchResults(
        query: SearchQuery(parsing: ""), pods: fixturePods, incidents: fixtureIncidents,
        workloads: fixtureWorkloads, commands: commands,
        recent: [recentStatus, recentIncidents])
    #expect(
        withRecent == [
            sec(.recent, [listed(recentStatus), listed(recentIncidents)]),
            sec(.commands, commands.map(SearchHit.command)),
        ])

    let withoutRecent = searchResults(
        query: SearchQuery(parsing: "   "), pods: fixturePods, incidents: fixtureIncidents,
        workloads: fixtureWorkloads, commands: commands, recent: [])
    #expect(withoutRecent == [sec(.commands, commands.map(SearchHit.command))])
}

/// HitFacts is what the three keys of a hit do, compared as one value.
struct HitFacts: Hashable {
    var id: String
    var action: SearchAction
    var workloadAction: SearchAction?
    var copyText: String?
}

private let newerRunPod = incident(
    id: "191", podName: "report-29807159-s966s", workloadKind: "CronJob",
    workloadName: "report", container: "api", reason: "Error", jobUID: "j1",
    lastSeen: "2026-08-27T14:41:00.000000Z")

// The newest pod row is not the first row served, so a run that offers the
// name of the pod that failed last has to read the times rather than the
// order.
private let podKeptRun = runFolds([jobFailure, runPod, newerRunPod])[0]
private let jobOnlyRun = runFolds([jobFailure])[0]

private let backCommand = SearchCommand(id: "back", title: "Back", key: "cmd-[", action: .back)

private let podHitFacts: (SearchHit, HitFacts) = (
    .pod(checkoutPod),
    HitFacts(
        id: "pod/u-checkout-api-7d9f-x2kqp",
        action: .open(.pod("u-checkout-api-7d9f-x2kqp", .containers)),
        workloadAction: .openWorkloadPods(checkoutRoute),
        copyText: "checkout-api-7d9f-x2kqp")
)

private let runHitFacts: (SearchHit, HitFacts) = (
    .run(podKeptRun),
    HitFacts(
        id: "run/j1", action: .open(.run("j1")),
        workloadAction: .openWorkloadRuns(reportRoute), copyText: "report-29807159-s966s")
)

private let jobOnlyRunHitFacts: (SearchHit, HitFacts) = (
    .run(jobOnlyRun),
    HitFacts(
        id: "run/j1", action: .open(.run("j1")),
        workloadAction: .openWorkloadRuns(reportRoute), copyText: "report")
)

private let bareIncidentHitFacts: (SearchHit, HitFacts) = (
    .incident(oom),
    HitFacts(
        id: "incident/190", action: .open(.incident("190")), workloadAction: nil,
        copyText: "worker")
)

private let workloadHitFacts: (SearchHit, HitFacts) = (
    .workload(checkoutWorkload),
    HitFacts(
        id: "workload/1/idios-smoke/Deployment/checkout-api", action: .open(checkoutRoute),
        workloadAction: .openWorkloadPods(checkoutRoute), copyText: "checkout-api")
)

private let cronJobHitFacts: (SearchHit, HitFacts) = (
    .workload(reportWorkload),
    HitFacts(
        id: "workload/1/idios-smoke/CronJob/report", action: .open(reportRoute),
        workloadAction: .openWorkloadRuns(reportRoute), copyText: "report")
)

private let commandHitFacts: (SearchHit, HitFacts) = (
    .command(backCommand),
    HitFacts(id: "command/back", action: .back, workloadAction: nil, copyText: nil)
)

/// Return opens the hit, Cmd-Return opens its workload on the tab its kind
/// reads by, and Shift-Return copies the name a person pastes; a pod
/// with no controller and a command have no workload to open.
@Test(
    arguments: [
        podHitFacts, runHitFacts, jobOnlyRunHitFacts, bareIncidentHitFacts, workloadHitFacts,
        cronJobHitFacts, commandHitFacts,
    ])
func searchHitActionsOpenTheHitAndItsWorkload(hit: SearchHit, want: HitFacts) {
    #expect(
        HitFacts(
            id: hit.id, action: hit.action, workloadAction: hit.workloadAction,
            copyText: hit.copyText) == want)
}
