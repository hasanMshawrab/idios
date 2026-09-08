import IdiosModel
import SwiftUI

/// KubeletCard is what the kubelet reported about one container, in the
/// kubelet's own words, and what the incident recorded at open when one
/// is lit.
struct KubeletCard: View {
    let container: Container
    let phase: String
    let qosClass: String?
    let incident: Incident?
    let job: Job?
    let now: Date

    var body: some View {
        DetailCard(title: "WHAT THE KUBELET REPORTS", meta: meta) {
            facts
            // Complete after a crashed subject pod is what a retry
            // succeeding looks like, and it is the line that says whether
            // anyone needs paging.
            if let job {
                FactRow(label: "Run outcome") { Text(job.runOutcomeLabel) }
            }
        }
    }

    private var meta: String? {
        guard let incident else { return nil }
        return "idios records requests and limits, never usage. The limit is context, not the "
            + "cause; the reason is \(incident.lastReason)."
    }

    private var facts: some View {
        VStack(alignment: .leading, spacing: 3) {
            FactRow(label: "Current state") {
                VStack(alignment: .leading, spacing: 1) {
                    Text(container.reason.map { "\(container.state.rawValue) - \($0)" }
                        ?? container.state.rawValue)
                        .foregroundStyle(container.state.badge(exitCode: container.exitCode).text)
                    if let since = runningSinceText(container, now: now) {
                        Text(since)
                            .font(.system(size: 10.5))
                            .foregroundStyle(.tertiary)
                            .help(
                                "containers.running_since, the kubelet's state.running.startedAt")
                    }
                }
            }
            FactRow(label: "Restart count") { Text("\(container.restartCount)") }
            FactRow(label: "Last terminated") { Text(lastTerminated(container)) }
            FactRow(label: "Last finished at") {
                Text(container.lastTerminatedAt.map { "\($0.raw) (k8s)" } ?? "never terminated")
            }
            if let incident {
                FactRow(label: "First reason") { Text(incident.firstReason) }
                FactRow(label: "Last reason") { Text(incident.lastReason) }
                FactRow(label: "Image at open") { Text(incident.image ?? "none recorded") }
                FactRow(label: "Image id at open") {
                    if let imageID = incident.imageID {
                        MiddleElidedText(value: imageID, keeping: 34)
                    } else {
                        Text("none recorded").foregroundStyle(.tertiary)
                    }
                }
            }
            FactRow(label: "Ready") {
                Text(String(container.ready) + " (\(readyMeaning(container.kind)))")
            }
            FactRow(label: "Pod phase") { Text(phase) }
            FactRow(label: "Container kind") { Text(container.kind.rawValue) }
            FactRow(label: "QoS class") { Text(qosClass ?? "unknown") }
            FactRow(label: "cpu / memory") { Text(cpuMemory) }
        }
    }

    // A pair with neither a request nor a limit is left out rather than
    // shown as its own "no requests written", so one absent resource does
    // not crowd out the other that was actually written; the resource name
    // prefixes each pair so cpu and memory are never mistaken for each other
    // once a pair with nothing set has been dropped.
    private var cpuMemory: String {
        let pairs = [
            resourceLine(
                name: "cpu", request: container.cpuRequest, limit: container.cpuLimit,
                requestValue: container.cpuRequestMillis, limitValue: container.cpuLimitMillis,
                unit: .millicores),
            resourceLine(
                name: "memory", request: container.memRequest, limit: container.memLimit,
                requestValue: container.memRequestBytes, limitValue: container.memLimitBytes,
                unit: .bytes),
        ].compactMap { $0 }
        return pairs.isEmpty ? "no requests written" : pairs.joined(separator: " - ")
    }

    private func lastTerminated(_ container: Container) -> String {
        guard let reason = container.lastTerminatedReason else { return "never terminated" }
        var text = reason
        if let exit = container.lastTerminatedExitCode { text += " - exit \(exit)" }
        let signal = container.lastTerminatedSignal ?? 0
        text += signal == 0 ? " - signal none" : " - signal \(signal)"
        return text
    }

    // What ready means depends on where the container sits in the pod.
    private func readyMeaning(_ kind: ContainerKind) -> String {
        switch kind {
        case .app: "app container: its readiness probe, or that it runs at all"
        case .sidecar: "sidecar: started and passing its probe"
        case .`init`: "init container: it ran to completion"
        case .ephemeral: "ephemeral container: not part of pod readiness"
        }
    }
}
