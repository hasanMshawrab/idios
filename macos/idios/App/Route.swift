import Foundation
import IdiosModel
import Observation

extension Route {
    /// launched is the route named on the command line, parsed before the
    /// window draws so a screenshot run opens on the screen it was asked for.
    static var launched: Route? {
        let args = CommandLine.arguments
        guard let flag = args.firstIndex(of: "-route"), flag + 1 < args.count else { return nil }
        return Route(path: args[flag + 1])
    }
}

/// GroupTarget is a workload group a menu bar row asks the list to show: the
/// group's id as the list computes it, and the cluster it is in, because a
/// narrowed scope has to widen before the group exists.
struct GroupTarget: Hashable {
    let id: String
    let clusterID: String
}

/// Navigator carries a route from outside the main window - the menu bar extra
/// - to the screen that can show it.
@Observable @MainActor
final class Navigator {
    private(set) var route: Route?

    /// serial changes on every request, so asking twice for the same route
    /// still reaches the window.
    private(set) var serial = 0

    /// backSerial changes on every Back request, for the same reason serial
    /// does: the screen watches it rather than a value that could repeat.
    private(set) var backSerial = 0

    /// open asks the main window for a route.
    func open(_ route: Route) {
        self.route = route
        serial += 1
    }

    /// forwardSerial changes on every Forward request, for the same reason
    /// serial does: the screen watches it rather than a value that could
    /// repeat.
    private(set) var forwardSerial = 0

    /// goBack asks the main window to leave the route on top of its stack.
    func goBack() {
        backSerial += 1
    }

    /// goForward asks the main window to return to the route Back left.
    func goForward() {
        forwardSerial += 1
    }

    private(set) var group: GroupTarget?

    /// groupSerial changes on every request, for the same reason serial does:
    /// the screen watches it rather than a value that could repeat.
    private(set) var groupSerial = 0

    /// open asks the main window to show one workload group in the list.
    func open(group: GroupTarget) {
        self.group = group
        groupSerial += 1
    }
}
