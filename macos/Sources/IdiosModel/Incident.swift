import Foundation
import IdiosAPI

/// Incident is one incident row.
public struct Incident: Identifiable, Hashable, Sendable {
    public let id: String
    public let clusterID: String
    public let namespace: String
    public let subjectKind: SubjectKind
    public let podUID: String?
    public let jobUID: String?
    public let containerName: String?
    public let workloadKind: String
    public let workloadName: String
    public let category: Category
    public let firstReason: String
    public let lastReason: String
    public let lastMessage: String?
    public let image: String?
    public let imageTag: String?
    public let imageID: String?
    public let occurrences: Int32
    public let openedAt: Timestamp
    public let lastSeenAt: Timestamp
    public let closedAt: Timestamp?
    public let closeReason: CloseReason?
    public let acknowledgedAt: Timestamp?
    public let dismissedAt: Timestamp?
    public let note: String?
    public let state: IncidentState
    public let podName: String?
    public let podDeletedAt: Timestamp?
    public let podDeletionReason: DeletionReason?
    public let containerCount: Int32
    public let exitCode: Int32?
    public let signal: Int32?
    public let nodeName: String
}

extension Incident {
    /// init builds an incident from its wire row.
    public init(wire: Components.Schemas.IncidentRow) throws {
        self.id = try require(wire.id, "id")
        self.clusterID = try require(wire.clusterId, "clusterId")
        self.namespace = try require(wire.namespace, "namespace")
        self.subjectKind = try require(modelEnum(wire.subjectKind), "subjectKind")
        self.podUID = wire.podUid
        self.jobUID = wire.jobUid
        self.containerName = wire.containerName
        self.workloadKind = try require(wire.workloadKind, "workloadKind")
        // A pod with no controller has no workload name; proto3 leaves the
        // empty string out of the JSON, and the screens fall back to the pod.
        self.workloadName = wire.workloadName ?? ""
        self.category = try require(modelEnum(wire.category), "category")
        self.firstReason = try require(wire.firstReason, "firstReason")
        self.lastReason = try require(wire.lastReason, "lastReason")
        self.lastMessage = wire.lastMessage
        self.image = wire.image
        self.imageTag = wire.imageTag
        self.imageID = wire.imageId
        self.occurrences = try require(wire.occurrences, "occurrences")
        self.openedAt = Timestamp(try require(wire.openedAt, "openedAt"))
        self.lastSeenAt = Timestamp(try require(wire.lastSeenAt, "lastSeenAt"))
        self.closedAt = wire.closedAt.map(Timestamp.init)
        self.closeReason = modelEnum(wire.closeReason)
        self.acknowledgedAt = wire.acknowledgedAt.map(Timestamp.init)
        self.dismissedAt = wire.dismissedAt.map(Timestamp.init)
        self.note = wire.note
        self.state = try require(modelEnum(wire.state), "state")
        self.podName = wire.podName
        self.podDeletedAt = wire.podDeletedAt.map(Timestamp.init)
        self.podDeletionReason = modelEnum(wire.podDeletionReason)
        self.containerCount = wire.containerCount ?? 0
        self.exitCode = wire.exitCode
        self.signal = wire.signal
        // A scheduling incident opens before placement and a job incident has
        // no pod at all; either way there is no node to name.
        self.nodeName = wire.nodeName ?? ""
    }
}

extension Page where Row == Incident {
    /// init builds a page of incidents from its wire response.
    public init(wire: Components.Schemas.IncidentsResponse) throws {
        self.init(
            rows: try (wire.incidents ?? []).map(Incident.init(wire:)), truncated: wire.truncated ?? false)
    }
}

/// IncidentCounts is the counter behind every sidebar folder.
public struct IncidentCounts: Hashable, Sendable {
    public let byState: [IncidentState: Int32]
    public let byCategory: [Category: Int32]
}

extension IncidentCounts {
    /// init builds the counters from their wire message.
    public init(wire: Components.Schemas.IncidentCounts) throws {
        var byState: [IncidentState: Int32] = [:]
        for row in wire.byState ?? [] {
            let state: IncidentState = try require(modelEnum(row.state), "state")
            byState[state] = row.count ?? 0
        }
        var byCategory: [Category: Int32] = [:]
        for row in wire.byCategory ?? [] {
            let category: Category = try require(modelEnum(row.category), "category")
            byCategory[category] = row.count ?? 0
        }
        self.byState = byState
        self.byCategory = byCategory
    }
}

/// IncidentDetail is everything the incident screen shows about one incident.
public struct IncidentDetail: Hashable, Sendable {
    public let incident: Incident
    public let pod: Pod?
    public let containers: [Container]
    public let artifacts: [Artifact]
    public let events: [Event]
    public let conditions: [PodCondition]
    public let job: Job?
    public let lastPodName: String?
    public let grafanaURL: String?
    /// relatedIncidents is the other incidents of the same pod and of the
    /// same Job, newest activity first, as the daemon bounded them.
    public let relatedIncidents: [Incident]
}

extension IncidentDetail {
    /// replacingIncident swaps in the incident row a write answered; everything
    /// else the detail carries is what the read already loaded.
    public func replacingIncident(_ incident: Incident) -> IncidentDetail {
        IncidentDetail(
            incident: incident, pod: pod, containers: containers,
            artifacts: artifacts, events: events, conditions: conditions, job: job,
            lastPodName: lastPodName, grafanaURL: grafanaURL,
            relatedIncidents: relatedIncidents)
    }

    /// init builds the detail from its wire message.
    public init(wire: Components.Schemas.IncidentDetail) throws {
        self.incident = try Incident(wire: try require(wire.incident, "incident"))
        self.pod = try wire.pod.map(Pod.init(wire:))
        self.containers = try (wire.containers ?? []).map(Container.init(wire:))
        self.artifacts = try (wire.artifacts ?? []).map(Artifact.init(wire:))
        self.events = try (wire.events ?? []).map(Event.init(wire:))
        self.conditions = try (wire.conditions ?? []).map(PodCondition.init(wire:))
        self.job = try wire.job.map(Job.init(wire:))
        self.lastPodName = wire.lastPodName
        self.grafanaURL = wire.grafanaUrl
        self.relatedIncidents = try (wire.relatedIncidents ?? []).map(Incident.init(wire:))
    }
}
