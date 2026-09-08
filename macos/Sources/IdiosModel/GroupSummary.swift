import Foundation

/// GroupFacts is what the incident rows cannot say about their workload: the
/// pods it runs and the runs it has had, read from the workload and jobs
/// endpoints when the store has them.
public struct GroupFacts: Hashable, Sendable {
    public let livePods: Int32?
    public let runsTotal: Int32?
    public let runsFailed: Int32?

    public init(livePods: Int32?, runsTotal: Int32?, runsFailed: Int32?) {
        self.livePods = livePods
        self.runsTotal = runsTotal
        self.runsFailed = runsFailed
    }
}

/// StateTone is which of the three hues a badge that summarises several rows
/// takes.
public enum StateTone: Hashable, Sendable { case open, acknowledged, closed }

/// GroupBadge is the one state badge of a header: its text and its tone.
public struct GroupBadge: Hashable, Sendable {
    public let text: String
    public let tone: StateTone
}

/// groupBadge counts the open rows; the tone is red while one of them is
/// unacknowledged, amber when every open row is acknowledged, grey when none
/// is open.
public func groupBadge(_ rows: [Incident]) -> GroupBadge {
    let open = rows.filter { $0.closedAt == nil }
    guard !open.isEmpty else { return GroupBadge(text: "\(rows.count) closed", tone: .closed) }
    let tone: StateTone = open.contains { $0.acknowledgedAt == nil } ? .open : .acknowledged
    return GroupBadge(text: "\(open.count) open", tone: tone)
}

/// runCadence is how often a run has been opening: the median gap between
/// consecutive runs' openings, or nil under three runs, where a gap is not yet
/// a rhythm.
public func runCadence(openedAt: [Timestamp]) -> String? {
    let dates = openedAt.compactMap(\.date).sorted()
    guard dates.count >= 3 else { return nil }
    let gaps = zip(dates, dates.dropFirst()).map { $1.timeIntervalSince($0) }.sorted()
    let median = gaps[gaps.count / 2]
    let text = durationText(from: Date(timeIntervalSince1970: 0),
        to: Date(timeIntervalSince1970: median))
    // A whole number of hours reads as a rhythm, not as a duration measured
    // to the minute.
    return text.hasSuffix(" 0m") ? String(text.dropLast(3)) : text
}

/// groupSummary is the header's plain-language line: for a CronJob or Job how
/// often it runs, how many runs failed of how many, and since when; for every
/// other kind how many pods have the problem of how many live, what the
/// problem is, and which image tags are involved.
public func groupSummary(kind: String, rows: [Incident], facts: GroupFacts?) -> String {
    guard !rows.isEmpty else { return "" }
    if kind == "CronJob" || kind == "Job" { return runSummary(rows: rows, facts: facts) }
    return podSummary(rows: rows, facts: facts)
}

private func runSummary(rows: [Incident], facts: GroupFacts?) -> String {
    let runs = buckets(rows) { $0.jobUID.map { "job/\($0)" } ?? "incident/\($0.id)" }
    var parts: [String] = []
    let openings = runs.compactMap { $0.rows.map(\.openedAt).min { $0.raw < $1.raw } }
    if let cadence = runCadence(openedAt: openings) { parts.append("every \(cadence)") }
    if let total = facts?.runsTotal, let failed = facts?.runsFailed {
        parts.append("\(failed) of \(count(total, "run")) failed")
    } else {
        parts.append("\(count(runs.count, "run")) failed")
    }
    if let oldest = rows.map(\.openedAt).min(by: { $0.raw < $1.raw }) {
        parts.append("since \(clockTime(oldest))")
    }
    return parts.joined(separator: ", ")
}

private func podSummary(rows: [Incident], facts: GroupFacts?) -> String {
    let open = rows.filter { $0.closedAt == nil }
    let pods = Set(open.compactMap(\.podUID)).count
    guard pods > 0 else {
        return "\(count(Set(rows.compactMap(\.podUID)).count, "pod")), nothing open"
    }
    let verb = (open.min { $0.category.rank < $1.category.rank })?.category.verb ?? "failing"
    var parts: [String] = []
    if let live = facts?.livePods {
        parts.append("\(pods) of \(count(live, "pod")) \(verb)")
    } else {
        parts.append("\(count(pods, "pod")) \(verb)")
    }
    let tags = distinct(rows.compactMap(\.imageTag))
    if tags.count == 1 {
        parts.append("tag \(tags[0])")
    } else if tags.count > 1 {
        parts.append("tags \(tags.joined(separator: ", "))")
    }
    return parts.joined(separator: ", ")
}

extension Category {
    // The header says what a person would say out loud about the pods, which
    // is the category as a verb rather than its name.
    fileprivate var verb: String {
        switch self {
        case .crash: "looping"
        case .oom: "out of memory"
        case .uncleanExit: "exiting badly"
        case .imagePull: "cannot pull the image"
        case .config: "cannot start"
        case .probe: "failing probes"
        case .scheduling: "unschedulable"
        case .stuck: "stuck"
        case .nodePressure: "evicted"
        case .rescheduled: "rescheduled"
        case .jobFailed: "failed"
        case .other: "failing"
        }
    }
}

/// ListSummary is the line above the list.
public struct ListSummary: Hashable, Sendable {
    public let problems: Int
    public let openIncidents: Int
    public let workloads: Int
    public let barePods: Int
    public let newest: Timestamp?
}

/// listSummary counts the groups as problems, the rows still open, the groups
/// that have a workload and the ones that are a bare pod, and finds the newest
/// last seen.
public func listSummary(groups: [[Incident]]) -> ListSummary {
    let rows = groups.flatMap { $0 }
    let workloads = groups.filter { group in group.contains { !$0.workloadName.isEmpty } }
    return ListSummary(
        problems: groups.count,
        openIncidents: rows.filter { $0.closedAt == nil }.count,
        workloads: workloads.count,
        barePods: groups.count - workloads.count,
        newest: rows.map(\.lastSeenAt).max { $0.raw < $1.raw })
}

private func count(_ n: some BinaryInteger, _ noun: String) -> String {
    "\(n) \(noun)\(n == 1 ? "" : "s")"
}
