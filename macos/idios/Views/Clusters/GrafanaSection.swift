import IdiosModel
import SwiftUI

/// GrafanaSection is the per-cluster Grafana log-link configuration: the
/// connection fields and the label builder that is the selector's only
/// writer.
struct GrafanaSection: View {
    let store: ClustersStore
    let cluster: Cluster

    @Environment(DaemonConnection.self) private var connection

    @State private var url: String
    @State private var datasourceUID: String
    @State private var rows: [EditableRow]
    @State private var fetchedPreview: GrafanaPreviewValues?
    @State private var isSaving = false

    private static let pickerValues: [GrafanaLabelValue] = [
        .namespace, .pod, .container, .workload, .node, .cluster,
    ]

    init(store: ClustersStore, cluster: Cluster) {
        self.store = store
        self.cluster = cluster
        _url = State(initialValue: cluster.grafanaURL)
        _datasourceUID = State(initialValue: cluster.lokiDatasourceUID)
        _rows = State(initialValue: GrafanaSection.storedLabels(for: cluster).rows.map { EditableRow(row: $0) })
    }

    var body: some View {
        SheetSection(header: {
            HStack(spacing: 6) {
                GrafanaMark()
                Text("Grafana")
                if cluster.grafanaURL.isEmpty {
                    Text("not configured")
                        .font(.system(size: 9.5, weight: .semibold))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Capsule().fill(.quaternary))
                        .foregroundStyle(.secondary)
                }
            }
        }) {
            row(label: "Grafana URL") {
                TextField("https://your-org.grafana.net", text: $url)
                    .textFieldStyle(.plain)
                    .font(.system(size: 11.5, design: .monospaced))
            }
            row(label: "Loki datasource") {
                TextField("datasource uid", text: $datasourceUID)
                    .textFieldStyle(.plain)
                    .font(.system(size: 11.5, design: .monospaced))
            }
            ForEach($rows) { $row in
                labelRow($row)
            }
            Button("Add label") {
                rows.append(EditableRow(row: GrafanaLabelRow(name: "", value: .text(""))))
            }
            .buttonStyle(.plain)
            .font(.system(size: 11))
            previewLine
            // A failed save reads from the sheet's shared action-error line;
            // a second copy here would show every error twice.
            HStack {
                if !cluster.grafanaURL.isEmpty {
                    Button("Clear") { Task { await clear() } }
                        .buttonStyle(.plain)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .disabled(isSaving)
                }
                Spacer()
                Button("Save") { Task { await save() } }
                    .disabled(!canSave || isSaving)
            }
        }
        .task(id: cluster.id) {
            fetchedPreview = await store.previewPod(of: cluster.id, connection: connection)
        }
        .onChange(of: cluster.grafanaURL) { resetFromStored() }
        .onChange(of: cluster.lokiDatasourceUID) { resetFromStored() }
        .onChange(of: cluster.logSelector) { resetFromStored() }
    }

    private func labelRow(_ row: Binding<EditableRow>) -> some View {
        HStack(spacing: 6) {
            TextField("label", text: row.name)
                .textFieldStyle(.plain)
                .font(.system(size: 11, design: .monospaced))
                .frame(width: 90)
            Text("=").font(.system(size: 11)).foregroundStyle(.secondary)
            Menu(valueLabel(row.wrappedValue.value)) {
                ForEach(GrafanaSection.pickerValues, id: \.self) { value in
                    Button(valueLabel(value)) { row.wrappedValue.value = value }
                }
                Button(valueLabel(.text(""))) { row.wrappedValue.value = .text("") }
            }
            .font(.system(size: 11))
            if case .text(let text) = row.wrappedValue.value {
                TextField(
                    "value", text: Binding(get: { text }, set: { row.wrappedValue.value = .text($0) })
                )
                .textFieldStyle(.plain)
                .font(.system(size: 11, design: .monospaced))
                .frame(width: 100)
            }
            Button {
                rows.removeAll { $0.id == row.wrappedValue.id }
            } label: {
                Image(systemName: "minus.circle")
            }
            .buttonStyle(.plain)
            .foregroundStyle(.secondary)
        }
    }

    private var previewLine: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text("Preview").font(.system(size: 10.5)).foregroundStyle(.secondary)
            Text(previewText)
                .font(.system(size: 10.5, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(nil)
        }
    }

    private var previewText: String {
        currentLabels.preview(values: previewValues.values)
    }

    // A cluster with no pod yet still gets a preview, from fixed sample
    // values naming no real workload.
    private var previewValues: GrafanaPreviewValues {
        fetchedPreview
            ?? GrafanaPreviewValues(
                namespace: "shop", pod: "web-6f7d9c-4x2lp", container: "app", workload: "web",
                node: "node-a", cluster: cluster.name)
    }

    private var currentLabels: GrafanaLabels {
        GrafanaLabels(rows: rows.map { GrafanaLabelRow(name: $0.name, value: $0.value) })
    }

    // Half-typed edits never autosave: the daemon validates the three
    // fields together, so a per-keystroke PATCH would reject every one.
    private var canSave: Bool {
        guard !url.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return false }
        return url != cluster.grafanaURL || datasourceUID != cluster.lokiDatasourceUID
            || currentLabels.selector != cluster.logSelector
    }

    private func save() async {
        isSaving = true
        defer { isSaving = false }
        _ = await store.setGrafana(
            cluster.id, url: url, datasourceUID: datasourceUID, selector: currentLabels.selector,
            connection: connection)
    }

    private func clear() async {
        isSaving = true
        defer { isSaving = false }
        _ = await store.setGrafana(
            cluster.id, url: "", datasourceUID: "", selector: "", connection: connection)
    }

    // The stream echoes a save (or another window's edit) as an updated
    // row; local state follows it so Save disables again once it lands.
    private func resetFromStored() {
        url = cluster.grafanaURL
        datasourceUID = cluster.lokiDatasourceUID
        rows = GrafanaSection.storedLabels(for: cluster).rows.map { EditableRow(row: $0) }
    }

    private static func storedLabels(for cluster: Cluster) -> GrafanaLabels {
        cluster.logSelector.isEmpty ? .standard : GrafanaLabels(selector: cluster.logSelector)
    }

    private func row<Content: View>(
        label: String, @ViewBuilder content: () -> Content
    ) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label)
                .font(.system(size: 10.5))
                .foregroundStyle(.secondary)
                .frame(width: 100, alignment: .leading)
            content()
            Spacer(minLength: 0)
        }
    }

    private func valueLabel(_ value: GrafanaLabelValue) -> String {
        switch value {
        case .namespace: "Namespace"
        case .pod: "Pod name"
        case .container: "Container name"
        case .workload: "Workload name"
        case .node: "Node name"
        case .cluster: "Cluster name"
        case .text: "Text"
        }
    }
}

// EditableRow gives one label row its own identity, separate from the
// label name: two rows may share a name mid-edit, and a ForEach keyed on
// the name would then merge them.
private struct EditableRow: Identifiable {
    let id = UUID()
    var name: String
    var value: GrafanaLabelValue

    init(name: String, value: GrafanaLabelValue) {
        self.name = name
        self.value = value
    }

    init(row: GrafanaLabelRow) {
        self.name = row.name
        self.value = row.value
    }
}
