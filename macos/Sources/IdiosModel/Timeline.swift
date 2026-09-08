import Foundation
import IdiosAPI

/// TimelineEntry is one row of an incident timeline; which fields it carries
/// depends on its kind, and only the observed time is always there.
public struct TimelineEntry: Hashable, Sendable {
    public let kind: TimelineKind
    public let k8sAt: Timestamp?
    public let observedAt: Timestamp
    public let containerName: String?
    public let state: ContainerState?
    public let reason: String?
    public let exitCode: Int32?
    public let signal: Int32?
    public let restartCount: Int32
    public let gapReconstructed: Bool
    public let conditionType: String?
    public let conditionStatus: String?
    public let message: String?
    public let eventType: String?
    public let eventReason: String?
    public let count: Int32
    public let artifactID: String?
    public let artifactKind: ArtifactKind?
    public let captureGap: CaptureGap?
    public let replicasetName: String?
    public let imageTag: String?
    public let revision: String?
    public let lifecycle: LifecycleStep?
    public let closeReason: CloseReason?
}

extension TimelineEntry {
    /// init builds a timeline entry from its wire row.
    public init(wire: Components.Schemas.TimelineEntry) throws {
        self.kind = try require(modelEnum(wire.kind), "kind")
        self.k8sAt = wire.k8sAt.map(Timestamp.init)
        self.observedAt = Timestamp(try require(wire.observedAt, "observedAt"))
        self.containerName = wire.containerName
        self.state = modelEnum(wire.state)
        self.reason = wire.reason
        self.exitCode = wire.exitCode
        self.signal = wire.signal
        self.restartCount = wire.restartCount ?? 0
        self.gapReconstructed = wire.gapReconstructed ?? false
        self.conditionType = wire.conditionType
        self.conditionStatus = wire.conditionStatus
        self.message = wire.message
        self.eventType = wire.eventType
        self.eventReason = wire.eventReason
        self.count = wire.count ?? 0
        self.artifactID = wire.artifactId
        self.artifactKind = modelEnum(wire.artifactKind)
        self.captureGap = modelEnum(wire.captureGap)
        self.replicasetName = wire.replicasetName
        self.imageTag = wire.imageTag
        self.revision = wire.revision
        self.lifecycle = modelEnum(wire.lifecycle)
        self.closeReason = modelEnum(wire.closeReason)
    }
}

extension Page where Row == TimelineEntry {
    /// init builds a timeline from its wire response.
    public init(wire: Components.Schemas.TimelineResponse) throws {
        self.init(
            rows: try (wire.entries ?? []).map(TimelineEntry.init(wire:)), truncated: wire.truncated ?? false)
    }
}
