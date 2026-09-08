import Foundation
import IdiosAPI

/// Cluster is one watched cluster and the runtime state of its watch.
public struct Cluster: Identifiable, Hashable, Sendable {
    public let id: String
    public let identity: String?
    public let name: String
    public let contextName: String?
    public let apiServerURL: String?
    public let firstSeenAt: Timestamp?
    public let lastConnectedAt: Timestamp?
    public let lastError: String?
    public let lastErrorAt: Timestamp?
    public let namespaces: [String]
    public let ready: Bool
    public let lastEventAt: Timestamp?
    public let skewSeconds: Double?
    public let grafanaURL: String
    public let lokiDatasourceUID: String
    public let logSelector: String
}

extension Cluster {
    /// init builds a cluster from its wire row.
    public init(wire: Components.Schemas.Cluster) throws {
        self.id = try require(wire.id, "id")
        self.identity = wire.identity
        self.name = try require(wire.name, "name")
        self.contextName = wire.contextName
        self.apiServerURL = wire.apiServerUrl
        self.firstSeenAt = wire.firstSeenAt.map(Timestamp.init)
        self.lastConnectedAt = wire.lastConnectedAt.map(Timestamp.init)
        self.lastError = wire.lastError
        self.lastErrorAt = wire.lastErrorAt.map(Timestamp.init)
        self.namespaces = wire.namespaces ?? []
        self.ready = wire.ready ?? false
        self.lastEventAt = wire.lastEventAt.map(Timestamp.init)
        self.skewSeconds = wire.skewSeconds
        self.grafanaURL = wire.grafanaUrl ?? ""
        self.lokiDatasourceUID = wire.lokiDatasourceUid ?? ""
        self.logSelector = wire.logSelector ?? ""
    }
}

extension Page where Row == Cluster {
    /// init builds a page of clusters from its wire response.
    public init(wire: Components.Schemas.ClustersResponse) throws {
        self.init(
            rows: try (wire.clusters ?? []).map(Cluster.init(wire:)), truncated: wire.truncated ?? false)
    }
}

/// ClusterScope is the set of clusters the screens are looking at.
public struct ClusterScope: Hashable, Sendable {
    /// all is true while every cluster is in scope, whatever selected holds.
    public var all: Bool

    /// selected holds the cluster row ids in scope while all is false.
    public var selected: Set<String>

    /// all is the default scope: every cluster the daemon reports.
    public static let all = ClusterScope(all: true, selected: [])

    /// init starts from an explicit selection, which may be empty.
    public init(selected: Set<String>) {
        self.init(all: false, selected: selected)
    }

    private init(all: Bool, selected: Set<String>) {
        self.all = all
        self.selected = selected
    }

    /// isAll is true while no cluster narrows the screens.
    public var isAll: Bool { all }

    /// isEmpty is true for a selection that names no cluster, which puts no
    /// cluster in scope.
    public var isEmpty: Bool { !all && selected.isEmpty }

    /// includes says whether a cluster is in scope.
    public func includes(_ clusterID: String) -> Bool {
        all || selected.contains(clusterID)
    }

    /// reconciled drops clusters the daemon no longer reports; the all scope
    /// stays all, and a selection left with nothing stays empty.
    public func reconciled(with clusters: [Cluster]) -> ClusterScope {
        guard !all else { return .all }
        return ClusterScope(selected: selected.intersection(clusters.map(\.id)))
    }
}

extension ClusterScope {
    /// including widens a narrowed scope to hold one more cluster; the all
    /// scope already holds every one and is returned unchanged.
    public func including(_ clusterID: String) -> ClusterScope {
        guard !all else { return .all }
        return ClusterScope(selected: selected.union([clusterID]))
    }
}
