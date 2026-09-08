import IdiosModel
import SwiftUI

/// GroupHeaderView is the one header shape: chevron, state dot, worst open
/// category chip, kind and name with the summary, the containers, the
/// reasons, "-" for restarts, the age, one state badge. A scope header, which
/// names the cluster or the namespace its rows are in rather than a problem,
/// is drawn as a section title over them.
struct GroupHeaderView: View {
    let group: IncidentGroup
    let collapsed: Bool
    let now: Date
    let toggle: () -> Void

    var body: some View {
        Group {
            if group.scope { sectionTitle } else { problemHeader }
        }
        .padding(.vertical, 4)
        // The tint reaches past the cells so the header's columns stay on the
        // rows' columns.
        .background(
            RoundedRectangle(cornerRadius: 5).fill(.quaternary.opacity(0.35))
                .padding(.horizontal, -6))
        .help(group.help)
    }

    // A scope is not a problem: it carries no category, no summary and none
    // of the row columns, only its name and how much is open under it.
    private var sectionTitle: some View {
        HStack(spacing: ListColumns.gap) {
            chevron
            Text(group.title)
                .font(.system(size: 14, weight: .semibold))
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
            Badge(
                text: group.badge.text, style: group.badge.tone.badge, width: ListColumns.state)
        }
    }

    private var problemHeader: some View {
        HStack(spacing: ListColumns.gap) {
            chevron
            RowDot(
                color: group.badge.tone.badge.text,
                help: group.worstCategory.map { "worst open category: \($0.rawValue)" } ?? "")
            if let category = group.worstCategory {
                CategoryBadge(category: category, width: ListColumns.category)
            } else {
                Color.clear.frame(width: ListColumns.category, height: 1)
            }
            title
            Text(containers)
                .lineLimit(1)
                .frame(width: ListColumns.container, alignment: .leading)
            Text(reasons)
                .lineLimit(1)
                .frame(width: ListColumns.reason, alignment: .leading)
            // A header spans rows and no line sums occurrences.
            RestartsCell(text: "-")
            SpanAgeCell(rows: group.rows, now: now)
            Badge(
                text: group.badge.text, style: group.badge.tone.badge, width: ListColumns.state)
        }
        .font(.system(size: 11, design: .monospaced))
        .foregroundStyle(.secondary)
    }

    private var chevron: some View {
        Button(action: toggle) {
            Image(systemName: collapsed ? "chevron.right" : "chevron.down")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.secondary)
                .frame(width: ListColumns.disclosure, height: 14)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(collapsed ? "show this group's rows" : "fold this group")
    }

    // A concatenated multi-font Text needs fixedSize to keep its intrinsic
    // height, which overflows the window here; the cells are their own views
    // instead.
    private var title: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            if !group.kind.isEmpty {
                Text(group.kind).font(.system(size: 12, weight: .semibold))
                Text(group.title)
                    .font(.system(size: 12, weight: .medium, design: .monospaced))
            } else if !group.title.isEmpty {
                Text(group.title).font(.system(size: 12, weight: .semibold))
            }
            Text(group.summary)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .foregroundStyle(.primary)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var containers: String {
        distinctValues(
            group.rows.map {
                scopeLabel(containerName: $0.containerName, subjectKind: $0.subjectKind)
            }
        ).joined(separator: " + ")
    }

    private var reasons: String {
        distinctValues(group.rows.map(\.lastReason)).joined(separator: ", ")
    }
}
