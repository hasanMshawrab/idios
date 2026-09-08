import IdiosModel
import SwiftUI

/// RunStripView is the line of runs above the Runs table: one cell per run,
/// oldest first, with the hover, the click and the drag the runs answer to.
struct RunStripView: View {
    let cells: [RunCell]
    let now: Date
    let openRun: (String) -> Void
    let acknowledge: ([String]) -> Void
    /// marked is the run this strip is drawn beside, outlined so a person can
    /// see where they are in the line; nil on the Workloads tab, where the
    /// newest run is the only one worth pointing at.
    var marked: String?

    // The cell is six points wide with one point between cells, so a drag's x
    // divided by seven is the cell under the pointer.
    private static let pitch: CGFloat = 7

    @State private var dragged: ClosedRange<Int>?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            headline
            ScrollView(.horizontal) {
                HStack(spacing: 1) {
                    ForEach(Array(cells.enumerated()), id: \.offset) { index, cell in
                        cellView(index: index, cell: cell)
                    }
                }
                .padding(.vertical, 1)
                .gesture(drag)
            }
            Text(
                "hover: condition, duration and exit code - click: the run's page - "
                    + "drag: acknowledge those runs")
                .font(.system(size: 10))
                .foregroundStyle(.secondary)
        }
    }

    private var headline: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(span)
            Spacer(minLength: 8)
            Text(counts)
        }
        .font(.system(size: 10.5))
        .foregroundStyle(.secondary)
        .lineLimit(1)
    }

    private var span: String {
        let stamps = cells.compactMap(\.startedAt)
        let head = "Last \(plural(cells.count, "run")), oldest first"
        guard let first = stamps.first, let last = stamps.last else { return head }
        return head + " - \(clockTime(first)) to \(clockTime(last))"
    }

    private var counts: String {
        var failed = 0
        var complete = 0
        var running = 0
        var swept = 0
        for cell in cells {
            switch cell.outcome {
            case .failed: failed += 1
            case .complete: complete += 1
            case .running: running += 1
            case .swept: swept += 1
            }
        }
        var parts: [String] = []
        if failed > 0 { parts.append("\(failed) failed") }
        if complete > 0 { parts.append("\(complete) complete") }
        if running > 0 { parts.append("\(running) running") }
        if swept > 0 { parts.append("\(swept) not recorded (swept)") }
        return parts.joined(separator: " - ")
    }

    private func cellView(index: Int, cell: RunCell) -> some View {
        runCellShape(cell)
            .frame(width: 6, height: 22)
            .overlay {
                // The page a strip is drawn on is about one run, and that run
                // is where the reader is; with no page to be on, the newest
                // run is the one a person came to read, and it is at the far
                // end of a line of identical cells.
                if cell.jobUID == marked {
                    RoundedRectangle(cornerRadius: 1)
                        .strokeBorder(Color.accentColor, lineWidth: 1.5)
                } else if index == cells.count - 1 {
                    RoundedRectangle(cornerRadius: 1).strokeBorder(.primary, lineWidth: 1)
                }
            }
            .overlay {
                if dragged?.contains(index) == true {
                    RoundedRectangle(cornerRadius: 1)
                        .strokeBorder(Color.accentColor, lineWidth: 1.5)
                }
            }
            .contentShape(Rectangle())
            .help(cell.hover(now: now))
            .onTapGesture { openRun(cell.jobUID) }
    }

    private var drag: some Gesture {
        DragGesture(minimumDistance: 3)
            .onChanged { value in
                dragged = selection(from: value.startLocation.x, to: value.location.x)
            }
            .onEnded { value in
                let range = selection(from: value.startLocation.x, to: value.location.x)
                dragged = nil
                guard let range else { return }
                var ids: [String] = []
                var seen: Set<String> = []
                for cell in cells[range] {
                    for id in cell.openIncidentIDs where seen.insert(id).inserted {
                        ids.append(id)
                    }
                }
                guard !ids.isEmpty else { return }
                acknowledge(ids)
            }
    }

    private func selection(from: CGFloat, to: CGFloat) -> ClosedRange<Int>? {
        guard !cells.isEmpty else { return nil }
        let first = index(at: min(from, to))
        let last = index(at: max(from, to))
        return first...last
    }

    private func index(at x: CGFloat) -> Int {
        min(max(Int(x / Self.pitch), 0), cells.count - 1)
    }

}

/// RunMatrixView is the strip past its limit: one row per hour, one cell
/// per minute, so two thousand runs still fit a pane.
struct RunMatrixView: View {
    let matrix: RunMatrix
    let now: Date
    let openRun: (String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(matrix.rows) { row in
                HStack(spacing: 1) {
                    Text(clockTime(row.hour))
                        .font(.system(size: 10, design: .monospaced))
                        .monospacedDigit()
                        .foregroundStyle(.secondary)
                        .frame(width: 36, alignment: .trailing)
                        .padding(.trailing, 4)
                    ForEach(Array(row.minutes.enumerated()), id: \.offset) { _, cell in
                        minuteView(cell)
                    }
                }
            }
        }
    }

    @ViewBuilder private func minuteView(_ cell: RunCell?) -> some View {
        if let cell {
            runCellShape(cell)
                .frame(width: 4, height: 12)
                .contentShape(Rectangle())
                .help(cell.hover(now: now))
                .onTapGesture { openRun(cell.jobUID) }
        } else {
            RoundedRectangle(cornerRadius: 1)
                .fill(.quaternary)
                .frame(width: 4, height: 12)
        }
    }
}

/// runCellShape draws one run in its outcome's tone, hollow and dashed for the
/// run whose record the sweeper removed.
@ViewBuilder func runCellShape(_ cell: RunCell) -> some View {
    let style = runCellStyle(cell.outcome)
    if case .swept = cell.outcome {
        RoundedRectangle(cornerRadius: 1)
            .strokeBorder(
                style.text, style: StrokeStyle(lineWidth: 1, dash: style.borderDash ?? []))
    } else {
        RoundedRectangle(cornerRadius: 1).fill(style.text)
    }
}
