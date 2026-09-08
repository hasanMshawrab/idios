import IdiosModel
import SwiftUI

/// LegendPopover is what the "?" opens: the lifecycle, the states with their
/// tooltips and keys, and Attention with the daemon's window.
struct LegendPopover: View {
    let attentionWindowSeconds: Int32?
    let openStatus: () -> Void

    private static let states: [IncidentState] = [
        .open, .acknowledged, .recovered, .podDeleted, .jobFinished, .manual, .dismissed,
    ]

    // The lifecycle, seven states, the attention paragraph and eight kinds
    // outgrow a popover on a short display, so the body scrolls.
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                Text("Lifecycle")
                    .font(.system(size: 11, weight: .semibold))
                    .foregroundStyle(.secondary)
                lifecycle
                Divider()
                stateGrid
                Divider()
                attention
                Divider()
                kinds
                Button("Status...", action: openStatus)
                    .controlSize(.small)
            }
            .padding(14)
        }
        .frame(width: 460)
        .frame(maxHeight: 560)
    }

    // The badges wrap: the four closing states spell out wider than a popover
    // that has to stay narrow enough to point at a sidebar row.
    private var lifecycle: some View {
        Flow(spacing: 5, lineSpacing: 5) {
            StateBadge(state: .open)
            chevron
            StateBadge(state: .acknowledged)
            chevron
            Text("closed, by")
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
            StateBadge(state: .recovered)
            StateBadge(state: .podDeleted)
            StateBadge(state: .jobFinished)
            StateBadge(state: .manual)
        }
    }

    private var chevron: some View {
        Image(systemName: "chevron.right")
            .font(.system(size: 8, weight: .bold))
            .foregroundStyle(.secondary)
    }

    private var stateGrid: some View {
        Grid(alignment: .topLeading, horizontalSpacing: 10, verticalSpacing: 7) {
            ForEach(LegendPopover.states, id: \.self) { state in
                GridRow {
                    HStack(spacing: 6) {
                        Circle().fill(state.badge.text).frame(width: 7, height: 7)
                        Text(state.key.map { "\(state.title) (\($0))" } ?? state.title)
                            .font(.system(size: 11.5, weight: .semibold))
                    }
                    .gridColumnAlignment(.leading)
                    Text(state.tooltip)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .lineLimit(nil)
                }
            }
        }
    }

    private var attention: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Circle().fill(IncidentState.attention.badge.text).frame(width: 7, height: 7)
                Text(IncidentState.attention.title)
                    .font(.system(size: 11.5, weight: .semibold))
            }
            Text(attentionText)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .lineLimit(nil)
        }
    }

    // The kinds carry no colour of their own: a kind is not a state.
    private var kinds: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Kinds")
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(.secondary)
            Grid(alignment: .topLeading, horizontalSpacing: 10, verticalSpacing: 7) {
                ForEach(kindWords) { kind in
                    GridRow {
                        Text(kind.word)
                            .font(.system(size: 11.5, weight: .semibold))
                            .gridColumnAlignment(.leading)
                        Text(kind.sentence)
                            .font(.system(size: 11))
                            .foregroundStyle(.secondary)
                            .lineLimit(nil)
                    }
                }
            }
            VStack(alignment: .leading, spacing: 3) {
                ForEach(ownerChains, id: \.self) { chain in
                    Text(chain)
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(.tertiary)
                }
            }
        }
    }

    // The window is a daemon setting, so the sentence carries the value the
    // daemon reports and names where it comes from until the snapshot arrives.
    private var attentionText: String {
        let window =
            attentionWindowSeconds.map { humanDuration(seconds: $0) }
            ?? "the daemon's attention window (read from Status)"
        return IncidentState.attention.tooltip
            .replacingOccurrences(of: ", where N is the daemon's attention window", with: "")
            .replacingOccurrences(of: "N hours", with: window)
    }
}
