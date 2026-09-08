extension Category {
    /// ranked is the category vocabulary, worst first: the sidebar's order,
    /// the fold's lead choice and a group's worst category.
    public static let ranked: [Category] = [
        .crash, .oom, .uncleanExit, .imagePull, .config, .probe, .scheduling, .stuck,
        .nodePressure, .rescheduled, .jobFailed,
    ]

    /// rank is this category's place in ranked; a category the list does not
    /// rank sorts behind every one it does.
    public var rank: Int { Category.ranked.firstIndex(of: self) ?? Category.ranked.count }
}

/// PodFold is one pod's incidents inside a group: the row that leads them and
/// the siblings that fold under it.
public struct PodFold: Identifiable, Hashable, Sendable {
    public let id: String
    public let podUID: String?
    public let lead: Incident
    public let siblings: [Incident]
}

/// foldOrder ranks two rows of one pod the way the list's fold and the
/// page's segments read them: an open row before a closed one, then the
/// worst category, then the newest last seen, then the id compared as text,
/// a last-resort total tie-break.
public func foldOrder(_ a: Incident, _ b: Incident) -> Bool {
    if (a.closedAt == nil) != (b.closedAt == nil) { return a.closedAt == nil }
    if a.category.rank != b.category.rank { return a.category.rank < b.category.rank }
    if a.lastSeenAt.raw != b.lastSeenAt.raw { return a.lastSeenAt.raw > b.lastSeenAt.raw }
    return a.id > b.id
}

/// podFolds gathers the rows of one group by pod, so one pod's containers and
/// categories read as the single event they are.
public func podFolds(_ rows: [Incident]) -> [PodFold] {
    // A job-subject incident has no pod, so its own id keeps it on its own.
    buckets(rows) { $0.podUID.map { "pod/\($0)" } ?? "incident/\($0.id)" }.map { bucket in
        let lead = bucket.rows.min(by: foldOrder) ?? bucket.rows[0]
        return PodFold(
            id: lead.id, podUID: lead.podUID, lead: lead,
            siblings: bucket.rows.filter { $0.id != lead.id })
    }
}

/// buckets groups rows by a derived key, keeping first-seen key order.
public func buckets(
    _ rows: [Incident], key: (Incident) -> String
) -> [(key: String, rows: [Incident])] {
    var order: [String] = []
    var buckets: [String: [Incident]] = [:]
    for row in rows {
        let key = key(row)
        if buckets[key] == nil { order.append(key) }
        buckets[key, default: []].append(row)
    }
    return order.map { (key: $0, rows: buckets[$0] ?? []) }
}
