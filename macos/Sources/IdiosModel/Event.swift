import Foundation
import IdiosAPI

/// Event is one Kubernetes event idios kept.
public struct Event: Identifiable, Hashable, Sendable {
    public let id: String
    public let clusterID: String
    public let eventUID: String
    public let namespace: String
    public let type: String
    public let involvedKind: String?
    public let involvedName: String?
    public let involvedUID: String?
    public let fieldPath: String?
    public let reason: String
    public let message: String?
    public let sourceComponent: String?
    public let count: Int32
    public let firstTS: Timestamp?
    public let lastTS: Timestamp?
    public let category: Category?
    public let incidentID: String?
}

extension Event {
    /// init builds an event from its wire row.
    public init(wire: Components.Schemas.K8sEvent) throws {
        self.id = try require(wire.id, "id")
        self.clusterID = try require(wire.clusterId, "clusterId")
        self.eventUID = try require(wire.eventUid, "eventUid")
        self.namespace = try require(wire.namespace, "namespace")
        self.type = wire._type ?? ""
        self.involvedKind = wire.involvedKind
        self.involvedName = wire.involvedName
        self.involvedUID = wire.involvedUid
        self.fieldPath = wire.fieldPath
        self.reason = wire.reason ?? ""
        self.message = wire.message
        self.sourceComponent = wire.sourceComponent
        self.count = wire.count ?? 0
        self.firstTS = wire.firstTs.map(Timestamp.init)
        self.lastTS = wire.lastTs.map(Timestamp.init)
        self.category = modelEnum(wire.category)
        self.incidentID = wire.incidentId
    }
}

extension Page where Row == Event {
    /// init builds a page of events from its wire response.
    public init(wire: Components.Schemas.EventsResponse) throws {
        self.init(
            rows: try (wire.events ?? []).map(Event.init(wire:)), truncated: wire.truncated ?? false)
    }
}

/// eventSpan is how long a repeated event went on: Kubernetes keeps one row per
/// reason and counts the repeats, so the row's own stamp is only the last of
/// them.
public func eventSpan(count: Int32, firstTS: Timestamp?, lastTS: Timestamp?) -> String? {
    guard count >= 2, let firstTS, firstTS.raw != lastTS?.raw else { return nil }
    let first = "first \(clockTime(firstTS))"
    // A stamp idios kept but cannot read is still a first time.
    guard let from = firstTS.date, let to = lastTS?.date else { return "x\(count), \(first)" }
    return "x\(count) over \(durationText(from: from, to: to)), \(first)"
}
