import Foundation

/// podNameSuffix is the part of a pod name that its workload name does not
/// already say.
public func podNameSuffix(name: String, workloadName: String) -> String {
    guard !workloadName.isEmpty else { return name }
    let prefix = workloadName + "-"
    guard name.hasPrefix(prefix) else { return name }
    let suffix = String(name.dropFirst(prefix.count))
    return suffix.isEmpty ? name : suffix
}

/// middleElided shortens a value to the given number of kept characters by
/// dropping its middle, so that both ends stay readable.
public func middleElided(_ value: String, keeping: Int) -> String {
    guard value.count > keeping else { return value }
    let head = max((keeping + 1) / 2, 0)
    let tail = max(keeping - head, 0)
    return String(value.prefix(head)) + "..." + String(value.suffix(tail))
}

/// scopePrefix names the cluster in front of the namespace only while more than
/// one cluster is in scope.
public func scopePrefix(clusterName: String, namespace: String, selectedClusterCount: Int)
    -> String
{
    selectedClusterCount > 1 ? "\(clusterName) / \(namespace)" : namespace
}

/// byteCount renders a byte count for a person.
public func byteCount(_ n: Int64) -> String {
    ByteCountFormatter.string(fromByteCount: n, countStyle: .file)
}

/// plural writes a count with its noun under the one rule English keeps:
/// one is singular and every other number, zero included, is not.
public func plural(_ count: Int, _ noun: String) -> String {
    "\(count) \(noun)\(count == 1 ? "" : "s")"
}

/// durationText renders an elapsed time in the two coarsest units it reaches.
public func durationText(from: Date, to: Date) -> String {
    let minutes = Int(to.timeIntervalSince(from) / 60)
    guard minutes >= 1 else { return "<1m" }
    guard minutes >= 60 else { return "\(minutes)m" }
    let hours = minutes / 60
    guard hours >= 24 else { return "\(hours)h \(minutes % 60)m" }
    return "\(hours / 24)d \(hours % 24)h"
}

/// kindRank is the bucket a workload kind sorts into: the controllers that keep
/// a pod alive first, the ones that run a pod to completion last, and a rank in
/// between for a kind this application has never heard of.
public func kindRank(_ kind: String) -> Int {
    switch kind {
    case "Deployment": 0
    case "StatefulSet": 1
    case "DaemonSet": 2
    case "CronJob": 4
    case "Job": 5
    case "none": 6
    default: 3
    }
}

/// scopeLabel is the word a row's container chip shows when the incident names
/// no container: the problem is the pod itself, or, for a Job's incident, the
/// Job itself, which never had a container of its own to name.
public func scopeLabel(containerName: String?, subjectKind: SubjectKind) -> String {
    if let containerName { return containerName }
    return subjectKind == .job ? "job" : "pod"
}

/// clockTime is the wall clock of a stored timestamp, in the UTC the daemon
/// stores; the raw string stays available on hover and on copy.
public func clockTime(_ timestamp: Timestamp, seconds: Bool = false) -> String {
    let parts = timestamp.raw.split(separator: "T")
    guard parts.count == 2 else { return timestamp.raw }
    let time = parts[1].prefix(while: { $0 != "." && $0 != "Z" })
    let fields = time.split(separator: ":")
    guard fields.count == 3 else { return timestamp.raw }
    return seconds ? String(time) : "\(fields[0]):\(fields[1])"
}

/// ResourceUnit is how a parsed resource value is spelled once the quantity the
/// kubelet wrote has been parsed.
public enum ResourceUnit: Hashable, Sendable { case bytes, millicores }

/// millicores writes a parsed cpu value the way a Kubernetes quantity spells
/// the small end of the scale, so half a core reads as 500m whatever the spec
/// said.
public func millicores(_ millis: Int64) -> String { "\(millis)m" }

/// resourceLine is one resource pair as the kubelet wrote it, with the parsed
/// value beside a quantity that does not already read as one: "1" and "1000m"
/// are the same cpu and only one of them can be compared with the other
/// container's.
public func resourceLine(
    name: String, request: String?, limit: String?,
    requestValue: Int64?, limitValue: Int64?, unit: ResourceUnit
) -> String? {
    func parsed(_ value: Int64) -> String {
        switch unit {
        case .bytes: byteCount(value)
        case .millicores: millicores(value)
        }
    }
    func side(_ label: String, _ written: String?, _ value: Int64?) -> String? {
        guard let written else {
            // A value the daemon parsed against a spec that no longer holds it
            // is still a number worth showing.
            return value.map { "\(label) \(parsed($0))" }
        }
        guard let value, parsed(value) != written else { return "\(label) \(written)" }
        return "\(label) \(written) (\(parsed(value)))"
    }
    let sides = [side("request", request, requestValue), side("limit", limit, limitValue)]
        .compactMap { $0 }
    guard !sides.isEmpty else { return nil }
    return "\(name) \(sides.joined(separator: ", "))"
}
