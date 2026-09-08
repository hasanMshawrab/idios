/// Rollup is the rows of two or more pods of one workload that share a
/// container and a category inside one group: replica fan-out of one
/// problem, read as one line.
public struct Rollup: Identifiable, Hashable, Sendable {
    public let id: String
    public let containerName: String?
    public let category: Category
    public let folds: [PodFold]

    public init(id: String, containerName: String?, category: Category, folds: [PodFold]) {
        self.id = id
        self.containerName = containerName
        self.category = category
        self.folds = folds
    }
}

extension Rollup {
    /// rows is every incident folded here, each pod's lead before its
    /// siblings, pods in served order.
    public var rows: [Incident] { folds.flatMap { [$0.lead] + $0.siblings } }

    /// openCount is how many of the rows are still open.
    public var openCount: Int { rows.filter { $0.closedAt == nil }.count }

    /// reasons is every last reason among the rows, once each, first seen
    /// first.
    public var reasons: [String] { distinct(rows.map(\.lastReason)) }

    /// exitCodes is every exit code among the rows, once each, first seen
    /// first.
    public var exitCodes: [Int32] { distinct(rows.compactMap(\.exitCode)) }

    /// imageTags is every image tag among the rows, once each, first seen
    /// first.
    public var imageTags: [String] { distinct(rows.compactMap(\.imageTag)) }
}

/// GroupEntry is one line of a group: one Job's run, one pod's fold, or the
/// rollup of several pods.
public enum GroupEntry: Identifiable, Hashable, Sendable {
    case run(RunFold)
    case fold(PodFold)
    case rollup(Rollup)

    public var id: String {
        switch self {
        case .run(let run): run.id
        case .fold(let fold): fold.id
        case .rollup(let rollup): rollup.id
        }
    }
}

/// groupEntries divides the rows of one group into runs, rollups and pod
/// folds: a job-subject incident folds into its run, the rows of two or more
/// pods of one workload sharing a container and a category roll up, every
/// other row folds by pod.
public func groupEntries(_ rows: [Incident], groupID: String) -> [GroupEntry] {
    let ofRun = { (row: Incident) in
        (row.workloadKind == "CronJob" || row.workloadKind == "Job") && row.jobUID != nil
    }
    let rest = rows.filter { !ofRun($0) }
    var served: [String: Int] = [:]
    for (position, row) in rows.enumerated() { served[row.id] = position }

    // A job-subject row has no pod and a bare pod has no replicas, so
    // neither is ever fan-out.
    let candidates = rest.filter { $0.podUID != nil && !$0.workloadName.isEmpty }
    let rolled = Set(
        buckets(candidates) { rollupKey($0, groupID: groupID) }
            .filter { Set($0.rows.compactMap(\.podUID)).count >= 2 }
            .map(\.key))
    let placed =
        runFolds(rows.filter(ofRun)).map { (served[$0.rows[0].id] ?? 0, GroupEntry.run($0)) }
        + buckets(rest) { row in
            let key = rollupKey(row, groupID: groupID)
            // The pod count above was taken over candidates only; a row that is
            // not one must never ride a key that reached two pods on other rows.
            if rolled.contains(key), row.podUID != nil, !row.workloadName.isEmpty { return key }
            return row.podUID.map { "pod/\($0)" } ?? "incident/\(row.id)"
        }
        .map { bucket -> (Int, GroupEntry) in
            let position = served[bucket.rows[0].id] ?? 0
            guard rolled.contains(bucket.key) else {
                return (position, .fold(podFolds(bucket.rows)[0]))
            }
            let first = bucket.rows[0]
            return (
                position,
                .rollup(
                    Rollup(
                        id: bucket.key, containerName: first.containerName,
                        category: first.category, folds: podFolds(bucket.rows)))
            )
        }
    return placed.sorted { $0.0 < $1.0 }.map(\.1)
}

// last_reason is not in the key: a crash loop alternates Error and
// CrashLoopBackOff on every occurrence, and a rollup that formed and
// dissolved as the loops went in and out of phase would be worse than
// none. The group id keeps two workloads' rollups apart when the list is
// grouped by something other than the workload.
private func rollupKey(_ row: Incident, groupID: String) -> String {
    "\(groupID)/rollup/\(row.clusterID)/\(row.namespace)/\(row.workloadKind)/"
        + "\(row.workloadName)/\(row.containerName ?? "")/\(row.category.rawValue)"
}

func distinct<T: Hashable>(_ values: [T]) -> [T] {
    var seen: Set<T> = []
    return values.filter { seen.insert($0).inserted }
}
