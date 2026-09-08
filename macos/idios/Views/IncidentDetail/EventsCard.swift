import IdiosModel
import SwiftUI

/// EventsCard lists Kubernetes events. The rows hold prose, so they are a list
/// of custom rows and not a Table: one row height for every row would cut the
/// messages.
struct EventsCard: View {
    let events: [Event]
    var title = "Attached events"
    var meta = "k8s_events WHERE incident_id"
    var emptyText = "No event is attached to this incident."
    /// incidentID is the incident the card is shown on: its own events read
    /// plain and the pod's other events are dimmed. A card that belongs to no
    /// single incident leaves it nil and dims nothing.
    var incidentID: String?
    /// showsContainer adds the column the Pod card's tab needs; a
    /// container's own pane leaves it off.
    var showsContainer = false
    /// marksPodScope tags a row with no field_path as the pod's own event,
    /// for a pane that is about one container.
    var marksPodScope = false

    // The prose columns are sized from the width the card is offered rather
    // than by min/max frames: a wrapping message's ideal width is its whole
    // text, and beside it a ranged column takes its ideal and the row grows
    // past the pane instead of giving way.
    @State private var rowWidth: CGFloat = 0

    private var columns: EventColumns.Widths {
        EventColumns.widths(available: rowWidth, showsContainer: showsContainer)
    }

    var body: some View {
        DetailCard(title: title, meta: meta) {
            if events.isEmpty {
                Text(emptyText)
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                heading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(events) { event in
                        Divider()
                        row(event).opacity(attached(event) ? 1 : 0.65)
                    }
                }
            }
            if events.contains(where: { !attached($0) }), incidentID != nil {
                Text(
                    "The dimmed rows are the pod's other events. An event attaches to the one "
                        + "incident that was open when it arrived, so this incident holds few of "
                        + "them or none.")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
            Text(
                "Events are rate-limited by Kubernetes itself (25 per object, refill 1 per 5 min); "
                    + "an absent event is not evidence that nothing happened. Container status is "
                    + "the authority.")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
        }
    }

    private func attached(_ event: Event) -> Bool {
        guard let incidentID else { return true }
        return event.incidentID == incidentID
    }

    private var heading: some View {
        HStack(alignment: .top, spacing: EventColumns.gap) {
            Text("TYPE").frame(width: EventColumns.type, alignment: .leading)
            Text("REASON").frame(width: columns.reason, alignment: .leading)
            if showsContainer {
                Text("CONTAINER").frame(width: columns.container, alignment: .leading)
            }
            Text("COUNT").frame(width: EventColumns.count, alignment: .trailing)
            Text("LAST_TS (k8s)").frame(width: EventColumns.stamp, alignment: .leading)
            Text("SOURCE").frame(width: columns.source, alignment: .leading)
            Text("MESSAGE").frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { rowWidth = $0 }
    }

    private func row(_ event: Event) -> some View {
        HStack(alignment: .top, spacing: EventColumns.gap) {
            Badge(text: event.type, style: style(event))
                .frame(width: EventColumns.type, alignment: .leading)
            HStack(spacing: 6) {
                Text(event.reason)
                    .font(.system(size: 11, design: .monospaced))
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .help(event.reason)
                if marksPodScope && eventContainer(fieldPath: event.fieldPath) == nil {
                    Badge(text: "pod", style: .neutral)
                }
            }
            .frame(width: columns.reason, alignment: .leading)
            if showsContainer {
                Text(eventContainer(fieldPath: event.fieldPath) ?? "")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .frame(width: columns.container, alignment: .leading)
            }
            Text("\(event.count)")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .frame(width: EventColumns.count, alignment: .trailing)
            VStack(alignment: .leading, spacing: 1) {
                Text(event.lastTS?.raw ?? "null")
                    .font(.system(size: 10.5, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                if let span = eventSpan(
                    count: event.count, firstTS: event.firstTS, lastTS: event.lastTS)
                {
                    Text(span)
                        .font(.system(size: 10))
                        .foregroundStyle(.tertiary)
                        .lineLimit(1)
                        .help("first_ts \(event.firstTS?.raw ?? "null")")
                }
            }
            .frame(width: EventColumns.stamp, alignment: .leading)
            Text(event.sourceComponent ?? "unknown")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .frame(width: columns.source, alignment: .leading)
            WrapText(value: event.message ?? "")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 5)
    }

    // Warning and Normal are not a stored vocabulary of their own; they borrow
    // the container-state colours the badge vocabulary already fixes.
    private func style(_ event: Event) -> BadgeStyle {
        event.type == "Warning" ? ContainerState.waiting.badge : .neutral
    }
}

/// EventColumns is the card's column budget: the three prose columns give
/// way in a narrow pane, MESSAGE keeps a floor, and the timestamp column is
/// fixed because a stamp that wraps is unreadable.
private enum EventColumns {
    static let type: CGFloat = 62
    static let reason = (min: 60.0, ideal: 124.0)
    static let container = (min: 62.0, ideal: 96.0)
    static let count: CGFloat = 44
    static let stamp: CGFloat = 190
    static let source = (min: 44.0, ideal: 74.0)
    static let message: CGFloat = 140
    static let gap: CGFloat = 10

    /// Widths is what the three prose columns get for one row width.
    struct Widths: Equatable {
        let reason: CGFloat
        let container: CGFloat
        let source: CGFloat
    }

    /// widths gives the prose columns their ideal while MESSAGE keeps its
    /// floor and shrinks them together, in proportion, as the pane narrows;
    /// below the floors MESSAGE takes what is left rather than the row
    /// growing past the pane.
    static func widths(available: CGFloat, showsContainer: Bool) -> Widths {
        let fixed = type + count + stamp + message + gap * (showsContainer ? 6 : 5)
        let mins = reason.min + source.min + (showsContainer ? container.min : 0)
        let ideals = reason.ideal + source.ideal + (showsContainer ? container.ideal : 0)
        let slack = min(max((available - fixed - mins) / (ideals - mins), 0), 1)
        func fit(_ column: (min: Double, ideal: Double)) -> CGFloat {
            column.min + (column.ideal - column.min) * slack
        }
        return Widths(
            reason: fit(reason), container: showsContainer ? fit(container) : 0,
            source: fit(source))
    }
}
