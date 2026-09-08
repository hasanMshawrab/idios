import Foundation
import IdiosModel
import Observation

/// Grouping is how the incidents list divides its rows into sections.
enum Grouping: String, CaseIterable, Sendable {
    case workload
    case namespace
    case category
    case cluster
    case time

    /// title names the mode in the Group popup.
    var title: String {
        switch self {
        case .workload: "Workload"
        case .namespace: "Namespace"
        case .category: "Category"
        case .cluster: "Cluster"
        case .time: "Time"
        }
    }
}

/// Density is how much a list row says without a hover: Compact is one line,
/// Comfortable keeps the second line open.
enum Density: String, CaseIterable, Sendable {
    case compact
    case comfortable

    /// title names the density in the View menu.
    var title: String {
        switch self {
        case .compact: "Compact"
        case .comfortable: "Comfortable"
        }
    }
}

/// Preferences is the UI state that outlives a launch: the daemon address, the
/// cluster scope, the grouping mode, the row density and remembered column
/// widths.
@Observable @MainActor
final class Preferences {
    private let defaults: UserDefaults

    /// daemonAddress is the host and port the application talks to.
    var daemonAddress: String {
        didSet { defaults.set(daemonAddress, forKey: Key.daemonAddress) }
    }

    /// scope is the set of clusters every list is filtered by.
    var scope: ClusterScope {
        didSet {
            defaults.set(scope.selected.sorted(), forKey: Key.clusterScope)
            defaults.set(scope.isAll, forKey: Key.clusterScopeAll)
        }
    }

    /// grouping is the section mode of the incidents list.
    var grouping: Grouping {
        didSet { defaults.set(grouping.rawValue, forKey: Key.grouping) }
    }

    /// density is how many lines a list row draws.
    var density: Density {
        didSet { defaults.set(density.rawValue, forKey: Key.density) }
    }

    /// columnWidths remembers a width per table, keyed by table name.
    var columnWidths: [String: Double] {
        didSet { defaults.set(columnWidths, forKey: Key.columnWidths) }
    }

    /// init reads the stored values, or their defaults on a first launch.
    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        self.daemonAddress =
            Preferences.addressOverride
            ?? defaults.string(forKey: Key.daemonAddress) ?? Preferences.defaultAddress
        // An install that predates the flag stored an empty array for the
        // all-clusters default and a non-empty one for a narrowed scope, so
        // an absent flag takes its meaning from the array.
        let selected = defaults.stringArray(forKey: Key.clusterScope) ?? []
        let all = defaults.object(forKey: Key.clusterScopeAll) as? Bool ?? selected.isEmpty
        self.scope = all ? .all : ClusterScope(selected: Set(selected))
        self.grouping =
            Grouping(rawValue: defaults.string(forKey: Key.grouping) ?? "") ?? .workload
        self.density =
            Density(rawValue: defaults.string(forKey: Key.density) ?? "") ?? .compact
        self.columnWidths = defaults.dictionary(forKey: Key.columnWidths) as? [String: Double] ?? [:]
    }

    /// defaultAddress is the daemon's own api_listen default.
    static let defaultAddress = "127.0.0.1:7770"

    // A launch argument beats the stored address and is not written back, so a
    // screenshot run against another port leaves the user's preference alone.
    private static var addressOverride: String? {
        let args = CommandLine.arguments
        guard let flag = args.firstIndex(of: "-daemon"), flag + 1 < args.count else { return nil }
        return args[flag + 1]
    }

    private enum Key {
        static let daemonAddress = "daemonAddress"
        static let clusterScope = "clusterScope"
        static let clusterScopeAll = "clusterScopeAll"
        static let grouping = "grouping"
        static let density = "density"
        static let columnWidths = "columnWidths"
    }
}
