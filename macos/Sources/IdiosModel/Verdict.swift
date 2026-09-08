import Foundation

/// verdictSentences is what the middle pane leads with: what happened and
/// how often, what the container was allowed, what was captured and whether
/// the image moved, each sentence left out when its fields are not there.
public func verdictSentences(
    incident: Incident, container: Container?, artifacts: [Artifact]
) -> [String] {
    var sentences = [whatHappened(incident)]
    if let allowance = memoryAllowance(container) { sentences.append(allowance) }
    sentences.append(whatWasCaptured(artifacts))
    if let move = imageMove(incident: incident, container: container) { sentences.append(move) }
    return sentences
}

private func whatHappened(_ incident: Incident) -> String {
    let what =
        incident.exitCode.map { "Exit \($0) (\(incident.lastReason))" } ?? incident.lastReason
    let often =
        incident.occurrences == 1 ? "once" : plural(Int(incident.occurrences), "time")
    return
        "\(what) \(often) since \(clockTime(incident.openedAt)), last \(clockTime(incident.lastSeenAt))."
}

// The limit is what the kubelet kills on, and the request rides with it
// because a limit alone does not say how much was asked for.
private func memoryAllowance(_ container: Container?) -> String? {
    guard let container else { return nil }
    var halves: [String] = []
    if let limit = container.memLimitBytes { halves.append("limit \(byteCount(limit))") }
    if let request = container.memRequestBytes { halves.append("request \(byteCount(request))") }
    guard !halves.isEmpty else { return nil }
    return "Memory \(halves.joined(separator: ", "))."
}

private func whatWasCaptured(_ artifacts: [Artifact]) -> String {
    let logs = artifacts.filter { $0.kind != .podJSON }
    guard !logs.isEmpty else { return "Nothing was captured." }
    let missing = logs.filter { $0.filePath == nil }
    guard let newest = missing.max(by: captureOrder) else { return "Every restart captured a log." }
    var sentence =
        "\(logs.count - missing.count) of \(plural(logs.count, "restart")) captured a log"
    sentence += "; \(captureLabel(newest)) wrote nothing"
    if let gap = newest.captureGap { sentence += " (capture gap: \(gap.label))" }
    return sentence + "."
}

// The running container's log has no restart count to place it by, and it is
// always the newest attempt there is.
private func captureOrder(_ a: Artifact, _ b: Artifact) -> Bool {
    if (a.kind == .logCurrent) != (b.kind == .logCurrent) { return b.kind == .logCurrent }
    return a.restartCount < b.restartCount
}

private func captureLabel(_ artifact: Artifact) -> String {
    artifact.kind == .logCurrent ? "the running container" : "restart \(artifact.restartCount)"
}

private func imageMove(incident: Incident, container: Container?) -> String? {
    guard let opened = incident.imageID, !opened.isEmpty,
        let running = container?.imageID, !running.isEmpty
    else { return nil }
    return opened == running
        ? "Same image digest since open." : "The image digest changed since this opened."
}
