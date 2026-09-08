import IdiosAPI
import Testing

@testable import IdiosModel

let crashArtifact = Artifact(
    id: "81",
    podUID: "pod-crash",
    incidentID: "412",
    containerName: "api",
    kind: .logPrevious,
    restartCount: 6,
    filePath: "prod/pod-crash/api-6.log",
    sizeBytes: 262_144,
    truncated: true,
    capturedEarly: true,
    captureGap: .kubeletError,
    captureNote: "the container is not available",
    capturedAt: Timestamp("2026-08-27T14:38:05.000000Z"))

@Test func artifactsCarryTheirGapNoteAndParsedSize() throws {
    let artifact = try Artifact(wire: fixture(Components.Schemas.Artifact.self, "artifact.json"))
    #expect(artifact == crashArtifact)
}
