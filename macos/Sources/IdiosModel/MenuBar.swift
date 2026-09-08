/// menuBarTail is the line under the rows the popover could not draw: how many
/// problems are left and, when every one of them is closed, the window they
/// closed inside.
public func menuBarTail(hidden: Int, allClosed: Bool, windowSeconds: Int32?) -> String? {
    guard hidden > 0 else { return nil }
    let problems = plural(hidden, "more problem")
    guard allClosed else { return problems }
    // A sentence that invents a window is worse than one that leaves it out,
    // so the window is named only once the daemon has reported it.
    guard let windowSeconds else { return "\(problems) closed" }
    return "\(problems) closed in the last \(humanDuration(seconds: windowSeconds))"
}
