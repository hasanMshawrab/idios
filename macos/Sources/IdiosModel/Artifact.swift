import Foundation
import IdiosAPI

/// Artifact is one capture attempt: a file idios wrote, or the gap that
/// explains why there is none.
public struct Artifact: Identifiable, Hashable, Sendable {
    public let id: String
    public let podUID: String
    public let incidentID: String?
    public let containerName: String?
    public let kind: ArtifactKind
    public let restartCount: Int32
    public let filePath: String?
    public let sizeBytes: Int64?
    public let truncated: Bool
    public let capturedEarly: Bool
    public let captureGap: CaptureGap?
    public let captureNote: String?
    public let capturedAt: Timestamp?
}

extension Artifact {
    /// init builds an artifact from its wire row.
    public init(wire: Components.Schemas.Artifact) throws {
        self.id = try require(wire.id, "id")
        self.podUID = try require(wire.podUid, "podUid")
        self.incidentID = wire.incidentId
        self.containerName = wire.containerName
        self.kind = try require(modelEnum(wire.kind), "kind")
        self.restartCount = wire.restartCount ?? 0
        self.filePath = wire.filePath
        self.sizeBytes = parseInt64(wire.sizeBytes)
        self.truncated = wire.truncated ?? false
        self.capturedEarly = wire.capturedEarly ?? false
        self.captureGap = modelEnum(wire.captureGap)
        self.captureNote = wire.captureNote
        self.capturedAt = wire.capturedAt.map(Timestamp.init)
    }
}
