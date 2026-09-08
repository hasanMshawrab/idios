/// SearchQuery is what a person typed, read once: the free text and the
/// fields the prefixes name.
public struct SearchQuery: Hashable, Sendable {
    public let text: String
    public let incidentID: String?
    public let namespace: String?
    public let node: String?
    public let tag: String?
    public let reason: String?

    /// isEmpty is a query with nothing to match on.
    public var isEmpty: Bool {
        text.isEmpty && incidentID == nil && namespace == nil && node == nil && tag == nil
            && reason == nil
    }
}

extension SearchQuery {
    /// init reads the raw field text: whitespace-separated terms, a term
    /// starting with "#" is an incident id, "ns:", "node:", "tag:" and
    /// "reason:" name a field, everything else joins the free text.
    public init(parsing raw: String) {
        var free: [String] = []
        var fields: [String: String] = [:]
        var id: String?
        for term in raw.lowercased().split(whereSeparator: { $0.isWhitespace }).map(String.init) {
            if term.hasPrefix("#") {
                let digits = term.dropFirst()
                // A "#" in front of anything but digits names no incident, and
                // dropping the term would lose a word a person meant to type.
                if !digits.isEmpty, digits.allSatisfy({ $0.isASCII && $0.isNumber }) {
                    id = String(digits)
                } else {
                    free.append(term)
                }
                continue
            }
            if let colon = term.firstIndex(of: ":") {
                let name = String(term[term.startIndex..<colon])
                let value = String(term[term.index(after: colon)...])
                if SearchQuery.fieldNames.contains(name) {
                    if !value.isEmpty { fields[name] = value }
                    continue
                }
            }
            free.append(term)
        }
        self.text = free.joined(separator: " ")
        self.incidentID = id
        self.namespace = fields["ns"]
        self.node = fields["node"]
        self.tag = fields["tag"]
        self.reason = fields["reason"]
    }

    private static let fieldNames: Set<String> = ["ns", "node", "tag", "reason"]

    fileprivate var terms: [String] {
        text.split(separator: " ").map(String.init)
    }

    fileprivate var hasField: Bool {
        incidentID != nil || namespace != nil || node != nil || tag != nil || reason != nil
    }
}

/// SearchSection is one group of hits, in the palette's order.
public enum SearchSection: String, CaseIterable, Hashable, Sendable {
    case recent, pods, runs, workloads, incidents, commands

    /// title is the section's heading.
    public var title: String {
        switch self {
        case .recent: "Recent"
        case .pods: "Pods"
        case .runs: "Runs"
        case .workloads: "Workloads"
        case .incidents: "Incidents"
        case .commands: "Commands"
        }
    }
}

/// SearchAction is what opening a hit does; the application maps a route
/// onto its screens.
public enum SearchAction: Hashable, Sendable {
    case open(Route)
    case openWorkloadPods(Route)
    case openWorkloadRuns(Route)
    case back
}

/// SearchCommand is a row of the Commands or Recent section: a title, the
/// key that also reaches it, and what it does.
public struct SearchCommand: Identifiable, Hashable, Sendable {
    public let id: String
    public let title: String
    public let key: String?
    public let action: SearchAction

    public init(id: String, title: String, key: String?, action: SearchAction) {
        self.id = id
        self.title = title
        self.key = key
        self.action = action
    }
}

/// SearchHit is one row of the palette carrying the value it stands for.
public enum SearchHit: Identifiable, Hashable, Sendable {
    case pod(PodRow)
    case run(RunFold)
    case workload(Workload)
    case incident(Incident)
    case command(SearchCommand)

    /// id is distinct across sections: "pod/<uid>", "run/<job uid>",
    /// "workload/<Workload.id>", "incident/<id>", "command/<id>".
    public var id: String {
        switch self {
        case .pod(let row): "pod/\(row.uid)"
        case .run(let fold): "run/\(fold.jobUID)"
        case .workload(let workload): "workload/\(workload.id)"
        case .incident(let incident): "incident/\(incident.id)"
        case .command(let command): "command/\(command.id)"
        }
    }

    /// action is what Return does: a pod opens its page, a run opens its run
    /// page, a workload opens its Workloads node, an incident opens its page,
    /// a command does what it says.
    public var action: SearchAction {
        switch self {
        case .pod(let row):
            .open(.pod(row.uid, .containers))
        case .run(let fold):
            .open(.run(fold.jobUID))
        case .workload(let workload):
            workload.podUID.map { .open(.pod($0, .containers)) }
                ?? .open(
                    .workload(
                        cluster: workload.clusterID, namespace: workload.namespace,
                        kind: workload.workloadKind, name: workload.workloadName))
        case .incident(let incident):
            .open(.incident(incident.id))
        case .command(let command):
            command.action
        }
    }

    /// workloadAction is what Cmd-Return does: the hit's workload in
    /// Workloads, on Runs for a CronJob or Job and on Pods otherwise; nil
    /// for a command and for a bare pod.
    public var workloadAction: SearchAction? {
        switch self {
        case .pod(let row):
            workloadRouteAction(
                cluster: row.clusterID, namespace: row.namespace, kind: row.workloadKind,
                name: row.workloadName)
        case .run(let fold):
            workloadRouteAction(
                cluster: fold.lead.clusterID, namespace: fold.lead.namespace,
                kind: fold.lead.workloadKind, name: fold.lead.workloadName)
        case .workload(let workload):
            workloadRouteAction(
                cluster: workload.clusterID, namespace: workload.namespace,
                kind: workload.workloadKind, name: workload.workloadName)
        case .incident(let incident):
            workloadRouteAction(
                cluster: incident.clusterID, namespace: incident.namespace,
                kind: incident.workloadKind, name: incident.workloadName)
        case .command:
            nil
        }
    }

    /// copyText is what Shift-Return copies: the name a person pastes into
    /// kubectl, the pod's when the hit has one and the workload's otherwise;
    /// nil for a command.
    public var copyText: String? {
        switch self {
        case .pod(let row): row.name
        case .run(let fold): newestPodRow(fold)?.podName ?? fold.lead.workloadName
        case .workload(let workload): workload.podName ?? workload.workloadName
        case .incident(let incident): incident.podName ?? incident.workloadName
        case .command: nil
        }
    }
}

// The list is served newest first, but a run's rows are read by their own
// times, so the name a run offers is the pod that failed last.
private func newestPodRow(_ fold: RunFold) -> Incident? {
    var newest: Incident?
    for row in fold.rows where row.podUID != nil {
        if row.lastSeenAt.raw > (newest?.lastSeenAt.raw ?? "") { newest = row }
    }
    return newest
}

private func workloadRouteAction(
    cluster: String, namespace: String, kind: String, name: String
) -> SearchAction? {
    guard kind != "none", !kind.isEmpty, !name.isEmpty else { return nil }
    let route = Route.workload(cluster: cluster, namespace: namespace, kind: kind, name: name)
    return kind == "CronJob" || kind == "Job"
        ? .openWorkloadRuns(route) : .openWorkloadPods(route)
}

/// SearchSectionHits is one section as drawn: its hits after the cap and
/// how many the cap cut.
public struct SearchSectionHits: Hashable, Sendable {
    public let section: SearchSection
    public let hits: [SearchHit]
    public let more: Int

    public init(section: SearchSection, hits: [SearchHit], more: Int) {
        self.section = section
        self.hits = hits
        self.more = more
    }
}

/// sectionLimit is how many hits a section shows; the rest are a count,
/// because a palette answers a name, not a list.
public let sectionLimit = 6

/// searchResults matches a query against the rows every store holds and
/// returns the sections that have anything to say, in order.
public func searchResults(
    query: SearchQuery, pods: [PodRow], incidents: [Incident],
    workloads: [Workload], commands: [SearchCommand], recent: [SearchCommand]
) -> [SearchSectionHits] {
    if query.isEmpty {
        // A visited page and the command that opens it share an id, and two
        // hits with one id are one row to a ForEach and to the selection.
        let recentHits = recent.map {
            SearchHit.command(
                SearchCommand(
                    id: "recent/\($0.id)", title: $0.title, key: $0.key, action: $0.action))
        }
        return [
            SearchSectionHits(section: .recent, hits: recentHits, more: 0),
            SearchSectionHits(section: .commands, hits: commands.map(SearchHit.command), more: 0),
        ].filter { !$0.hits.isEmpty }
    }

    let podHits = ranked(
        pods, query: query,
        section: query.incidentID == nil && query.tag == nil && query.reason == nil,
        kept: { fieldsKept($0, query) }, fields: fields(pod:), hit: SearchHit.pod)
    let runHits = ranked(
        runFolds(incidents.filter { $0.jobUID != nil }), query: query,
        section: query.incidentID == nil, kept: { fieldsKept($0, query) }, fields: fields(run:),
        hit: SearchHit.run)
    let workloadHits = ranked(
        workloads, query: query,
        section: query.incidentID == nil && query.node == nil && query.reason == nil,
        kept: { fieldsKept($0, query) }, fields: fields(workload:), hit: SearchHit.workload)
    let incidentHits = ranked(
        incidents, query: query, section: true, kept: { fieldsKept($0, query) },
        fields: fields(incident:), hit: SearchHit.incident)
    let named = ranked(
        commands, query: query, section: true, kept: { _ in true },
        fields: fields(command:), hit: SearchHit.command)
    // A field or an id names a row of the cluster, and no command is one.
    let commandHits =
        query.hasField ? [] : contextualCommands(workloadHits).map(SearchHit.command) + named

    return [
        section(.pods, podHits), section(.runs, runHits), section(.workloads, workloadHits),
        section(.incidents, incidentHits), section(.commands, commandHits),
    ].compactMap { $0 }
}

/// commandList is every command the palette lists on an empty query: the
/// Go menu's items with their keys and one "Show <view>" per sidebar view.
public func commandList(views: [IncidentState]) -> [SearchCommand] {
    [
        SearchCommand(
            id: "incidents", title: "Incidents", key: "cmd-1", action: .open(.incidents(nil))),
        SearchCommand(
            id: "workloads", title: "Workloads", key: "cmd-2", action: .open(.workloads)),
        SearchCommand(id: "status", title: "Status", key: "cmd-3", action: .open(.status)),
        SearchCommand(id: "back", title: "Back", key: "cmd-[", action: .back),
    ]
        + views.map {
            SearchCommand(
                id: "view/\($0.rawValue)", title: "Show \($0.title)", key: nil,
                action: .open(.incidents($0)))
        }
}

// A palette answers a name, so the field a name matched decides how well:
// the whole text as the field, at its head, or anywhere inside it.
private enum Rank: Int {
    case exact, prefix, contains
}

// An id is only ever typed whole; a name is typed in part.
private struct Field {
    let value: String
    let exactOnly: Bool

    init(_ value: String?, exactOnly: Bool = false) {
        self.value = value?.lowercased() ?? ""
        self.exactOnly = exactOnly
    }
}

private func fields(pod row: PodRow) -> [Field] {
    [
        Field(row.name), Field(row.namespace), Field(row.nodeName), Field(row.workloadName),
    ]
}

private func fields(run fold: RunFold) -> [Field] {
    [
        Field(fold.suffix), Field(fold.jobUID), Field(fold.lead.workloadName),
        Field(fold.lead.namespace),
    ]
        + fold.rows.map { Field($0.podName) } + fold.reasons.map { Field($0) }
}

private func fields(workload: Workload) -> [Field] {
    [
        Field(workload.workloadName), Field(workload.namespace), Field(workload.workloadKind),
        Field(workload.podName),
    ] + workload.imageTags.map { Field($0.tag) }
}

private func fields(incident: Incident) -> [Field] {
    [
        Field(incident.id, exactOnly: true), Field(incident.podName),
        Field(incident.workloadName),
        Field(incident.namespace), Field(incident.containerName), Field(incident.lastReason),
        Field(incident.firstReason), Field(incident.imageTag), Field(incident.nodeName),
    ]
}

private func fields(command: SearchCommand) -> [Field] {
    [Field(command.title)]
}

private func fieldsKept(_ row: PodRow, _ query: SearchQuery) -> Bool {
    keeps(query.namespace, [Field(row.namespace)]) && keeps(query.node, [Field(row.nodeName)])
}

private func fieldsKept(_ fold: RunFold, _ query: SearchQuery) -> Bool {
    keeps(query.namespace, [Field(fold.lead.namespace)])
        && keeps(query.node, fold.rows.map { Field($0.nodeName) })
        && keeps(query.tag, fold.rows.map { Field($0.imageTag) })
        && keeps(query.reason, fold.reasons.map { Field($0) })
}

private func fieldsKept(_ workload: Workload, _ query: SearchQuery) -> Bool {
    keeps(query.namespace, [Field(workload.namespace)])
        && keeps(query.tag, workload.imageTags.map { Field($0.tag) })
}

private func fieldsKept(_ incident: Incident, _ query: SearchQuery) -> Bool {
    keeps(query.namespace, [Field(incident.namespace)])
        && keeps(query.node, [Field(incident.nodeName)])
        && keeps(query.tag, [Field(incident.imageTag)])
        && keeps(query.reason, [Field(incident.lastReason), Field(incident.firstReason)])
        && (query.incidentID == nil || query.incidentID == incident.id)
}

private func keeps(_ wanted: String?, _ fields: [Field]) -> Bool {
    guard let wanted else { return true }
    return fields.contains { !$0.value.isEmpty && $0.value.contains(wanted) }
}

private func matches(_ terms: [String], _ fields: [Field]) -> Bool {
    terms.allSatisfy { term in
        fields.contains { field in
            guard !field.value.isEmpty else { return false }
            return field.exactOnly ? field.value == term : field.value.contains(term)
        }
    }
}

private func rank(_ text: String, _ fields: [Field]) -> Rank {
    guard !text.isEmpty else { return .exact }
    if fields.contains(where: { !$0.value.isEmpty && $0.value == text }) { return .exact }
    if fields.contains(where: { !$0.exactOnly && !$0.value.isEmpty && $0.value.hasPrefix(text) })
    {
        return .prefix
    }
    return .contains
}

// A protocol would buy nothing here: each row set brings its own keep and
// field readers, and the ordering is the same for all of them.
private func ranked<Row>(
    _ rows: [Row], query: SearchQuery, section: Bool, kept: (Row) -> Bool,
    fields: (Row) -> [Field], hit: (Row) -> SearchHit
) -> [SearchHit] {
    guard section else { return [] }
    let terms = query.terms
    var scored: [(rank: Rank, order: Int, hit: SearchHit)] = []
    for (order, row) in rows.enumerated() {
        let rowFields = fields(row)
        guard kept(row), matches(terms, rowFields) else { continue }
        // An id was typed whole or it did not match at all.
        let rowRank = query.incidentID != nil ? Rank.exact : rank(query.text, rowFields)
        scored.append((rowRank, order, hit(row)))
    }
    return scored.sorted { ($0.rank.rawValue, $0.order) < ($1.rank.rawValue, $1.order) }
        .map(\.hit)
}

// While a name is being typed the Commands section is about that name: the
// workloads it found come before a command whose own title says it.
private func contextualCommands(_ workloadHits: [SearchHit]) -> [SearchCommand] {
    workloadHits.prefix(3).compactMap { hit -> SearchCommand? in
        guard case .workload(let workload) = hit, let action = hit.workloadAction else {
            return nil
        }
        return SearchCommand(
            id: "workload/\(workload.id)",
            title: "Show \(workload.workloadKind) \(workload.workloadName) in Workloads",
            key: nil, action: action)
    }
}

private func section(_ section: SearchSection, _ hits: [SearchHit]) -> SearchSectionHits? {
    guard !hits.isEmpty else { return nil }
    return SearchSectionHits(
        section: section, hits: Array(hits.prefix(sectionLimit)),
        more: max(0, hits.count - sectionLimit))
}
