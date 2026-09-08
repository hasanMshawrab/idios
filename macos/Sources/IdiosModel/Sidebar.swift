extension IncidentState {
    /// closedStates is what the sidebar's Closed disclosure holds, in its order.
    public static let closedStates: [IncidentState] = [
        .recovered, .podDeleted, .jobFinished, .manual, .dismissed,
    ]
}

/// closedCount is the Closed disclosure's own number: every state a row leaves
/// the open list by, dismissed included because it outranks the rest.
public func closedCount(_ counts: IncidentCounts) -> Int32 {
    IncidentState.closedStates.reduce(0) { $0 + (counts.byState[$1] ?? 0) }
}

/// CategoryRows is the Category section: the categories drawn as rows and how
/// many are folded into the "more with none" line.
public struct CategoryRows: Hashable, Sendable {
    public let shown: [Category]
    public let hidden: Int
}

/// categoryRows keeps a category as a row while it counts anything or is the
/// active filter, so the row a person clicked never vanishes under them; with
/// no counts yet, every category is a row.
public func categoryRows(counts: IncidentCounts?, active: Category?) -> CategoryRows {
    let order = Category.ranked + [.other]
    guard let counts else { return CategoryRows(shown: order, hidden: 0) }
    let shown = order.filter { (counts.byCategory[$0] ?? 0) > 0 || $0 == active }
    return CategoryRows(shown: shown, hidden: order.count - shown.count)
}

/// humanDuration renders a configured number of seconds in the coarsest unit
/// that divides it.
public func humanDuration(seconds: Int32) -> String {
    if seconds >= 3600, seconds % 3600 == 0 { return "\(seconds / 3600) h" }
    if seconds >= 60, seconds % 60 == 0 { return "\(seconds / 60) min" }
    return "\(seconds) s"
}
