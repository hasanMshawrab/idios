import Foundation

/// TimelineFold is one line of the timeline: an entry, and the entries
/// after it that repeat it, which its summary stands for.
public struct TimelineFold: Identifiable, Hashable, Sendable {
    public let id: String
    public let lead: TimelineEntry
    public let repeats: [TimelineEntry]
    /// summary is the folded cycle's one line, nil when nothing folded.
    public var summary: String?
}

/// timelineFolds folds a repeating cycle into one line: consecutive entries
/// whose kind, container, state and reason repeat with the same period at
/// least three times. Lifecycle, rollout and cut entries never fold,
/// because they are the spine of the story rather than one of its kinds.
public func timelineFolds(_ entries: [TimelineEntry], minimumCycles: Int) -> [TimelineFold] {
    let keys = entries.map(foldKey)
    var folds: [TimelineFold] = []
    var at = 0
    while at < entries.count {
        let (period, cycles) = cycle(keys, from: at, minimumCycles: minimumCycles)
        guard period * cycles > 1 else {
            folds.append(
                TimelineFold(id: foldID(entries[at]), lead: entries[at], repeats: [], summary: nil))
            at += 1
            continue
        }
        folds.append(folded(Array(entries[at..<(at + period * cycles)]), period: period, cycles: cycles))
        at += period * cycles
    }
    return folds
}

// A cycle of more than four kinds is no longer a rhythm a reader can hold.
private let longestPeriod = 4

private func cycle(_ keys: [FoldKey?], from start: Int, minimumCycles: Int) -> (Int, Int) {
    for period in 1...longestPeriod {
        guard start + period <= keys.count, keys[start..<(start + period)].allSatisfy({ $0 != nil })
        else { break }
        var cycles = 1
        while repeatsAt(keys, start: start, next: start + cycles * period, period: period) {
            cycles += 1
        }
        if cycles >= minimumCycles { return (period, cycles) }
    }
    return (1, 1)
}

private func repeatsAt(_ keys: [FoldKey?], start: Int, next: Int, period: Int) -> Bool {
    guard next + period <= keys.count else { return false }
    return (0..<period).allSatisfy { keys[next + $0] != nil && keys[next + $0] == keys[start + $0] }
}

private func folded(_ stretch: [TimelineEntry], period: Int, cycles: Int) -> TimelineFold {
    let lead = stretch[0]
    let starts = stride(from: 0, to: stretch.count, by: period).map { stretch[$0].observedAt }
    var parts = ["x\(cycles)"]
    if let cadence = runCadence(openedAt: starts) { parts.append("every \(cadence)") }
    parts.append("first \(clockTime(lead.observedAt))")
    parts.append("last \(clockTime(stretch[stretch.count - 1].observedAt))")
    return TimelineFold(
        id: foldID(lead), lead: lead, repeats: Array(stretch.dropFirst()),
        summary: parts.joined(separator: ", "))
}

// The daemon orders by the observed time and never writes two rows of one
// kind at one instant for one container.
private func foldID(_ entry: TimelineEntry) -> String {
    "\(entry.observedAt.raw)/\(entry.kind.rawValue)"
}

/// FoldKey is what makes one entry a repeat of another; the times are outside
/// it, because a cycle is the same thing happening again.
private struct FoldKey: Hashable {
    let kind: TimelineKind
    let containerName: String?
    let state: ContainerState?
    let reason: String?
    let eventReason: String?
    let conditionType: String?
    let conditionStatus: String?
}

private func foldKey(_ entry: TimelineEntry) -> FoldKey? {
    switch entry.kind {
    case .lifecycle, .rollout, .cut: return nil
    case .containerTransition, .condition, .event, .capture:
        return FoldKey(
            kind: entry.kind, containerName: entry.containerName, state: entry.state,
            reason: entry.reason, eventReason: entry.eventReason,
            conditionType: entry.conditionType, conditionStatus: entry.conditionStatus)
    }
}
