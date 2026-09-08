import IdiosModel
import SwiftUI

/// ClusterDetailPane is the selected cluster's four sections: identity and
/// rename, watched namespaces, the Grafana log-link configuration, and
/// removal.
struct ClusterDetailPane: View {
    let store: ClustersStore
    let cluster: Cluster
    let remove: () -> Void

    @Environment(DaemonConnection.self) private var connection
    @State private var name: String

    init(store: ClustersStore, cluster: Cluster, remove: @escaping () -> Void) {
        self.store = store
        self.cluster = cluster
        self.remove = remove
        _name = State(initialValue: cluster.name)
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                clusterSection
                namespacesSection
                GrafanaSection(store: store, cluster: cluster)
                removeSection
            }
            .padding(16)
        }
        // The field tracks the stored name so a rename applied through the
        // stream shows without a second edit.
        .onChange(of: cluster.name) { name = cluster.name }
    }

    private var clusterSection: some View {
        SheetSection("Cluster") {
            row(label: "Name") {
                TextField("Name", text: $name)
                    .textFieldStyle(.plain)
                    .font(.system(size: 12.5, weight: .semibold))
                    .onSubmit { Task { await rename() } }
            }
            if let contextName = cluster.contextName {
                row(label: "Context") {
                    Text(contextName).font(.system(size: 11.5, design: .monospaced))
                }
            }
            if let apiServerURL = cluster.apiServerURL {
                row(label: "API server") {
                    Text(apiServerURL).font(.system(size: 11.5, design: .monospaced))
                }
            }
            row(label: "Connection") {
                HStack(spacing: 6) {
                    Circle()
                        .fill(clusterDot(ready: cluster.ready, hasError: cluster.lastError != nil))
                        .frame(width: 7, height: 7)
                    Text(connectionLine).font(.system(size: 11.5))
                }
            }
        }
    }

    private var connectionLine: String {
        var parts = [cluster.ready ? "ready" : "not ready"]
        if let lastEventAt = cluster.lastEventAt {
            parts.append("last event \(timeOfDayUTC(lastEventAt))")
        }
        if let skewSeconds = cluster.skewSeconds {
            parts.append("skew \(String(format: "%.1f", skewSeconds))s")
        }
        return parts.joined(separator: " - ")
    }

    private var namespacesSection: some View {
        SheetSection(header: {
            HStack(spacing: 6) {
                Text("Watched namespaces")
                if !cluster.namespaces.isEmpty {
                    Text("\(cluster.namespaces.count)")
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(.tertiary)
                }
            }
        }) {
            ForEach(cluster.namespaces, id: \.self) { namespace in
                namespaceRow(namespace)
            }
            AddNamespaceField(store: store, cluster: cluster)
        }
    }

    private func namespaceRow(_ namespace: String) -> some View {
        HStack {
            Text(namespace).font(.system(size: 11.5))
            Spacer()
            Button {
                Task {
                    await store.removeNamespace(namespace, from: cluster.id, connection: connection)
                }
            } label: {
                Image(systemName: "minus.circle")
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
        }
    }

    private var removeSection: some View {
        SheetSection("Remove") {
            VStack(alignment: .leading, spacing: 3) {
                Button("Remove cluster...", role: .destructive, action: remove)
                    .buttonStyle(.plain)
                    .font(.system(size: 11.5))
                    .foregroundStyle(BadgeStyle.red.text)
                Text(
                    "Its watcher stops within ten seconds. Every pod, incident, event and "
                        + "captured file of this cluster is removed now. This cannot be undone.")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
                    .lineLimit(nil)
            }
        }
    }

    private func row<Content: View>(
        label: String, @ViewBuilder content: () -> Content
    ) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label)
                .font(.system(size: 10.5))
                .foregroundStyle(.secondary)
                .frame(width: 70, alignment: .leading)
            content()
            Spacer(minLength: 0)
        }
    }

    private func rename() async {
        let requested = name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard requested != cluster.name, !requested.isEmpty else {
            name = cluster.name
            return
        }
        guard await store.rename(cluster.id, to: requested, connection: connection) else {
            name = cluster.name
            return
        }
    }
}

// timeOfDayUTC renders a stored timestamp as the clock face the mockup
// shows, falling back to the raw string when it did not parse.
private func timeOfDayUTC(_ timestamp: Timestamp) -> String {
    guard let date = timestamp.date else { return timestamp.raw }
    let formatter = DateFormatter()
    formatter.dateFormat = "HH:mm:ss"
    formatter.timeZone = TimeZone(identifier: "UTC")
    return formatter.string(from: date) + " UTC"
}

/// SheetSection is one titled inset group of the clusters sheet's detail
/// pane; every section shares the same margins so nothing looks like it
/// belongs to a different sheet.
struct SheetSection<Header: View, Content: View>: View {
    let header: Header
    let content: Content

    init(@ViewBuilder header: () -> Header, @ViewBuilder content: () -> Content) {
        self.header = header()
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            header
                .font(.system(size: 11.5, weight: .semibold))
                .foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: 6) {
                content
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 8).fill(.quaternary.opacity(0.35)))
        }
    }
}

extension SheetSection where Header == Text {
    /// init builds a section whose header is a plain title.
    init(_ title: String, @ViewBuilder content: () -> Content) {
        self.header = Text(title)
        self.content = content()
    }
}
