/// BarePodRow is one name among the pods no controller owns: the live pod's
/// row and the kept rows of deleted pods of the same name, read as one line.
public struct BarePodRow: Identifiable, Hashable, Sendable {
    public let id: String
    public let name: String
    public let rows: [Workload]
    public let openIncidents: Int32
}

extension BarePodRow {
    /// podCount is how many pods of this name idios still keeps.
    public var podCount: Int { rows.count }

    /// live is the row whose pod is still in the cluster, nil when every
    /// kept pod of the name is gone.
    public var live: Workload? { rows.first { $0.livePods > 0 } }

    /// needsChooser is true when the row names more than one kept pod, so
    /// opening it has to ask which.
    public var needsChooser: Bool { rows.count > 1 }
}

/// barePodRows merges the workload rows of kind none by pod name, so five
/// rows of one name are one line that says how many pods it stands for.
public func barePodRows(_ rows: [Workload]) -> [BarePodRow] {
    var order: [String] = []
    var byName: [String: [Workload]] = [:]
    for row in rows where row.workloadKind == "none" {
        let name = row.podName ?? ""
        // Two pods idios cannot name are not one pod.
        let key = name.isEmpty ? "uid/\(row.podUID ?? "")" : "name/\(name)"
        if byName[key] == nil { order.append(key) }
        byName[key, default: []].append(row)
    }
    return order.map { key in
        let bucket = (byName[key] ?? []).sorted { a, b in
            if (a.livePods > 0) != (b.livePods > 0) { return a.livePods > 0 }
            return (a.podUID ?? "") < (b.podUID ?? "")
        }
        let first = bucket[0]
        let name = first.podName ?? ""
        return BarePodRow(
            id: "\(first.clusterID)/\(first.namespace)/none/"
                + (name.isEmpty ? (first.podUID ?? "") : name),
            name: name, rows: bucket, openIncidents: bucket.reduce(0) { $0 + $1.openIncidents })
    }
}

/// kindCaption is the tree's heading over one kind: the kind in the plural
/// and how many rows stand under it, because the caption names the kind and
/// not the row.
public func kindCaption(kind: String, count: Int) -> String {
    "\(kind == "none" ? "Bare pods" : kind + "s") \(count)"
}

/// ClusterGroup is one cluster of the workload tree and the namespaces under it.
public struct ClusterGroup: Identifiable, Hashable, Sendable {
    public let clusterID: String
    public let namespaces: [NamespaceGroup]

    /// init builds the group from its cluster id and its namespaces.
    public init(clusterID: String, namespaces: [NamespaceGroup]) {
        self.clusterID = clusterID
        self.namespaces = namespaces
    }

    public var id: String { clusterID }

    /// openIncidents is what the namespaces under the cluster hold open.
    public var openIncidents: Int32 { namespaces.reduce(0) { $0 + $1.openIncidents } }
}

/// NamespaceGroup is one namespace of a cluster and the kinds under it.
public struct NamespaceGroup: Identifiable, Hashable, Sendable {
    public let clusterID: String
    public let namespace: String
    public let kinds: [KindGroup]

    /// init builds the group from its namespace and the kinds under it.
    public init(clusterID: String, namespace: String, kinds: [KindGroup]) {
        self.clusterID = clusterID
        self.namespace = namespace
        self.kinds = kinds
    }

    public var id: String { "\(clusterID)/\(namespace)" }

    /// openIncidents is what the kinds under the namespace hold open.
    public var openIncidents: Int32 { kinds.reduce(0) { $0 + $1.openIncidents } }
}

/// KindGroup is one workload kind of a namespace and the rows under its caption.
public struct KindGroup: Identifiable, Hashable, Sendable {
    public let clusterID: String
    public let namespace: String
    public let kind: String
    public let rows: [Workload]

    /// init builds the group from its kind and the rows of that kind.
    public init(clusterID: String, namespace: String, kind: String, rows: [Workload]) {
        self.clusterID = clusterID
        self.namespace = namespace
        self.kind = kind
        self.rows = rows
    }

    public var id: String { "\(clusterID)/\(namespace)/\(kind)" }

    /// openIncidents is what the rows under the caption hold open.
    public var openIncidents: Int32 { rows.reduce(0) { $0 + $1.openIncidents } }

    /// bareRows are the merged pod names of a kind-none group, empty otherwise.
    public var bareRows: [BarePodRow] { kind == "none" ? barePodRows(rows) : [] }

    /// workloadRows are the rows of a controlled kind, empty for kind none.
    public var workloadRows: [Workload] { kind == "none" ? [] : rows }

    /// captionCount is how many lines the caption heads; the pods no controller
    /// owns are read one row per name, so it counts names and not pods.
    public var captionCount: Int { kind == "none" ? bareRows.count : rows.count }
}

/// WorkloadTreeRow is one line of the flattened workload tree.
public enum WorkloadTreeRow: Identifiable, Hashable, Sendable {
    case cluster(ClusterGroup)
    case namespace(NamespaceGroup)
    case kind(KindGroup)
    case barePod(BarePodRow)
    case workload(Workload)

    public var id: String {
        switch self {
        case .cluster(let group): return clusterRowID(group.clusterID)
        case .namespace(let group): return group.id
        case .kind(let group): return group.id
        case .barePod(let row): return row.id
        case .workload(let row): return row.id
        }
    }

    /// level is how far the row is indented: clusters at the root, namespaces
    /// one step in, captions and the rows they head two.
    public var level: Int {
        switch self {
        case .cluster: return 0
        case .namespace: return 1
        case .kind, .barePod, .workload: return 2
        }
    }
}

/// clusterRowID is the id a cluster group is collapsed by; it is prefixed
/// because a cluster id and a namespace id are otherwise the same shape.
public func clusterRowID(_ clusterID: String) -> String { "cluster/" + clusterID }

/// flatWorkloadTree reads the groups as one list of lines, leaving out what
/// the collapsed groups hide.
public func flatWorkloadTree(_ groups: [ClusterGroup], collapsed: Set<String>)
    -> [WorkloadTreeRow]
{
    var rows: [WorkloadTreeRow] = []
    for cluster in groups {
        rows.append(.cluster(cluster))
        guard !collapsed.contains(clusterRowID(cluster.clusterID)) else { continue }
        for group in cluster.namespaces {
            rows.append(.namespace(group))
            guard !collapsed.contains(group.id) else { continue }
            for kind in group.kinds {
                rows.append(.kind(kind))
                rows.append(contentsOf: kind.bareRows.map(WorkloadTreeRow.barePod))
                rows.append(contentsOf: kind.workloadRows.map(WorkloadTreeRow.workload))
            }
        }
    }
    return rows
}
