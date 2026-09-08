import Foundation
import IdiosModel
import SwiftUI

/// RailHeading names one group of the rail.
struct RailHeading: View {
    let text: String

    var body: some View {
        Text(text)
            .font(.system(size: 9.5, weight: .bold))
            .foregroundStyle(.secondary)
    }
}

/// RailRow is one label and one value of the rail, stacked so a raw timestamp is
/// never cut by the rail's width.
struct RailRow<Value: View>: View {
    let label: String
    @ViewBuilder let value: Value

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(label)
                .font(.system(size: 10.5))
                .foregroundStyle(.secondary)
                .frame(width: 96, alignment: .leading)
            value
                .font(.system(size: 11))
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// DeletedBanner says the pod is gone, how idios learned it, what it inferred,
/// and how long the record stays.
struct DeletedBanner: View {
    let deletedAt: Timestamp?
    let source: DeletionSource?
    let reason: DeletionReason?
    let retentionDays: Int32?

    var body: some View {
        DetailCard {
            HStack(alignment: .top, spacing: 12) {
                Badge(
                    text: source == .unwatched ? "unwatched" : "pod deleted",
                    style: IncidentState.podDeleted.badge)
                VStack(alignment: .leading, spacing: 3) {
                    FactRow(label: "deleted_at", labelWidth: 116) {
                        Text(deletedAt?.raw ?? "null")
                            .font(.system(size: 11, design: .monospaced))
                    }
                    FactRow(label: "deletion_source", labelWidth: 116) {
                        Text(source?.label ?? "unknown")
                            .help(source?.rawValue ?? "deletion_source is not stored")
                    }
                    FactRow(label: "deletion_reason", labelWidth: 116) {
                        (Text(reason?.label ?? "unknown")
                            + Text(" (inferred)").foregroundStyle(.tertiary))
                            .help(reason?.rawValue ?? "deletion_reason is not stored")
                    }
                    FactRow(label: "kept until", labelWidth: 116) { Text(keptUntil) }
                    Text(caveat)
                        .lineLimit(nil)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .help(caveat)
                        .padding(.top, 3)
                }
            }
        }
    }

    // How idios learned of the deletion decides what the record means: only a
    // watch event dates it, and an unwatched namespace is not a deletion at all.
    private var caveat: String {
        switch source {
        case .reconcile:
            return "Vanished while idios was not running; the clock for retention starts at the "
                + "reconcile, not the real deletion."
        case .unwatched:
            return "deletion_source unwatched: the namespace was removed from "
                + "watched_namespaces. \"We stopped looking\" is not \"it is gone\"."
        case .watch, nil:
            return "This Pod no longer exists. Its containers, history, incidents and files are "
                + "kept until the sweep."
        }
    }

    private var keptUntil: String {
        guard let retentionDays else { return "unknown: retention was not read" }
        guard let date = deletedAt?.date else { return "unknown: deleted_at is unreadable" }
        let sweep = date.addingTimeInterval(Double(retentionDays) * 86_400)
        return "\(utcMinute(sweep)) (retention \(retentionDays)d)"
    }
}

/// utcMinute writes an instant the daemon did not store, so it is not a stored
/// timestamp and is written to the minute in the UTC everything else uses.
func utcMinute(_ date: Date) -> String {
    let formatter = DateFormatter()
    formatter.locale = Locale(identifier: "en_US_POSIX")
    formatter.timeZone = TimeZone(identifier: "UTC")
    formatter.dateFormat = "yyyy-MM-dd HH:mm 'UTC'"
    return formatter.string(from: date)
}
