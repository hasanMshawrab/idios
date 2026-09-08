import IdiosModel
import SwiftUI

/// AddClusterSheet lets a person add a cluster from a kubeconfig context and
/// choose which of its namespaces idios watches.
struct AddClusterSheet: View {
    let store: ClustersStore
    /// cancelLabel names the leaving button; the setup sheet calls it Skip.
    var cancelLabel: LocalizedStringKey = "Cancel"

    @Environment(DaemonConnection.self) private var connection
    @Environment(\.dismiss) private var dismiss

    @State private var contexts: [KubeContext] = []
    @State private var isLoadingContexts = false
    @State private var selectedContext: KubeContext?
    @State private var name = ""
    // False once the person types into Name, so a later context choice stops
    // overwriting what they typed.
    @State private var nameFollowsContext = true
    @State private var namespaces: KubeNamespaces?
    @State private var isLoadingNamespaces = false
    @State private var checkedNamespaces: Set<String> = []
    @State private var namespaceText = ""
    @State private var isSubmitting = false
    // Set once AddCluster answers, so a retry after a namespace failure posts
    // the namespaces again without re-posting the cluster: the daemon
    // refuses a second cluster of the same name.
    @State private var createdCluster: Cluster?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Add cluster").font(.system(size: 13, weight: .semibold))
            contextSection
            if selectedContext != nil {
                TextField("Name", text: nameField)
                namespaceSection
            }
            // While the context list is empty, contextSection already shows
            // the load failure; showing it here too would print it twice.
            if !contexts.isEmpty, let error = store.actionError, !error.message.isEmpty {
                Text(error.message)
                    .font(.system(size: 11.5))
                    .foregroundStyle(BadgeStyle.red.text)
            }
            HStack {
                Spacer()
                Button(cancelLabel) { dismiss() }
                    .keyboardShortcut(.cancelAction)
                Button("Add") { Task { await add() } }
                    .keyboardShortcut(.defaultAction)
                    .disabled(selectedContext == nil || name.isEmpty || isSubmitting)
            }
        }
        .padding(20)
        .frame(minWidth: 440)
        .task {
            store.clearActionError()
            await loadContexts()
        }
        .task(id: selectedContext) { await loadNamespaces() }
    }

    @ViewBuilder private var contextSection: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Context").font(.system(size: 11.5, weight: .semibold)).foregroundStyle(.secondary)
            if isLoadingContexts {
                ProgressView().controlSize(.small)
            } else if contexts.isEmpty, let error = store.actionError {
                Text(error.message)
                    .font(.system(size: 11.5))
                    .foregroundStyle(BadgeStyle.red.text)
            } else {
                List(selection: $selectedContext) {
                    ForEach(contexts, id: \.self) { context in
                        contextRow(context).tag(context)
                    }
                }
                .listStyle(.bordered)
                .frame(height: 140)
                .onChange(of: selectedContext) {
                    guard nameFollowsContext else { return }
                    name = selectedContext?.name ?? ""
                }
                if selectedContext == nil {
                    Text("Choose a context above.")
                        .font(.system(size: 10.5))
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private func contextRow(_ context: KubeContext) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(context.name)
            if let secondary = secondaryLine(context) {
                Text(secondary)
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
            }
        }
    }

    // Neither field is guaranteed on a kubeconfig context; the line shows
    // what is there and collapses when neither is.
    private func secondaryLine(_ context: KubeContext) -> String? {
        let parts = [context.cluster, context.server].compactMap { $0 }
        return parts.isEmpty ? nil : parts.joined(separator: " - ")
    }

    @ViewBuilder private var namespaceSection: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Namespaces")
                .font(.system(size: 11.5, weight: .semibold))
                .foregroundStyle(.secondary)
            if isLoadingNamespaces {
                ProgressView().controlSize(.small)
            } else if let namespaces, !namespaces.forbidden {
                NamespacePicker(names: namespaces.names, checked: $checkedNamespaces)
            }
            // A namespace the Role cannot list, or one that does not exist
            // yet, can still be typed here even while the checklist is shown.
            TextField("one namespace per line", text: $namespaceText, axis: .vertical)
                .lineLimit(2...5)
        }
    }

    private var nameField: Binding<String> {
        Binding(
            get: { name },
            // AppKit re-writes the same value on a focus change; only an
            // actual edit should stop Name from tracking the context.
            set: { newValue in
                if newValue != name { nameFollowsContext = false }
                name = newValue
            })
    }

    private func loadContexts() async {
        isLoadingContexts = true
        contexts = await store.contexts(connection: connection)
        isLoadingContexts = false
    }

    private func loadNamespaces() async {
        namespaces = nil
        checkedNamespaces = []
        namespaceText = ""
        guard let selectedContext else { return }
        isLoadingNamespaces = true
        namespaces = await store.namespaces(of: selectedContext.name, connection: connection)
        isLoadingNamespaces = false
    }

    private func add() async {
        guard let selectedContext else { return }
        isSubmitting = true
        defer { isSubmitting = false }
        let cluster: Cluster
        if let createdCluster {
            cluster = createdCluster
        } else {
            guard let created = await store.add(
                context: selectedContext.name, name: name, connection: connection)
            else { return }
            createdCluster = created
            cluster = created
        }
        let names = NamespacePicker.namesToAdd(checked: checkedNamespaces, text: namespaceText)
        for namespace in names {
            guard await store.addNamespace(namespace, to: cluster.id, connection: connection)
            else { return }
        }
        dismiss()
    }
}
