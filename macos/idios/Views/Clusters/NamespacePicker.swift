import IdiosModel
import SwiftUI

/// NamespacePicker is the checklist of a cluster's namespaces, shared by the
/// add-cluster and manage-clusters sheets so a name checked here and one
/// typed alongside it merge the same way in both.
struct NamespacePicker: View {
    let names: [String]
    @Binding var checked: Set<String>

    @State private var filter = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            TextField("Filter namespaces", text: $filter)
                .font(.system(size: 11))
            HStack {
                Text("\(checked.count) of \(names.count) checked")
                    .font(.system(size: 10.5))
                    .foregroundStyle(.secondary)
                Spacer()
                Button(selectAllTitle) { toggleSelectAll() }
                    .font(.system(size: 10.5))
                    .disabled(filteredNames.isEmpty)
            }
            List {
                ForEach(filteredNames, id: \.self) { namespace in
                    Toggle(namespace, isOn: checkedBinding(namespace))
                        .toggleStyle(.checkbox)
                        .font(.system(size: 11))
                }
            }
            .listStyle(.bordered)
            .frame(height: 180)
        }
    }

    // A case-insensitive substring match, like the other filter fields in
    // this app: a cluster with sixty namespaces is otherwise found only by
    // reading the whole list.
    private var filteredNames: [String] {
        let needle = filter.trimmingCharacters(in: .whitespaces).lowercased()
        guard !needle.isEmpty else { return names }
        return names.filter { $0.lowercased().contains(needle) }
    }

    // "Select all" once any filtered name is unchecked; once a search has
    // every match checked already, the same button clears just those.
    private var selectAllTitle: String {
        filteredNames.allSatisfy(checked.contains) ? "Select none" : "Select all"
    }

    private func toggleSelectAll() {
        if selectAllTitle == "Select all" {
            checked.formUnion(filteredNames)
        } else {
            checked.subtract(filteredNames)
        }
    }

    private func checkedBinding(_ namespace: String) -> Binding<Bool> {
        Binding(
            get: { checked.contains(namespace) },
            set: { isOn in
                if isOn {
                    checked.insert(namespace)
                } else {
                    checked.remove(namespace)
                }
            })
    }
}

extension NamespacePicker {
    /// namesToAdd merges the checked names with one namespace per line of
    /// free text; a name the Role cannot list can still be typed. Sorted so
    /// the order posted is deterministic.
    static func namesToAdd(checked: Set<String>, text: String) -> [String] {
        var names = checked
        for line in text.split(separator: "\n") {
            let trimmed = line.trimmingCharacters(in: .whitespacesAndNewlines)
            if !trimmed.isEmpty { names.insert(trimmed) }
        }
        return names.sorted()
    }
}

/// AddNamespaceField is the inline control that starts watching a namespace:
/// a text field that submits on Return, plus the cluster's not-yet-watched
/// namespaces when its context answered - picking one adds it immediately,
/// because a checked name waiting for a Return nothing points at never lands.
struct AddNamespaceField: View {
    let store: ClustersStore
    let cluster: Cluster

    @Environment(DaemonConnection.self) private var connection
    @State private var namespaces: KubeNamespaces?
    @State private var isLoading = false
    @State private var filter = ""
    @State private var text = ""
    @State private var isSubmitting = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            if isLoading {
                ProgressView().controlSize(.small)
            } else if let namespaces, !namespaces.forbidden, !unwatchedNames.isEmpty {
                DisclosureGroup("From the cluster") {
                    clusterList
                }
                .font(.system(size: 11))
            }
            TextField("Add namespace...", text: $text)
                .textFieldStyle(.plain)
                .font(.system(size: 11))
                .disabled(isSubmitting)
                .onSubmit { Task { await submit() } }
        }
        .task { await load() }
    }

    // A plain stack, not a List: the detail pane already scrolls, and a
    // table nested inside that scroll view drops clicks on macOS.
    private var clusterList: some View {
        VStack(alignment: .leading, spacing: 2) {
            if unwatchedNames.count > 8 {
                TextField("Filter namespaces", text: $filter)
                    .font(.system(size: 11))
            }
            ForEach(filteredUnwatched, id: \.self) { namespace in
                Button {
                    Task { await add(namespace) }
                } label: {
                    HStack {
                        Text(namespace).font(.system(size: 11))
                        Spacer()
                        Image(systemName: "plus.circle")
                            .foregroundStyle(.secondary)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(isSubmitting)
            }
        }
        .padding(.top, 2)
    }

    // Namespaces the cluster already watches are dropped from the list;
    // adding one would only repeat a call the daemon already answered, and
    // a just-picked name leaves here by arriving there.
    private var unwatchedNames: [String] {
        guard let namespaces else { return [] }
        return namespaces.names.filter { !cluster.namespaces.contains($0) }
    }

    private var filteredUnwatched: [String] {
        let needle = filter.trimmingCharacters(in: .whitespaces).lowercased()
        guard !needle.isEmpty else { return unwatchedNames }
        return unwatchedNames.filter { $0.lowercased().contains(needle) }
    }

    private func load() async {
        guard let context = cluster.contextName else { return }
        isLoading = true
        namespaces = await store.namespaces(of: context, connection: connection)
        isLoading = false
    }

    private func add(_ namespace: String) async {
        isSubmitting = true
        defer { isSubmitting = false }
        _ = await store.addNamespace(namespace, to: cluster.id, connection: connection)
    }

    private func submit() async {
        let names = NamespacePicker.namesToAdd(checked: [], text: text)
        guard !names.isEmpty else { return }
        isSubmitting = true
        defer { isSubmitting = false }
        for namespace in names {
            guard await store.addNamespace(namespace, to: cluster.id, connection: connection)
            else { return }
        }
        text = ""
    }
}
