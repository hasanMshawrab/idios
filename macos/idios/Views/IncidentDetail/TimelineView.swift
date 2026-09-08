import IdiosModel
import SwiftUI

/// TimelineFilter is which sources of the merged timeline a person asked to see.
enum TimelineFilter: String, CaseIterable, Hashable {
    case all
    case transitions
    case events
    case captures

    /// title names the filter on the segmented control.
    var title: String {
        switch self {
        case .all: "All"
        case .transitions: "Transitions"
        case .events: "Events"
        case .captures: "Captures"
        }
    }

    // Lifecycle, rollout and cut are the spine of the story rather than one of
    // its sources: they stay whatever a person narrowed to.
    fileprivate func keeps(_ kind: TimelineKind) -> Bool {
        switch kind {
        case .lifecycle, .rollout, .cut: true
        case .containerTransition, .condition: self == .all || self == .transitions
        case .event: self == .all || self == .events
        case .capture: self == .all || self == .captures
        }
    }
}

/// TimelineView is one incident's merged, time-ordered history: every
/// append-only row that touched the pod, in the order the daemon merged them.
struct TimelineView: View {
    let entries: [TimelineEntry]
    let truncated: Bool
    let isLoading: Bool
    let error: APIError?

    @State private var filter = TimelineFilter.all

    var body: some View {
        DetailCard(title: "Timeline", meta: meta) {
            Picker("", selection: $filter) {
                ForEach(TimelineFilter.allCases, id: \.self) { item in
                    Text(item.title).tag(item)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
            content
        }
    }

    @ViewBuilder private var content: some View {
        if let error {
            WrapText(value: error.message)
                .font(.system(size: 11.5))
                .foregroundStyle(.secondary)
        } else if visible.isEmpty {
            Text(entries.isEmpty && isLoading ? "Loading..." : emptyMessage)
                .font(.system(size: 11.5))
                .foregroundStyle(.secondary)
        } else {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(folds.enumerated()), id: \.offset) { _, fold in
                    if fold.repeats.isEmpty {
                        TimelineRow(entry: fold.lead)
                    } else {
                        FoldedTimelineRows(fold: fold)
                    }
                }
            }
        }
    }

    private var visible: [TimelineEntry] {
        entries.filter { filter.keeps($0.kind) }
    }

    // Folded after the filter, so a narrowing that hides a kind never folds
    // across the hole it leaves.
    private var folds: [TimelineFold] {
        timelineFolds(visible, minimumCycles: 3)
    }

    private var emptyMessage: String {
        entries.isEmpty
            ? "No history row survives for this incident."
            : "Nothing on this timeline came from that source."
    }

    private var meta: String {
        let counted = "\(visible.count) of \(entries.count) "
            + (entries.count == 1 ? "entry" : "entries")
        return truncated ? counted + " - list cut short by the daemon" : counted
    }
}

/// FoldedTimelineRows is a repeating cycle drawn as its first entry and a
/// summary, with the entries it stands for one chevron away.
private struct FoldedTimelineRows: View {
    let fold: TimelineFold

    @State private var expanded = false

    var body: some View {
        DisclosureGroup(isExpanded: $expanded) {
            ForEach(Array(fold.repeats.enumerated()), id: \.offset) { _, entry in
                TimelineRow(entry: entry)
            }
        } label: {
            TimelineRow(entry: fold.lead, summary: fold.summary)
        }
    }
}

/// TimelineRow draws one merged entry: when it happened, a dot in the colour of
/// what it is, and what it says.
struct TimelineRow: View {
    let entry: TimelineEntry
    /// summary stands for the entries folded behind this one, nil when none is.
    var summary: String?

    var body: some View {
        if entry.kind == .cut {
            cut
        } else {
            // The rail is drawn unpadded so it runs from row to row; the two
            // text columns carry the row's own vertical padding instead.
            HStack(alignment: .top, spacing: 8) {
                time.padding(.vertical, 4)
                rail
                VStack(alignment: .leading, spacing: 2) {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(title)
                            .font(.system(size: 11.5))
                            .lineLimit(nil)
                            .textSelection(.enabled)
                        if let summary {
                            Text(summary)
                                .font(.system(size: 11))
                                .foregroundStyle(.secondary)
                                .lineLimit(nil)
                        }
                    }
                    detail
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.vertical, 4)
            }
        }
    }

    // A sweep that removed rows from this span must interrupt the list, not sit
    // in it: a quiet stretch has to say it was trimmed.
    private var cut: some View {
        HStack(spacing: 8) {
            rule
            Text(entry.message ?? "rows removed by the sweeper")
                .font(.system(size: 10.5, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(nil)
            rule
        }
        .padding(.vertical, 8)
    }

    private var rule: some View {
        Rectangle()
            .fill(.clear)
            .frame(height: 1)
            .overlay(
                Rectangle()
                    .stroke(style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                    .foregroundStyle(.tertiary))
    }

    // One time on the row; the hover carries both stamps, so a reader who needs
    // to know which one is drawn does not need a second line to say so.
    private var time: some View {
        Text(clockTime(entry.k8sAt ?? entry.observedAt, seconds: true))
            .font(.system(size: 11, design: .monospaced))
            .foregroundStyle(.secondary)
            .frame(width: 72, alignment: .trailing)
            .help("k8s \(entry.k8sAt?.raw ?? "none") - observed \(entry.observedAt.raw)")
    }

    // A reconstructed row is drawn hollow and dashed because idios inferred it
    // from a restart count it never saw move.
    @ViewBuilder private var rail: some View {
        let colour = entry.badge.text
        ZStack(alignment: .top) {
            Rectangle()
                .fill(.tertiary)
                .frame(width: 1)
                .frame(maxHeight: .infinity)
            Group {
                if entry.gapReconstructed {
                    Circle()
                        .strokeBorder(colour, style: StrokeStyle(lineWidth: 1.5, dash: [2, 2]))
                } else {
                    Circle().fill(colour)
                }
            }
            .frame(width: 9, height: 9)
            .padding(.top, 3)
        }
        .frame(width: 11)
    }

    @ViewBuilder private var detail: some View {
        switch entry.kind {
        case .containerTransition:
            secondary(transitionDetail)
        case .condition:
            if let text = conditionDetail {
                secondary(text)
            }
        case .event:
            if let text = eventDetail {
                secondary(text)
            }
        case .capture:
            if let gap = entry.captureGap {
                GapBadge(gap: gap)
            }
        case .rollout:
            if let tag = entry.imageTag {
                secondary("image tag \(tag)")
            }
        case .lifecycle, .cut:
            EmptyView()
        }
    }

    private func secondary(_ text: String) -> some View {
        WrapText(value: text)
            .font(.system(size: 10.5, design: .monospaced))
            .foregroundStyle(.secondary)
    }

    private var title: String {
        switch entry.kind {
        case .containerTransition: transitionTitle
        case .condition:
            "\(entry.conditionType ?? "condition") \(entry.conditionStatus ?? "unknown")"
        case .event: "Event \(entry.eventReason ?? "unknown") x\(entry.count)"
        case .capture: "Captured \(captureLabel)"
        case .rollout:
            "ReplicaSet \(entry.replicasetName ?? "unknown") first seen, "
                + "revision \(entry.revision ?? "unknown")"
        case .lifecycle:
            entry.lifecycle == .closed
                ? "Incident closed: \(entry.closeReason?.rawValue ?? "unknown")"
                : "Incident opened"
        case .cut: entry.message ?? ""
        }
    }

    private var transitionTitle: String {
        var text = "\(entry.containerName ?? "container"): \(entry.state?.rawValue ?? "unknown")"
        if let reason = entry.reason, !reason.isEmpty {
            text += "(\(reason))"
        }
        if let exitCode = entry.exitCode {
            text += " exit \(exitCode)"
        }
        // The kubelet reports signal 0 for a container that was not signalled.
        if let signal = entry.signal, signal != 0 {
            text += ", signal \(signal)"
        }
        return text
    }

    private var transitionDetail: String {
        var parts = ["restart_count \(entry.restartCount)"]
        if let k8sAt = entry.k8sAt {
            parts.append("k8s \(k8sAt.raw)")
        }
        parts.append("observed \(entry.observedAt.raw)")
        if entry.gapReconstructed {
            parts.append("gap_reconstructed")
        }
        return parts.joined(separator: " - ")
    }

    private var conditionDetail: String? {
        var parts: [String] = []
        if let reason = entry.reason, !reason.isEmpty {
            parts.append(reason)
        }
        if let message = entry.message, !message.isEmpty {
            parts.append(message)
        }
        return parts.isEmpty ? nil : parts.joined(separator: " - ")
    }

    private var eventDetail: String? {
        var parts: [String] = []
        if let type = entry.eventType, !type.isEmpty {
            parts.append(type)
        }
        if let message = entry.message, !message.isEmpty {
            parts.append(message)
        }
        return parts.isEmpty ? nil : parts.joined(separator: " - ")
    }

    private var captureLabel: String {
        guard let kind = entry.artifactKind else { return "nothing" }
        return artifactLabel(kind: kind, restartCount: entry.restartCount)
    }
}
