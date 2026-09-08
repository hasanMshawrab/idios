import IdiosModel

/// artifactOrder is the order the chips are drawn in: the dead instances oldest
/// first, then the running container's log.
func artifactOrder(_ a: Artifact, _ b: Artifact) -> Bool {
    guard a.kind == b.kind else { return kindRank(a.kind) < kindRank(b.kind) }
    return a.restartCount < b.restartCount
}

private func kindRank(_ kind: ArtifactKind) -> Int {
    switch kind {
    case .logPrevious: 0
    case .logCurrent: 1
    case .podJSON: 2
    }
}

/// defaultArtifact is the chip a screen opens on: the previous log with the
/// highest restart count belongs to the instance that died closest to the
/// incident, and with no previous log the running container's is all there is.
func defaultArtifact(_ chips: [Artifact]) -> Artifact? {
    let previous = chips.filter { $0.kind == .logPrevious }.max { $0.restartCount < $1.restartCount }
    return previous ?? chips.first { $0.kind == .logCurrent } ?? chips.first
}

/// artifactLabel names a captured file the way the file is named on disk.
func artifactLabel(_ artifact: Artifact) -> String {
    artifactLabel(kind: artifact.kind, restartCount: artifact.restartCount)
}

/// artifactLabel names a captured file from the two fields that decide its name,
/// for rows that carry those fields without carrying the artifact.
func artifactLabel(kind: ArtifactKind, restartCount: Int32) -> String {
    switch kind {
    case .logPrevious: "restart_" + String(format: "%03d", restartCount)
    case .logCurrent: "current.log"
    case .podJSON: "pod.json"
    }
}
