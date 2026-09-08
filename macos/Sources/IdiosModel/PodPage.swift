import Foundation

/// PodPane is one tab of the Pod card.
public enum PodPane: String, CaseIterable, Hashable, Sendable {
    case events, conditions, files, podJSON, related
}

/// ContainerTab is one tab of a container's pane.
public enum ContainerTab: String, CaseIterable, Hashable, Sendable {
    case overview, timeline, logs
}

/// PodSelection is what the middle pane of the pod page shows: the Pod card
/// on one of its tabs, or one container with the incident whose segment is
/// lit (nil when the container has none) and the pane's tab.
public enum PodSelection: Hashable, Sendable {
    case pod(PodPane)
    case container(name: String, incidentID: String?, tab: ContainerTab)
}

extension PodTab {
    /// pane is the Pod card tab a pod route opens: the left column is the
    /// container and incident list, so the three tabs that listed them open
    /// Events.
    public var pane: PodPane {
        switch self {
        case .containers, .incidents, .events: .events
        case .conditions: .conditions
        case .logs: .files
        case .podJSON: .podJSON
        }
    }
}

/// segmentOrder is the order of a container's incidents in the segmented
/// control: the fold order, so the segment that is lit on arrival is the
/// row the list would have led with.
public func segmentOrder(_ rows: [Incident]) -> [Incident] { rows.sorted(by: foldOrder) }

/// containerOrder puts the containers the way the pod runs them: init
/// containers first, then the app, then the sidecars, ephemeral last, by
/// name inside a kind.
public func containerOrder(_ containers: [Container]) -> [Container] {
    containers.sorted { a, b in
        if a.kind != b.kind { return kindRank(a.kind) < kindRank(b.kind) }
        return a.name < b.name
    }
}

private func kindRank(_ kind: ContainerKind) -> Int {
    switch kind {
    case .`init`: 0
    case .app: 1
    case .sidecar: 2
    case .ephemeral: 3
    }
}

/// eventContainer reads the container an event's field_path names, or nil
/// for a pod-level event; the kubelet writes spec.containers{name},
/// spec.initContainers{name} and spec.ephemeralContainers{name}.
public func eventContainer(fieldPath: String?) -> String? {
    guard let fieldPath, let open = fieldPath.firstIndex(of: "{"),
        let close = fieldPath.lastIndex(of: "}"), open < close
    else { return nil }
    let name = fieldPath[fieldPath.index(after: open)..<close]
    return name.isEmpty ? nil : String(name)
}

/// containerEvents keeps the pod's events that name the container plus
/// every pod-level event, in served order, because the event that explains
/// a death is usually the pod's (Scheduled, Killing, Evicted, Preempted).
public func containerEvents(_ events: [Event], container: String) -> [Event] {
    events.filter { event in
        let named = eventContainer(fieldPath: event.fieldPath)
        return named == nil || named == container
    }
}

/// readinessFlips counts the changes of the pod's Ready condition in the
/// history, so a header tag that would otherwise flip every few seconds can
/// say the pod is looping instead.
public func readinessFlips(_ conditions: [PodCondition]) -> Int {
    let ready = conditions.filter { $0.type == "Ready" }
        .sorted { $0.observedAt.raw < $1.observedAt.raw }
    return zip(ready, ready.dropFirst()).filter { $0.status != $1.status }.count
}

/// podStateTag is the page header's tag: DELETED for a pod that is gone,
/// LOOPING for one whose readiness keeps flipping, else the phase and, when
/// a Ready condition was read, READY or NOT READY.
public func podStateTag(phase: String, deleted: Bool, ready: Bool?, flips: Int) -> String {
    if deleted { return "DELETED" }
    if flips > 3 { return "\(phase.uppercased()), LOOPING" }
    switch ready {
    case .some(true): return "\(phase.uppercased()), READY"
    case .some(false): return "\(phase.uppercased()), NOT READY"
    case .none: return phase.uppercased()
    }
}

/// containerStateLine is the one line a container card says under its name:
/// the state, its reason or readiness, and what the kubelet counted.
public func containerStateLine(_ container: Container) -> String {
    var parts = [container.state.rawValue]
    switch container.state {
    case .running: parts.append(container.ready ? "ready" : "not ready")
    case .waiting, .terminated: if let reason = container.reason { parts.append(reason) }
    }
    if container.state == .terminated, let exit = container.exitCode {
        parts.append("exit \(exit)")
    } else {
        parts.append(plural(Int(container.restartCount), "restart"))
    }
    return parts.joined(separator: " - ")
}

/// SiblingRow is one line of the rail's siblings: the page's own pod first,
/// then the daemon's five.
public struct SiblingRow: Hashable, Sendable {
    public let uid: String
    public let name: String
    public let isThisPod: Bool
    public let deleted: Bool
    public let ready: Bool
    public let category: Category?
}

/// siblingRows puts the page's pod at the top of the daemon's siblings,
/// so the rail always shows where this pod stands among them.
public func siblingRows(pod: PodRow, ready: Bool?, siblings: [SiblingPod]) -> [SiblingRow] {
    [SiblingRow(
        uid: pod.uid, name: pod.name, isThisPod: true, deleted: pod.deletedAt != nil,
        ready: ready ?? false, category: nil)]
        + siblings.map {
            SiblingRow(
                uid: $0.uid, name: $0.name, isThisPod: false, deleted: $0.deletedAt != nil,
                ready: $0.ready, category: $0.worstOpenCategory)
        }
}

/// moreSiblings is how many pods of the controller the rail does not show:
/// the total counts this pod, so it and the shown siblings come off.
public func moreSiblings(total: Int32, shown: Int) -> Int {
    max(Int(total) - 1 - shown, 0)
}

/// runningSinceText is when the container that is running now started, which is
/// not when the pod started once a container has restarted.
public func runningSinceText(_ container: Container, now: Date) -> String? {
    guard container.state == .running, let since = container.runningSince else { return nil }
    guard let from = since.date else { return "since \(clockTime(since))" }
    return "since \(clockTime(since)) (\(durationText(from: from, to: now)))"
}

/// nodeLine is the node the pod is on, and the node the lit incident opened on
/// when the pod has since been placed somewhere else.
public func nodeLine(podNode: String?, incidentNode: String, deleted: Bool) -> String {
    let prefix = deleted ? "was on " : ""
    guard let podNode else {
        guard !incidentNode.isEmpty else { return "not scheduled" }
        return "\(prefix)\(incidentNode) (at open; the pod is no longer placed)"
    }
    guard !incidentNode.isEmpty, incidentNode != podNode else { return "\(prefix)\(podNode)" }
    return "\(prefix)\(podNode) (opened on \(incidentNode))"
}
