import IdiosModel
import SwiftUI

/// ListColumns is the one set of widths the column header, the group headers
/// and every row share, so the columns line up.
enum ListColumns {
    static let disclosure: CGFloat = 14
    static let dot: CGFloat = 10
    static let category: CGFloat = 96
    static let container: CGFloat = 84
    static let reason: CGFloat = 200
    static let restarts: CGFloat = 56
    static let age: CGFloat = 64
    static let state: CGFloat = 100
    static let gap: CGFloat = 10

    /// childIndent is how far a row under a group header steps in, so the eye
    /// reads one problem and its parts rather than a run of lines.
    static let childIndent: CGFloat = 24
}

/// GroupChild is the treatment every row under a header takes: the indent, the
/// guide rail from the header down to the last child, and the tint that stops
/// where the children stop.
struct GroupChild: ViewModifier {
    let isLast: Bool

    func body(content: Content) -> some View {
        content
            .padding(.leading, ListColumns.childIndent)
            .overlay(alignment: .leading) { rail }
            // A listRowBackground sits under the selection, so the accent
            // still paints a selected child whole.
            .listRowBackground(Color.clear.overlay(.quaternary.opacity(0.12)))
    }

    private var rail: some View {
        GeometryReader { geometry in
            let height = isLast ? geometry.size.height / 2 : geometry.size.height
            Rectangle()
                .fill(.quaternary)
                .frame(width: 1, height: height)
                .offset(x: 9)
            if isLast {
                // The last child turns the rail in rather than letting it run
                // off the end of the group.
                Rectangle()
                    .fill(.quaternary)
                    .frame(width: 8, height: 1)
                    .offset(x: 9, y: height)
            }
        }
    }
}

/// ColumnHeaderRow names the columns once above the list.
struct ColumnHeaderRow: View {
    var body: some View {
        HStack(spacing: ListColumns.gap) {
            Color.clear.frame(width: ListColumns.disclosure, height: 1)
            Color.clear.frame(width: ListColumns.dot, height: 1)
            Text("Category").frame(width: ListColumns.category, alignment: .leading)
            Text("Workload / pod").frame(maxWidth: .infinity, alignment: .leading)
            Text("Container").frame(width: ListColumns.container, alignment: .leading)
            Text("Reason").frame(width: ListColumns.reason, alignment: .leading)
            Text("Restarts")
                .help(restartsTooltip)
                .frame(width: ListColumns.restarts, alignment: .trailing)
            Text("Age").frame(width: ListColumns.age, alignment: .leading)
            Text("State").frame(width: ListColumns.state, alignment: .leading)
        }
        .font(.system(size: 10, weight: .semibold))
        .foregroundStyle(.tertiary)
        .lineLimit(1)
        .padding(.horizontal, 20)
        .padding(.bottom, 3)
    }
}

/// RowDot is the state colour of a line, the one mark that says open.
struct RowDot: View {
    let color: Color
    var help: String = ""

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: 7, height: 7)
            .frame(width: ListColumns.dot, height: 14)
            .help(help)
    }
}

/// RestartsCell is the count of container restarts a line carries, or "-" for
/// a line that spans rows and would have to sum them.
struct RestartsCell: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.system(size: 11.5))
            .monospacedDigit()
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .frame(width: ListColumns.restarts, alignment: .trailing)
            .help(restartsTooltip)
    }
}

/// AgeCell is how long ago a timestamp is, on one line, with the stored value
/// and the column it reads on hover.
struct AgeCell: View {
    let timestamp: Timestamp?
    let field: String
    let now: Date

    var body: some View {
        Text(elapsed)
            .font(.system(size: 11.5))
            .monospacedDigit()
            .foregroundStyle(.secondary)
            .lineLimit(1)
            .frame(width: ListColumns.age, alignment: .leading)
            .help(hover)
    }

    private var elapsed: String {
        guard let date = timestamp?.date else { return "-" }
        return durationText(from: date, to: now)
    }

    private var hover: String {
        guard let timestamp else { return "" }
        return "\(field) \(clockTime(timestamp, seconds: true)) UTC"
    }
}

/// SpanAgeCell is the age of a line that stands for several rows: how long the
/// problem has been going on, or how long ago the last of it closed.
struct SpanAgeCell: View {
    let rows: [Incident]
    let now: Date

    var body: some View {
        // The stored layout is fixed-width UTC, so string order is time order.
        let open = rows.filter { $0.closedAt == nil }
        if let oldest = open.map(\.openedAt).min(by: { $0.raw < $1.raw }) {
            AgeCell(timestamp: oldest, field: "oldest opened_at", now: now)
        } else {
            AgeCell(
                timestamp: rows.compactMap(\.closedAt).max(by: { $0.raw < $1.raw }),
                field: "newest closed_at", now: now)
        }
    }
}

/// distinctValues keeps the first of each equal value, in the order seen, so a
/// cell that stands for several rows lists what they say once each.
func distinctValues<T: Hashable>(_ values: [T]) -> [T] {
    var seen: Set<T> = []
    return values.filter { seen.insert($0).inserted }
}
