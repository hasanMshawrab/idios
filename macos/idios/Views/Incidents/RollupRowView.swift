import IdiosModel
import SwiftUI

/// RollupRowView is one line for the pods of a workload that share one
/// problem: what decides whether to open it up.
struct RollupRowView: View {
    let rollup: Rollup
    let clusterName: String
    let selectedClusters: Int
    let now: Date
    let expanded: Bool
    let density: Density
    let toggle: () -> Void

    @State private var hovering = false

    private var rows: [Incident] { rollup.rows }
    private var badge: GroupBadge { groupBadge(rows) }

    var body: some View {
        HStack(alignment: .top, spacing: ListColumns.gap) {
            FoldDisclosure(
                expanded: expanded,
                help: expanded ? "fold these pods into one line" : "show each pod",
                toggle: toggle)
                .frame(width: ListColumns.disclosure)
            RowDot(color: badge.tone.badge.text)
            CategoryBadge(category: rollup.category, width: ListColumns.category)
            VStack(alignment: .leading, spacing: 3) {
                subject
                if density == .comfortable || hovering { secondLine }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            ContainerChip(name: rollup.containerName, containerCount: nil, subjectKind: .pod)
                .frame(width: ListColumns.container, alignment: .leading)
            Text(reasons)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .frame(width: ListColumns.reason, alignment: .leading)
            // The pill on the first line counts the incidents; no line sums
            // the restarts of the pods it stands for.
            RestartsCell(text: "-")
            SpanAgeCell(rows: rows, now: now)
            Badge(text: badge.text, style: badge.tone.badge, width: ListColumns.state)
        }
        .padding(.vertical, 4)
        .opacity(rollup.openCount == 0 ? 0.6 : 1)
        .onHover { hovering = $0 }
    }

    private var subject: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            // The group header names the workload only when the list is grouped
            // by workload; under any other grouping this line is the one place
            // that says whose pods these are.
            Text("\(rollup.folds.count) pods of \(workloadTitle(rows[0]))")
                .font(.system(size: 12, weight: .semibold))
            CountPill(
                text: "\(rows.count) incidents",
                help: "\(rows.count) incidents on \(rollup.folds.count) pods share this "
                    + "container and category")
        }
    }

    private var reasons: String {
        let reasons = rollup.reasons.joined(separator: ", ")
        guard !rollup.exitCodes.isEmpty else { return reasons }
        return reasons + " - exit " + rollup.exitCodes.map(String.init).joined(separator: ", ")
    }

    private var secondLine: some View {
        Flow(spacing: 5) {
            Text(
                scopePrefix(
                    clusterName: clusterName, namespace: rows[0].namespace,
                    selectedClusterCount: selectedClusters))
            if !rollup.imageTags.isEmpty {
                RowSeparator()
                Text((rollup.imageTags.count == 1 ? "tag " : "tags ")
                    + rollup.imageTags.joined(separator: ", "))
            }
        }
        .font(.system(size: 11, design: .monospaced))
        .foregroundStyle(.secondary)
    }
}
