import IdiosModel
import SwiftUI

/// RunRowView is one run of a CronJob or Job: the job_failed row and the pod
/// rows of one Job as one line.
struct RunRowView: View {
    let run: RunFold
    let kind: String
    let workloadName: String
    let now: Date
    let expanded: Bool
    let density: Density
    let toggle: () -> Void
    let openRun: (String) -> Void

    @State private var hovering = false

    private var badge: GroupBadge { groupBadge(run.rows) }
    private var closed: Bool { run.openCount == 0 }

    var body: some View {
        HStack(alignment: .top, spacing: ListColumns.gap) {
            FoldDisclosure(
                expanded: expanded,
                help: expanded ? "fold this run's incidents" : "show this run's incidents",
                toggle: toggle)
                .frame(width: ListColumns.disclosure)
            RowDot(color: badge.tone.badge.text)
            CategoryBadge(
                category: run.categories.first ?? run.lead.category,
                width: ListColumns.category)
            VStack(alignment: .leading, spacing: 3) {
                subject
                if density == .comfortable || hovering { secondLine }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Text(containers)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .frame(width: ListColumns.container, alignment: .leading)
            Text(reasons)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .frame(width: ListColumns.reason, alignment: .leading)
            RestartsCell(text: restarts)
            SpanAgeCell(rows: run.rows, now: now)
            Badge(text: badge.text, style: badge.tone.badge, width: ListColumns.state)
        }
        .padding(.vertical, 4)
        .opacity(closed ? 0.6 : 1)
        .onHover { hovering = $0 }
    }

    // A CronJob's group already names the workload, so the run's own segment
    // is what tells one line from the next; a Job is its own single run.
    @ViewBuilder private var subject: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if kind == "CronJob" {
                Text("run").font(.system(size: 12)).foregroundStyle(.secondary)
                Button { openRun(run.jobUID) } label: {
                    if let suffix = run.suffix {
                        Text(suffix)
                            .font(.system(size: 12, weight: .medium, design: .monospaced))
                    } else {
                        MiddleElidedText(value: run.jobUID, keeping: 12)
                            .font(.system(size: 12, design: .monospaced))
                    }
                }
                .buttonStyle(.link)
                .help("open this run")
            } else {
                Text("Job").font(.system(size: 12)).foregroundStyle(.secondary)
                Text(workloadName)
                    .font(.system(size: 12, weight: .medium, design: .monospaced))
            }
            if !pods.isEmpty {
                Text("- \(pods)")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var pods: String {
        let count = run.podCount
        guard count > 0 else { return "" }
        if kind != "CronJob", count == 1, let suffix = run.podSuffixes.first {
            return "pod \(suffix)"
        }
        return count == 1 ? "1 pod" : "\(count) pods"
    }

    private var containers: String {
        distinctValues(
            run.rows.map {
                scopeLabel(containerName: $0.containerName, subjectKind: $0.subjectKind)
            }
        ).joined(separator: ", ")
    }

    private var reasons: String {
        let reasons = run.reasons.joined(separator: ", ")
        guard !run.exitCodes.isEmpty else { return reasons }
        return reasons + " - exit " + run.exitCodes.map(String.init).joined(separator: ", ")
    }

    // The run's own line has no restart count when the job row leads it: a Job
    // never had a container to restart.
    private var restarts: String {
        run.lead.podUID == nil ? "-" : "\(run.lead.occurrences)"
    }

    private var secondLine: some View {
        Flow(spacing: 5) {
            Text(run.lead.namespace)
            if run.categories.count > 1 {
                RowSeparator()
                Text(run.categories.map(\.label).joined(separator: ", "))
            }
            if kind == "CronJob", !run.podSuffixes.isEmpty {
                RowSeparator()
                Text("pods " + run.podSuffixes.joined(separator: ", "))
            }
            if !tags.isEmpty {
                RowSeparator()
                Text((tags.count == 1 ? "tag " : "tags ") + tags.joined(separator: ", "))
            }
            if let deleted = run.rows.first(where: { $0.podDeletedAt != nil }),
                let deletedAt = deleted.podDeletedAt
            {
                RowSeparator()
                Text(
                    "pod deleted \(clockTime(deletedAt)), "
                        + (deleted.podDeletionReason?.label ?? "unknown"))
                    .help(
                        "pod_deleted_at \(deletedAt.raw), deletion_reason "
                            + (deleted.podDeletionReason?.rawValue ?? "unknown"))
            }
        }
        .font(.system(size: 11, design: .monospaced))
        .foregroundStyle(.secondary)
    }

    private var tags: [String] {
        distinctValues(run.rows.compactMap(\.imageTag))
    }
}
