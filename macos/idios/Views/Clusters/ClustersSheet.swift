import IdiosModel
import SwiftUI

/// ClustersSheet is the two-pane master-detail of every cluster idios
/// watches: a rail of clusters on the left, the selected one's sections
/// (Cluster, Watched namespaces, Grafana, Remove) on the right.
struct ClustersSheet: View {
    let store: ClustersStore

    @Environment(DaemonConnection.self) private var connection
    @Environment(Preferences.self) private var preferences
    @Environment(\.dismiss) private var dismiss

    @State private var selectedClusterID: String?
    @State private var removeTarget: Cluster?
    @State private var showAddCluster = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Clusters and namespaces")
                .font(.system(size: 13, weight: .semibold))
                .padding(.horizontal, 16)
                .padding(.vertical, 14)
            HStack(spacing: 0) {
                rail
                    .frame(width: 196)
                Divider()
                detail
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            if let error = store.actionError, !error.message.isEmpty {
                Text(error.message)
                    .font(.system(size: 11))
                    .foregroundStyle(BadgeStyle.red.text)
                    .padding(.horizontal, 16)
                    .padding(.top, 4)
            }
            HStack {
                Spacer()
                Button("Done") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(16)
        }
        .frame(minWidth: 640, minHeight: 520)
        .task {
            store.clearActionError()
            reconcileSelection()
        }
        .onChange(of: store.clusters) { reconcileSelection() }
        .sheet(isPresented: $showAddCluster) {
            AddClusterSheet(store: store)
        }
        .alert(
            "Remove cluster \(removeTarget?.name ?? "")?", isPresented: removeAlertShown,
            presenting: removeTarget
        ) { cluster in
            Button("Remove", role: .destructive) {
                Task {
                    await store.remove(cluster.id, connection: connection, preferences: preferences)
                }
            }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text(
                "Its watcher stops within ten seconds. Every pod, incident, event and "
                    + "captured file of this cluster is removed now. This cannot be undone.")
        }
    }

    private var rail: some View {
        VStack(spacing: 0) {
            List(selection: $selectedClusterID) {
                ForEach(store.clusters) { cluster in
                    railRow(cluster).tag(cluster.id)
                }
            }
            .listStyle(.plain)
            Divider()
            Button("Add cluster...") { showAddCluster = true }
                .buttonStyle(.plain)
                .font(.system(size: 11.5))
                .padding(10)
        }
    }

    private func railRow(_ cluster: Cluster) -> some View {
        HStack(spacing: 7) {
            Circle()
                .fill(clusterDot(ready: cluster.ready, hasError: cluster.lastError != nil))
                .frame(width: 7, height: 7)
            VStack(alignment: .leading, spacing: 1) {
                Text(cluster.name)
                    .font(.system(size: 12, weight: .semibold))
                    .lineLimit(1)
                if let contextName = cluster.contextName {
                    Text(contextName)
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
            }
        }
        .padding(.vertical, 2)
    }

    @ViewBuilder private var detail: some View {
        if let cluster = selectedCluster {
            ClusterDetailPane(store: store, cluster: cluster, remove: { removeTarget = cluster })
                .id(cluster.id)
        } else {
            Color.clear
        }
    }

    private var selectedCluster: Cluster? {
        guard let selectedClusterID else { return nil }
        return store.cluster(id: selectedClusterID)
    }

    // The selection defaults to the first row and drops a cluster the
    // daemon no longer reports, so the detail pane never asks about a
    // cluster that is gone.
    private func reconcileSelection() {
        if let selectedClusterID, store.cluster(id: selectedClusterID) != nil { return }
        selectedClusterID = store.clusters.first?.id
    }

    private var removeAlertShown: Binding<Bool> {
        Binding(get: { removeTarget != nil }, set: { shown in if !shown { removeTarget = nil } })
    }
}
