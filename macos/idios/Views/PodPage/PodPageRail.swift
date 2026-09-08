import Foundation
import IdiosModel
import SwiftUI

/// PodPageRail is the pod's identity: the owner chain down to the selected
/// container, where it runs, the times of what is selected, and its
/// siblings among the controller's other pods.
struct PodPageRail: View {
    let detail: PodDetail
    let container: Container?
    let lit: IncidentDetail?
    let now: Date
    let openPod: (String) -> Void
    let openRun: (String) -> Void
    let openWorkloadPods: (PodRow) -> Void

    private var pod: PodRow { detail.pod }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ownerChain.explained("ownerChain")
            Divider()
            context
            Divider()
            times
            Divider()
            siblings.explained("siblings")
        }
    }

    private var ownerChain: some View {
        VStack(alignment: .leading, spacing: 10) {
            RailHeading(text: "OWNER CHAIN")
            VStack(alignment: .leading, spacing: 0) {
                if pod.workloadKind != "none" && !pod.workloadName.isEmpty {
                    chainLink(color: BadgeStyle.blue.text, last: false) {
                        railLink(
                            label: "\(pod.workloadKind.uppercased()) (workload_kind)",
                            value: pod.workloadName, note: nil)
                    }
                }
                if let kind = pod.controllerKind, let name = pod.controllerName {
                    chainLink(color: BadgeStyle.blue.text, last: false) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("\(kind.uppercased()) (controller_kind)")
                                .font(.system(size: 9.5, weight: .bold))
                                .foregroundStyle(.tertiary)
                            controllerName(kind: kind, name: name)
                            if let uid = pod.controllerUID {
                                HStack(spacing: 4) {
                                    Text("uid")
                                    MiddleElidedText(value: uid, keeping: 12)
                                }
                                .font(.system(size: 10)).foregroundStyle(.tertiary)
                            }
                        }
                    }
                }
                chainLink(color: BadgeStyle.red.text, last: container == nil) {
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 4) {
                            Text("POD (uid").font(.system(size: 9.5, weight: .bold))
                            MiddleElidedText(value: pod.uid, keeping: 12).font(.system(size: 9.5))
                            Text(")").font(.system(size: 9.5, weight: .bold))
                        }
                        .foregroundStyle(.tertiary)
                        Text(pod.name).font(.system(size: 11.5, design: .monospaced))
                            .textSelection(.enabled)
                        Text(podNote).font(.system(size: 10)).foregroundStyle(.tertiary)
                    }
                }
                if let container {
                    chainLink(color: BadgeStyle.red.text, last: true) {
                        railLink(
                            label: "CONTAINER", value: container.name,
                            note: containerNote(container))
                    }
                }
            }
        }
    }

    @ViewBuilder private func identifier(_ value: String?, keeping: Int) -> some View {
        if let value {
            MiddleElidedText(value: value, keeping: keeping)
        } else {
            Text("null").foregroundStyle(.tertiary)
        }
    }

    private func containerNote(_ container: Container) -> String {
        var parts = ["kind \(container.kind.rawValue)"]
        if let exit = container.lastTerminatedExitCode ?? container.exitCode {
            parts.append("exit \(exit)")
        }
        parts.append(plural(Int(container.restartCount), "restart"))
        return parts.joined(separator: " - ")
    }

    // A Job is a page about one run, so its name is the way to that page;
    // every other controller has no page of its own here.
    @ViewBuilder private func controllerName(kind: String, name: String) -> some View {
        if kind == "Job", let uid = pod.controllerUID {
            Button(name) { openRun(uid) }
                .buttonStyle(.link)
                .font(.system(size: 11.5, design: .monospaced))
        } else {
            Text(name).font(.system(size: 11.5, design: .monospaced))
                .textSelection(.enabled)
        }
    }

    private var podNote: String {
        guard let deletedAt = pod.deletedAt else {
            return "phase \(pod.phase) - deleted_at null"
        }
        return "phase \(pod.phase) - deleted_at \(deletedAt.raw)"
    }

    private var context: some View {
        VStack(alignment: .leading, spacing: 3) {
            RailHeading(text: "CONTEXT")
            RailRow(label: "Node") {
                Text(
                    nodeLine(
                        podNode: pod.nodeName, incidentNode: lit?.incident.nodeName ?? "",
                        deleted: pod.deletedAt != nil)
                )
                .help("pods.node_name, and incidents.node_name where the incident opened")
            }
            RailRow(label: "QoS class") { Text(pod.qosClass ?? "unknown") }
            if let container {
                RailRow(label: "Image id") { identifier(container.imageID, keeping: 34) }
                RailRow(label: "Container id") {
                    if let containerID = container.containerID {
                        MiddleElidedText(value: containerID, keeping: 20)
                    } else {
                        Text("null").foregroundStyle(.tertiary)
                    }
                }
            }
        }
    }

    @ViewBuilder private var times: some View {
        if let lit {
            IncidentTimesRail(incident: lit.incident, now: now)
        } else {
            podTimes
        }
    }

    private var podTimes: some View {
        VStack(alignment: .leading, spacing: 3) {
            RailHeading(text: "TIMES pod")
            RailRow(label: "created") { stamp(pod.createdAt, column: "created_at", source: "k8s") }
            RailRow(label: "started") {
                optionalStamp(pod.startedAt, column: "started_at", source: "k8s")
            }
            RailRow(label: "first seen") {
                stamp(pod.firstSeenAt, column: "first_seen_at", source: "observed")
            }
            RailRow(label: "last seen") {
                stamp(pod.lastSeenAt, column: "last_seen_at", source: "observed")
            }
            RailRow(label: "deletion requested") {
                optionalStamp(
                    pod.deletionRequestedAt, column: "deletion_requested_at", source: "k8s")
            }
            RailRow(label: "deleted") {
                optionalStamp(pod.deletedAt, column: "deleted_at", source: "observed")
            }
        }
    }

    private var siblings: some View {
        VStack(alignment: .leading, spacing: 6) {
            RailHeading(text: siblingsHeading)
            ForEach(siblingRows(pod: pod, ready: podReady, siblings: detail.siblings), id: \.uid) {
                row in
                siblingRow(row)
            }
            let more = moreSiblings(total: detail.siblingTotal, shown: detail.siblings.count)
            if more > 0 {
                Button("+ \(more) more of this \(pod.controllerKind ?? "controller")") {
                    openWorkloadPods(pod)
                }
                .buttonStyle(.link)
                .font(.system(size: 10.5))
                .help("opens Workloads > \(pod.workloadName) > Pods")
            }
        }
    }

    private var podReady: Bool? {
        guard let condition = detail.conditions.first(where: { $0.type == "Ready" }) else {
            return nil
        }
        return condition.status == "True"
    }

    private var siblingsHeading: String {
        guard let kind = pod.controllerKind else { return "SIBLINGS none" }
        return "SIBLINGS " + plural(Int(detail.siblingTotal), "pod") + " of this \(kind)"
    }

    @ViewBuilder private func siblingRow(_ row: SiblingRow) -> some View {
        let content = HStack(spacing: 6) {
            Circle().fill(dotColor(row)).frame(width: 7, height: 7)
            Text(podNameSuffix(name: row.name, workloadName: pod.workloadName))
                .font(.system(size: 11, design: .monospaced))
            Spacer()
            trailing(row)
        }
        if row.isThisPod {
            content
        } else {
            Button { openPod(row.uid) } label: { content }
                .buttonStyle(.plain)
        }
    }

    private func dotColor(_ row: SiblingRow) -> Color {
        if row.category != nil { return BadgeStyle.red.text }
        if row.deleted { return BadgeStyle.grey.text }
        return row.ready ? BadgeStyle.green.text : BadgeStyle.orange.text
    }

    @ViewBuilder private func trailing(_ row: SiblingRow) -> some View {
        if row.isThisPod {
            Text("this pod").font(.system(size: 10)).foregroundStyle(.tertiary)
        } else if let category = row.category {
            CategoryBadge(category: category)
        } else if row.deleted {
            Text("deleted").font(.system(size: 10)).foregroundStyle(.tertiary)
        } else if row.ready {
            Text("ready").font(.system(size: 10)).foregroundStyle(.tertiary)
        } else {
            Text("not ready").font(.system(size: 10)).foregroundStyle(.tertiary)
        }
    }
}

/// IncidentTimesRail is the rail's TIMES group for a lit incident, shared by
/// the pod page and the page a Job's incident gets when no pod is kept.
struct IncidentTimesRail: View {
    let incident: Incident
    let now: Date

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            RailHeading(text: "TIMES incident \(incident.id)")
            RailRow(label: "opened") {
                stamp(incident.openedAt, column: "opened_at", source: "k8s")
            }
            RailRow(label: "last seen") {
                stamp(incident.lastSeenAt, column: "last_seen_at", source: "observed")
            }
            RailRow(label: "open for") { Text(openFor(incident, now: now)) }
            if let closedAt = incident.closedAt {
                RailRow(label: "closed") {
                    stamp(closedAt, column: "closed_at", source: "observed")
                }
                RailRow(label: "close reason") {
                    if let reason = incident.closeReason {
                        Text(reason.label).help("close_reason \(reason.rawValue)")
                    } else {
                        Text("null").foregroundStyle(.tertiary)
                    }
                }
            }
            RailRow(label: "acknowledged") {
                optionalStamp(
                    incident.acknowledgedAt, column: "acknowledged_at", source: "observed")
            }
            RailRow(label: "dismissed") {
                optionalStamp(incident.dismissedAt, column: "dismissed_at", source: "observed")
            }
        }
    }
}

/// openFor is how long an incident has been open, or was open before it
/// closed, as every rail that carries an incident says it.
func openFor(_ incident: Incident, now: Date) -> String {
    guard let opened = incident.openedAt.date else { return "unknown" }
    let end = incident.closedAt?.date ?? now
    return durationText(from: opened, to: end)
}

// The row says the word a reader wants; the column the value came from
// joins the raw stamp on hover, so nothing is lost by saying it plainly.
func stamp(_ timestamp: Timestamp, column: String, source: String) -> some View {
    VStack(alignment: .leading, spacing: 0) {
        Text(timestamp.raw)
            .font(.system(size: 10.5, design: .monospaced))
            .textSelection(.enabled)
        Text("(\(source))").font(.system(size: 9.5)).foregroundStyle(.tertiary)
    }
    .help("\(column) \(timestamp.raw)")
}

@ViewBuilder func optionalStamp(
    _ timestamp: Timestamp?, column: String, source: String
) -> some View {
    if let timestamp {
        stamp(timestamp, column: column, source: source)
    } else {
        Text("null").font(.system(size: 10.5, design: .monospaced))
            .foregroundStyle(.tertiary)
            .help(column)
    }
}

// The rail draws a dot per link and a line to the next one, so the eye
// reads owner to owned in one pass.
func chainLink<Content: View>(
    color: Color, last: Bool, @ViewBuilder content: () -> Content
) -> some View {
    HStack(alignment: .top, spacing: 10) {
        VStack(spacing: 0) {
            Circle().fill(color).frame(width: 8, height: 8).padding(.top, 3)
            if !last {
                Rectangle().fill(.tertiary).frame(width: 1).frame(maxHeight: .infinity)
            }
        }
        .frame(width: 8)
        content().padding(.bottom, last ? 0 : 12)
    }
    .fixedSize(horizontal: false, vertical: true)
}

/// railLink is one named value of the owner chain.
func railLink(label: String, value: String, note: String?) -> some View {
    VStack(alignment: .leading, spacing: 2) {
        Text(label).font(.system(size: 9.5, weight: .bold)).foregroundStyle(.tertiary)
        Text(value).font(.system(size: 11.5, design: .monospaced)).textSelection(.enabled)
        if let note {
            Text(note).font(.system(size: 10)).foregroundStyle(.tertiary)
        }
    }
}
