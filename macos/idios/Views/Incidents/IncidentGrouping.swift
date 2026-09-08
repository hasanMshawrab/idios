import IdiosModel
import SwiftUI

/// IncidentClusterGroup is the outer level of the incidents list: one cluster
/// bucket, or the single unnamed bucket that keeps the list one shape when the
/// mode groups by something else.
struct IncidentClusterGroup: Identifiable {
    let id: String
    let title: String
    let dotStyle: Color?
    let groups: [IncidentGroup]
}

/// IncidentGroup is one section of the incidents list.
struct IncidentGroup: Identifiable {
    let id: String
    let kind: String
    let title: String
    let meta: String
    let help: String
    let openCount: Int
    let worstCategory: IdiosModel.Category?
    let newestSeen: Timestamp?
    let rows: [Incident]
    let entries: [GroupEntry]
    /// factsKey addresses the workload whose live pods and run totals the
    /// summary needs, nil for a group that is not one workload.
    let factsKey: WorkloadKey?
    let summary: String
    let badge: GroupBadge
    /// scope marks a header that names the cluster or the namespace the rows
    /// below it are in rather than the problem they share; it is drawn as a
    /// section title over the other headers.
    let scope: Bool
}

extension IncidentGroup {
    /// headerless is a group that says nothing its one row does not already
    /// say: a bare pod, a Job with one run, a single-pod workload. A CronJob
    /// keeps its header because its "+ N more runs" line belongs under one.
    var headerless: Bool { !kind.isEmpty && kind != "CronJob" && entries.count == 1 }
}

/// workloadTitle is the name a row is filed under: a bare pod has no workload
/// name, and the pod is what a person recognises.
func workloadTitle(_ incident: Incident) -> String {
    if !incident.workloadName.isEmpty { return incident.workloadName }
    return incident.podName ?? incident.id
}

/// workloadKindLabel is the kind as a person reads it; the daemon stores "none"
/// for a pod with no controller.
func workloadKindLabel(_ incident: Incident) -> String {
    incident.workloadKind == "none" ? "Pod" : incident.workloadKind
}

/// incidentGroups divides rows into the sections the Group popup asks for,
/// keeping the order the daemon returned inside each one.
func incidentGroups(
    rows: [Incident], grouping: Grouping, cluster: (String) -> Cluster?, selectedClusters: Int,
    facts: (WorkloadKey) -> GroupFacts?
) -> [IncidentClusterGroup] {
    switch grouping {
    case .workload:
        // A second cluster in scope makes the cluster the first thing a
        // workload name needs; with one cluster it would only repeat itself.
        guard selectedClusters > 1 else {
            return [
                unnamedBucket(
                    workloadGroups(
                        rows, cluster: cluster, selectedClusters: selectedClusters,
                        underCluster: false, facts: facts))
            ]
        }
        return clusterBuckets(rows, cluster: cluster) { clusterRows in
            workloadGroups(
                clusterRows, cluster: cluster, selectedClusters: selectedClusters,
                underCluster: true, facts: facts)
        }
    case .namespace:
        return [
            unnamedBucket(
                buckets(rows) { "\($0.clusterID)/\($0.namespace)" }.map { bucket in
                    let first = bucket.rows[0]
                    // A namespace name is only unique inside its cluster, so
                    // the heading names both whatever the scope is.
                    return group(
                        id: bucket.key, kind: "",
                        title: scopePrefix(
                            clusterName: clusterName(first.clusterID, cluster),
                            namespace: first.namespace, selectedClusterCount: 2),
                        meta: "", help: "", rows: bucket.rows, scope: true)
                })
        ]
    case .category:
        return [
            unnamedBucket(
                buckets(rows) { $0.category.rawValue }.map { bucket in
                    let category = bucket.rows[0].category
                    return group(
                        id: bucket.key, kind: "", title: category.label, meta: "",
                        help: category.rawValue, rows: bucket.rows)
                })
        ]
    case .cluster:
        return clusterBuckets(rows, cluster: cluster) { clusterRows in
            [
                group(
                    id: "\(clusterRows[0].clusterID)/all", kind: "", title: "", meta: "",
                    help: "", rows: clusterRows)
            ]
        }
    case .time:
        return [
            unnamedBucket(
                // The daemon orders by last_seen_at descending, so a day is a
                // cut in the served order rather than a sort of its own.
                buckets(rows) { String($0.lastSeenAt.raw.prefix(10)) }.map { bucket in
                    group(
                        id: bucket.key, kind: "", title: bucket.key, meta: "UTC", help: "",
                        rows: bucket.rows)
                })
        ]
    }
}

private func workloadGroups(
    _ rows: [Incident], cluster: (String) -> Cluster?, selectedClusters: Int, underCluster: Bool,
    facts: (WorkloadKey) -> GroupFacts?
) -> [IncidentGroup] {
    buckets(rows) { incident in
        // A bare pod has no workload name; its own uid is what keeps two such
        // pods apart.
        let key =
            incident.workloadName.isEmpty
            ? (incident.podUID ?? incident.id) : incident.workloadName
        return "\(incident.clusterID)/\(incident.namespace)/\(incident.workloadKind)/\(key)"
    }
    .map { bucket in
        let first = bucket.rows[0]
        let meta =
            underCluster
            ? first.namespace
            : scopePrefix(
                clusterName: clusterName(first.clusterID, cluster), namespace: first.namespace,
                selectedClusterCount: selectedClusters)
        // A bare pod has no workload to read facts about.
        let key =
            first.workloadName.isEmpty
            ? nil
            : WorkloadKey(
                cluster: first.clusterID, namespace: first.namespace, kind: first.workloadKind,
                name: first.workloadName)
        return group(
            id: bucket.key, kind: workloadKindLabel(first), title: workloadTitle(first),
            meta: meta, help: "", rows: bucket.rows, factsKey: key,
            summary: groupSummary(
                kind: first.workloadKind, rows: bucket.rows, facts: key.flatMap(facts)))
    }
}

private func clusterBuckets(
    _ rows: [Incident], cluster: (String) -> Cluster?, groups: ([Incident]) -> [IncidentGroup]
) -> [IncidentClusterGroup] {
    buckets(rows) { $0.clusterID }
        .map { bucket in
            let row = cluster(bucket.key)
            return IncidentClusterGroup(
                id: bucket.key, title: row?.name ?? bucket.key,
                dotStyle: row.map { clusterDot(ready: $0.ready, hasError: $0.lastError != nil) },
                groups: groups(bucket.rows))
        }
        .sorted { $0.title < $1.title }
}

private func unnamedBucket(_ groups: [IncidentGroup]) -> IncidentClusterGroup {
    IncidentClusterGroup(id: "all", title: "", dotStyle: nil, groups: groups)
}

private func clusterName(_ clusterID: String, _ cluster: (String) -> Cluster?) -> String {
    cluster(clusterID)?.name ?? clusterID
}

private func group(
    id: String, kind: String, title: String, meta: String, help: String,
    rows: [Incident], factsKey: WorkloadKey? = nil, summary: String? = nil,
    scope: Bool = false
) -> IncidentGroup {
    let open = rows.filter { $0.closedAt == nil }
    return IncidentGroup(
        id: id, kind: kind, title: title, meta: meta, help: help, openCount: open.count,
        worstCategory: open.map(\.category).min { $0.rank < $1.rank },
        // The stored layout is fixed-width UTC, so string order is time order.
        newestSeen: rows.map(\.lastSeenAt).max { $0.raw < $1.raw },
        rows: rows, entries: groupEntries(rows, groupID: id), factsKey: factsKey,
        summary: summary ?? countsSummary(rows: rows, open: open.count), badge: groupBadge(rows),
        scope: scope)
}

// A group that is not one workload has no facts to read: what it can say is
// how much it holds.
private func countsSummary(rows: [Incident], open: Int) -> String {
    let incidents = rows.count == 1 ? "1 incident" : "\(rows.count) incidents"
    return "\(incidents), \(open) open"
}
