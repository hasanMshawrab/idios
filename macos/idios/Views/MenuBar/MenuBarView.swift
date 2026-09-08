import AppKit
import IdiosModel
import SwiftUI

/// MenuBarLabel is what the status item shows: the open count, red when a
/// cluster is not being watched as asked.
struct MenuBarLabel: View {
    let store: MenuBarStore
    let connection: DaemonConnection

    var body: some View {
        Label {
            Text("\(store.openCount)").monospacedDigit()
        } icon: {
            // The asset is a template image, so the tint below is the only
            // thing that colours it; the app mark's own red tittle is not in
            // the glyph, which is why an unwatched cluster can own red here.
            Image(.menuBarGlyph)
                .foregroundStyle(store.clustersWithError > 0 ? BadgeStyle.red.text : Color.primary)
        }
        .task(id: connection.generation) { await store.watch(connection: connection) }
    }
}

/// MenuBarView is the status item popover: the open count, the incidents that
/// need attention and one line per cluster.
struct MenuBarView: View {
    let store: MenuBarStore
    let connection: DaemonConnection

    /// open is called with the route a click asks the main window to show.
    let open: (Route) -> Void

    /// openGroup asks the main window to show one group in the list; the
    /// popover closes behind it as every other navigation does.
    let openGroup: (GroupTarget) -> Void

    /// attentionWindowSeconds is the daemon's attention window, which is what
    /// the tail sentence names rather than a number written here.
    let attentionWindowSeconds: Int32?

    @Environment(\.dismiss) private var dismiss
    @State private var confirmingAcknowledge = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            header
            Divider()
            if case .unreachable(let description) = connection.state {
                notConnected(description)
            } else {
                incidents
                Divider()
                clusters
            }
            Divider()
            actions
        }
        .frame(width: 360)
        .confirmationDialog(
            "Acknowledge \(plural(acknowledgeableCount, "incident"))?",
            isPresented: $confirmingAcknowledge
        ) {
            Button("Acknowledge") { acknowledgeShown() }
            Button("Cancel", role: .cancel) {}
                .keyboardShortcut(.cancelAction)
        }
    }

    // A person who acknowledges from here is watching the numbers fall, so the
    // popover stays open over the write.
    private func acknowledgeShown() {
        Task { _ = await store.acknowledgeShown(connection: connection) }
    }

    private var acknowledgeableCount: Int {
        store.rows.filter { $0.closedAt == nil && $0.acknowledgedAt == nil }.count
    }

    // Every action that leaves the popover for the main window runs through
    // here so the popover does not linger over it. dismiss() closes the
    // hosting window in the .window MenuBarExtra style; MenuBarView is also
    // reused as the plain "menubar" screenshot window, where the same
    // dismiss() would close the window the capture tool is about to
    // photograph, so it only fires when the window that was key before open
    // activated the main window is not that screenshot window.
    private func navigate(_ route: Route) {
        let hostWindow = NSApp.keyWindow
        open(route)
        guard hostWindow?.identifier?.rawValue.contains(Screenshot.mainWindowIdentifier) != true
        else { return }
        dismiss()
    }

    private var header: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(store.openCount > 0 ? BadgeStyle.red.text : Color.secondary)
                .frame(width: 8, height: 8)
            Text(headline)
                .font(.system(size: 12.5, weight: .semibold))
            if let subline {
                Text(subline)
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            Text(clusterSummary)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 14)
        .padding(.top, 12)
        .padding(.bottom, 10)
    }

    private var headline: String {
        store.openCount == 0 ? "Nothing is open" : "\(store.openCount) open"
    }

    private var subline: String? {
        switch store.attentionCount {
        case 0: nil
        case 1: "1 needs attention"
        default: "\(store.attentionCount) need attention"
        }
    }

    private var clusterSummary: String {
        let count = store.clusters.count
        let clusters = count == 1 ? "1 cluster" : "\(count) clusters"
        guard store.clustersWithError > 0 else { return clusters }
        return "\(clusters) - \(store.clustersWithError) with an error"
    }

    // The list's own grouping function folds the popover's rows, so a CronJob
    // failing every two minutes is one line in both places and the two can
    // never disagree about what one problem is. The menu bar ignores the
    // cluster scope, so the cluster count it passes is the number of clusters
    // the daemon reports.
    private var groups: [IncidentGroup] {
        let all = incidentGroups(
            rows: store.rows, grouping: .workload,
            cluster: { id in store.clusters.first { $0.id == id } },
            selectedClusters: store.clusters.count, facts: { _ in nil })
            .flatMap(\.groups)
        // A problem still open outranks one that closed; inside each half the
        // daemon's own last_seen_at order is what the rows keep.
        return all.filter { $0.openCount > 0 } + all.filter { $0.openCount == 0 }
    }

    @ViewBuilder private var incidents: some View {
        if store.rows.isEmpty {
            Text("Nothing needs attention")
                .font(.system(size: 11.5))
                .foregroundStyle(.secondary)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
        } else {
            let groups = groups
            let shown = groups.prefix(5)
            let hidden = groups.dropFirst(shown.count)
            VStack(alignment: .leading, spacing: 0) {
                ForEach(shown) { group in
                    groupRow(group)
                }
                if let tail = menuBarTail(
                    hidden: hidden.count, allClosed: hidden.allSatisfy { $0.openCount == 0 },
                    windowSeconds: attentionWindowSeconds)
                {
                    MenuBarItem(action: { navigate(.incidents(.attention)) }) {
                        Text(tail)
                            .font(.system(size: 11.5))
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            }
            .padding(6)
        }
    }

    /// groupRow is one problem: its state dot, the workload it is about, the
    /// plain-language summary the list's header uses and the newest time under
    /// it.
    private func groupRow(_ group: IncidentGroup) -> some View {
        MenuBarItem(action: { reveal(group) }) {
            HStack(spacing: 8) {
                Circle()
                    .fill(groupBadge(group.rows).tone.badge.text)
                    .frame(width: 7, height: 7)
                Text(groupTitle(group))
                    .font(.system(size: 12))
                    .lineLimit(1)
                    .truncationMode(.tail)
                    // The workload is what a person is looking for, so the
                    // summary beside it is what gives up width first.
                    .layoutPriority(1)
                Text("- \(group.summary)")
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                Spacer(minLength: 8)
                if let newest = group.newestSeen {
                    Text(clockTime(newest))
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(.secondary)
                }
            }
        }
        // The row is one line wide, so the whole of it and the state the dot
        // stands for are on hover.
        .help("\(groupTitle(group)) - \(group.summary) - \(groupBadge(group.rows).text)")
    }

    // The menu bar ignores cluster scope, so a workload name alone is
    // ambiguous once a second cluster exists; group.meta already carries the
    // cluster and namespace the grouping chose for that case.
    private func groupTitle(_ group: IncidentGroup) -> String {
        let name = "\(group.kind) \(group.title)"
        guard store.clusters.count > 1 else { return name }
        return "\(group.meta) / \(name)"
    }

    private func reveal(_ group: IncidentGroup) {
        guard let clusterID = group.rows.first?.clusterID else { return }
        let hostWindow = NSApp.keyWindow
        openGroup(GroupTarget(id: group.id, clusterID: clusterID))
        guard hostWindow?.identifier?.rawValue.contains(Screenshot.mainWindowIdentifier) != true
        else { return }
        dismiss()
    }

    private var clusters: some View {
        VStack(alignment: .leading, spacing: 6) {
            if store.clusters.isEmpty {
                Text("No clusters")
                    .foregroundStyle(.secondary)
            }
            ForEach(store.clusters) { cluster in
                clusterRow(cluster)
            }
        }
        .font(.system(size: 11.5))
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
    }

    private func clusterRow(_ cluster: Cluster) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle()
                .fill(clusterDot(ready: cluster.ready, hasError: cluster.lastError != nil))
                .frame(width: 7, height: 7)
                .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
            Text(cluster.name).lineLimit(1)
            Spacer(minLength: 8)
            if let error = cluster.lastError {
                WrapText(value: error)
                    .font(.system(size: 11))
                    .foregroundStyle(BadgeStyle.red.text)
                    .multilineTextAlignment(.trailing)
            } else {
                Text(syncText(cluster))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func syncText(_ cluster: Cluster) -> String {
        guard let date = cluster.lastEventAt?.date else { return "no events yet" }
        return "synced - \(durationText(from: date, to: Date())) ago"
    }

    private func notConnected(_ description: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Not connected")
                .font(.system(size: 12, weight: .semibold))
            Text(connection.address)
                .font(.system(size: 11, design: .monospaced))
            WrapText(value: description)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var actions: some View {
        VStack(alignment: .leading, spacing: 0) {
            MenuBarItem(
                action: { navigate(.incidents(nil)) }, shortcut: "i",
                modifiers: [.command, .shift])
            {
                HStack {
                    Text("Open idios")
                    Spacer(minLength: 8)
                    Text("Cmd Shift I")
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(.secondary)
                }
            }
            MenuBarItem(
                action: { confirmingAcknowledge = true }, shortcut: "a", modifiers: [.option])
            {
                HStack {
                    Text("Acknowledge everything shown")
                    Spacer(minLength: 8)
                    Text("Opt A")
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(.secondary)
                }
            }
            .disabled(acknowledgeableCount == 0)
            MenuBarItem(action: { navigate(.status) }) {
                Text("Status...")
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            MenuBarItem(action: { navigate(.clusters) }) {
                Text("Clusters and namespaces...")
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .font(.system(size: 12))
        .padding(6)
    }
}

/// MenuBarItem is one clickable line of the popover, highlighted the way a menu
/// item is; the popover is a window, so nothing draws that for it.
struct MenuBarItem<Content: View>: View {
    let action: () -> Void
    var shortcut: KeyEquivalent?
    var modifiers: EventModifiers = .command
    @ViewBuilder let content: Content

    @Environment(\.isEnabled) private var isEnabled
    @State private var hovering = false

    var body: some View {
        button
            .buttonStyle(.plain)
            .opacity(isEnabled ? 1 : 0.4)
            .onHover { hovering = $0 }
    }

    @ViewBuilder private var button: some View {
        if let shortcut {
            plain.keyboardShortcut(shortcut, modifiers: modifiers)
        } else {
            plain
        }
    }

    private var highlight: AnyShapeStyle {
        hovering && isEnabled ? AnyShapeStyle(.selection) : AnyShapeStyle(.clear)
    }

    private var plain: some View {
        Button(action: action) {
            content
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 8)
                .frame(height: 26)
                .background(RoundedRectangle(cornerRadius: 5).fill(highlight))
                .contentShape(Rectangle())
        }
    }
}
