import IdiosModel
import SwiftUI

/// IncidentRowView is one line of the triage list: what decides a click.
struct IncidentRowView: View {
    let incident: Incident
    let clusterName: String
    let selectedClusters: Int
    let now: Date
    var siblings: Int = 0
    var expanded: Bool = false
    var toggleFold: (() -> Void)? = nil
    var isSibling: Bool = false
    // A fold drawn under a rollup or a run steps in so the eye reads it as part
    // of that line.
    var indented: Bool = false
    // Outside the list the row stands in a card that has room for both lines
    // and no hover to ask with.
    var density: Density = .comfortable

    @State private var hovering = false

    private var acknowledged: Bool { incident.acknowledgedAt != nil }
    private var closed: Bool { incident.closedAt != nil }
    private var leads: Bool { siblings > 0 && toggleFold != nil }
    private var isJobSubject: Bool { incident.subjectKind == .job }

    var body: some View {
        HStack(alignment: .top, spacing: ListColumns.gap) {
            if indented { Color.clear.frame(width: ListColumns.disclosure, height: 1) }
            if leads {
                FoldDisclosure(
                    expanded: expanded,
                    help: expanded ? "fold this pod's incidents" : "show this pod's other incidents",
                    toggle: { toggleFold?() })
                    .frame(width: ListColumns.disclosure)
            } else {
                Color.clear.frame(width: ListColumns.disclosure, height: 1)
            }
            RowDot(color: incident.state.badge.text, help: incident.state.tooltip)
            CategoryBadge(category: incident.category, width: ListColumns.category)
            VStack(alignment: .leading, spacing: 3) {
                subject
                if density == .comfortable || hovering { secondLine }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            containerChip
                .frame(width: ListColumns.container, alignment: .leading)
            Text(reason)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .frame(width: ListColumns.reason, alignment: .leading)
            RestartsCell(text: isJobSubject ? "-" : "\(incident.occurrences)")
            AgeCell(
                timestamp: incident.closedAt ?? incident.openedAt,
                field: closed ? "closed_at" : "opened_at", now: now)
            StateBadge(state: incident.state, width: ListColumns.state)
        }
        .padding(.vertical, 4)
        .opacity(closed ? 0.6 : 1)
        .onHover { hovering = $0 }
    }

    // A sibling row stands under the lead that already names the workload and
    // the pod, so the container it differs in is its title.
    @ViewBuilder private var subject: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if isSibling {
                Text(
                    scopeLabel(
                        containerName: incident.containerName, subjectKind: incident.subjectKind))
                    .font(.system(size: 11.5, weight: .medium, design: .monospaced))
                    .foregroundStyle(incident.containerName == nil ? .tertiary : .primary)
                    .help(
                        incident.containerName.map {
                            "container \($0) of \(incident.containerCount)"
                        } ?? scopeHelp(incident.subjectKind))
            } else {
                Text(kindWord).font(.system(size: 12)).foregroundStyle(.secondary)
                Text(name)
                    .font(
                        .system(
                            size: 12, weight: acknowledged ? .regular : .semibold,
                            design: .monospaced))
                if let suffix = podSuffix {
                    RowSeparator()
                    Text("pod").font(.system(size: 11)).foregroundStyle(.secondary)
                    MiddleElidedText(value: suffix, full: incident.podName, keeping: 14)
                        .font(.system(size: 11, design: .monospaced))
                }
            }
            if leads { siblingPill }
        }
    }

    // A bare pod is filed under its own name, so its line reads as the pod it
    // is; a job-subject row is about the Job that failed, whatever created it.
    private var kindWord: String {
        if isJobSubject { return "Job" }
        return incident.workloadName.isEmpty ? "Pod" : workloadKindLabel(incident)
    }

    private var name: String { workloadTitle(incident) }

    private var podSuffix: String? {
        guard !incident.workloadName.isEmpty, let podName = incident.podName else { return nil }
        return podNameSuffix(name: podName, workloadName: incident.workloadName)
    }

    private var reason: String {
        guard let exit = incident.exitCode else { return incident.lastReason }
        return "\(incident.lastReason) - \(exitText(exit))"
    }

    private var siblingPill: some View {
        CountPill(
            text: "+\(siblings) on this pod", help: "\(siblings) more incidents share this pod")
    }

    @ViewBuilder private var containerChip: some View {
        let chip = ContainerChip(
            name: incident.containerName, containerCount: incident.containerCount,
            subjectKind: incident.subjectKind)
        if isJobSubject { chip.help(jobContainerTooltip) } else { chip }
    }

    @ViewBuilder private var secondLine: some View {
        if isSibling { siblingLine } else { leadLine }
    }

    // A sibling repeats nothing the lead already says: what is left is what the
    // two incidents disagree about.
    private var siblingLine: some View {
        Flow(spacing: 5) {
            if let exit = incident.exitCode {
                Text(exitText(exit))
            }
            if let tag = incident.imageTag {
                if incident.exitCode != nil { RowSeparator() }
                Text("tag \(tag)")
                    .help(incident.image ?? tag)
            }
            if let acknowledgedAt = incident.acknowledgedAt {
                if incident.exitCode != nil || incident.imageTag != nil { RowSeparator() }
                Text("acknowledged \(clockTime(acknowledgedAt))")
                    .help("acknowledged_at \(acknowledgedAt.raw)")
            }
        }
        .font(.system(size: 11, design: .monospaced))
        .foregroundStyle(.secondary)
    }

    private var leadLine: some View {
        Flow(spacing: 5) {
            Text(
                scopePrefix(
                    clusterName: clusterName, namespace: incident.namespace,
                    selectedClusterCount: selectedClusters))
            if let podName = incident.podName {
                RowSeparator()
                Text("pod")
                MiddleElidedText(
                    value: podNameSuffix(name: podName, workloadName: incident.workloadName),
                    full: podName)
            }
            if let exit = incident.exitCode {
                RowSeparator()
                Text(exitText(exit))
            }
            if let deletedAt = incident.podDeletedAt {
                RowSeparator()
                Text(
                    "pod deleted \(clockTime(deletedAt)), "
                        + (incident.podDeletionReason?.label ?? "unknown"))
                    .help(
                        "pod_deleted_at \(deletedAt.raw), deletion_reason "
                            + (incident.podDeletionReason?.rawValue ?? "unknown"))
                Text("(inferred)").foregroundStyle(.tertiary)
            }
            if let tag = incident.imageTag {
                RowSeparator()
                Text("tag \(tag)")
                    .help(incident.image ?? tag)
            }
            if let acknowledgedAt = incident.acknowledgedAt {
                RowSeparator()
                Text("acknowledged \(clockTime(acknowledgedAt))")
                    .help("acknowledged_at \(acknowledgedAt.raw)")
            }
        }
        .font(.system(size: 11, design: .monospaced))
        .foregroundStyle(.secondary)
    }

    // The kubelet reports signal 0 when a container was not signalled; saying
    // "signal 0" would read as a signal that was sent.
    private func exitText(_ exit: Int32) -> String {
        guard let signal = incident.signal, signal != 0 else { return "exit \(exit)" }
        return "exit \(exit), signal \(signal)"
    }
}

/// FoldDisclosure is the chevron that opens and closes a folded line.
struct FoldDisclosure: View {
    let expanded: Bool
    let help: String
    let toggle: () -> Void

    var body: some View {
        Button(action: toggle) {
            Image(systemName: expanded ? "chevron.down" : "chevron.right")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.secondary)
                .frame(width: 12, height: 14)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(help)
    }
}

/// ContainerChip names the container a line is about, or says that the
/// problem is the pod, or the Job, itself.
struct ContainerChip: View {
    let name: String?
    let containerCount: Int32?
    let subjectKind: SubjectKind

    var body: some View {
        Text(scopeLabel(containerName: name, subjectKind: subjectKind))
            .font(.system(size: 11, design: .monospaced))
            .lineLimit(1)
            .foregroundStyle(name == nil ? .tertiary : .secondary)
            .padding(.horizontal, 5)
            .padding(.vertical, 1)
            .background(RoundedRectangle(cornerRadius: 4).fill(.quaternary.opacity(0.5)))
            .help(help)
    }

    private var help: String {
        guard let name else { return scopeHelp(subjectKind) }
        guard let containerCount else { return "container \(name)" }
        return "container \(name) of \(containerCount)"
    }
}

/// scopeHelp explains an empty container_name for the chip's hover.
func scopeHelp(_ subjectKind: SubjectKind) -> String {
    subjectKind == .job
        ? "container_name is empty: the problem is the Job itself"
        : "container_name is empty: the problem is the pod itself"
}

/// RowSeparator is the dash between the facts of a row's second line.
struct RowSeparator: View {
    var body: some View {
        Text("-").foregroundStyle(.quaternary)
    }
}

/// CountPill is the small capsule that counts folded rows.
struct CountPill: View {
    let text: String
    let help: String

    var body: some View {
        Text(text)
            .font(.system(size: 10.5, weight: .semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 7)
            .padding(.vertical, 1)
            .background(Capsule().fill(.quaternary.opacity(0.5)))
            .help(help)
    }
}
