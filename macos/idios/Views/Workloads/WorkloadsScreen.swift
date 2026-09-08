import AppKit
import IdiosModel
import SwiftUI

/// WorkloadsTreeState is what the tree remembers: the selected workload, its
/// tab, the filter and the namespace groups a person closed.
struct WorkloadsTreeState {
    var selected: WorkloadKey?
    /// tab is read by the rail's "+ N more" line, which opens a workload on
    /// its Pods tab; it lives here rather than in the pane so it survives a
    /// route that sets it before the pane exists.
    var tab: WorkloadTab?
    var filter = ""
    /// collapsed names the closed groups rather than the open ones, because a
    /// group a person has not touched is open.
    var collapsed: Set<String> = []
}

/// WorkloadsScreen is the workload tree on the left and the selected workload's
/// window of incidents, revisions, restarts and pods on the right.
struct WorkloadsScreen: View {
    let clusters: [Cluster]
    let store: WorkloadsStore
    @Binding var tree: WorkloadsTreeState
    @Binding var route: Route
    let openPod: (String) -> Void
    let openIncident: (String) -> Void
    let openRun: (String) -> Void

    @Environment(Preferences.self) private var preferences
    @Environment(DaemonConnection.self) private var connection
    @Environment(ExplainState.self) private var explain
    @Environment(\.controlActiveState) private var activeState

    // The chips are the pane's own reading position, dropped when the
    // selection moves to a workload whose tabs are not the same ones; the
    // tab itself lives in tree so a route can set it before the pane exists.
    @State private var podFilter: WorkloadPodFilter?
    @State private var runFilter: WorkloadRunFilter?
    // A bare-pod row that stands for more than one kept pod has to ask which
    // one before it can open anything.
    @State private var chooser: BarePodRow?
    @State private var pendingAcknowledge: PendingAcknowledge?

    private var selected: WorkloadKey? { tree.selected }

    // Which table the overlay draws: the tree until a workload is selected,
    // and then the tab in view, because the tabs share no region. The tab is
    // resolved the way the pane resolves it, so a person on the Runs tab is
    // told about runs even though they never chose the tab.
    private var explainPart: WorkloadsPart {
        guard let selected else { return .tree }
        return selectedWorkloadTab(
            tree.tab, kind: selected.kind,
            hasRollouts: !(store.detail?.rollouts.isEmpty ?? true)
        ).part
    }

    // The first row of each kind stands for the shape of a row: the tree
    // recycles its rows, so a region hung on every one of them would follow
    // the scroll position instead.
    private var firstKind: String? { clusterGroups.first?.namespaces.first?.kinds.first?.id }

    private var firstBareRow: String? {
        clusterGroups.flatMap { cluster in
            cluster.namespaces.flatMap { $0.kinds.flatMap(\.bareRows) }
        }
        .first?.id
    }

    // The tabs open on a default the person has not chosen, and the request has
    // to carry it or the page would not be the one the chips draw as engaged.
    private var podsLive: Bool { (podFilter ?? .live) == .live }

    private var selectedRunFilter: WorkloadRunFilter? {
        guard let selected else { return runFilter }
        return runFilter ?? defaultRunFilter(store.strip(of: selected))
    }

    var body: some View {
        content
            // The key that explains a screen has to reach it wherever the
            // pointer is, and the tree's filter field keeps its own focus.
            .focusable()
            .focusEffectDisabled()
            .onKeyPress(keys: ["?"]) { _ in
                explain.toggle(.workloads(explainPart))
                return .handled
            }
            // The overlay is installed on the window's split view, so the
            // sidebar is under its scrim too; this screen publishes which of
            // its tables that overlay draws.
            .onAppear { explain.current = .workloads(explainPart) }
            .onChange(of: explainPart) { _, part in
                explain.current = .workloads(part)
                if explain.screen != nil { explain.open(.workloads(part)) }
            }
            .onDisappear {
                if explain.screen == .workloads(explainPart) { explain.close() }
                explain.current = nil
            }
            .task(id: ListKey(generation: connection.generation, scope: preferences.scope))
            {
                let wanted = wantedSelection()
                await store.load(connection: connection, scope: preferences.scope)
                adoptSelection(wanted)
            }
            .task(
                id: DetailKey(
                    generation: connection.generation, workload: selected, podsLive: podsLive)
            ) {
                guard let selected else { return }
                await store.loadDetail(selected, podsLive: podsLive, connection: connection)
            }
            .task(id: DetailKey(generation: connection.generation, workload: selected)) {
                guard let selected else { return }
                await store.loadIncidents(key: selected, connection: connection)
            }
            .task(id: DetailKey(generation: connection.generation, workload: selected)) {
                guard let selected, selected.kind == "CronJob" else { return }
                await store.loadStrip(selected, connection: connection)
            }
            .task(
                id: RunsKey(
                    generation: connection.generation, workload: selected,
                    live: selectedRunFilter == .live, failed: selectedRunFilter == .failed)
            ) {
                guard let selected, selected.kind == "CronJob" else { return }
                await store.loadRuns(
                    selected, live: selectedRunFilter == .live,
                    failed: selectedRunFilter == .failed, connection: connection)
            }
            .onChange(of: selected) { old, _ in
                guard old != nil else { return }
                tree.tab = nil
                podFilter = nil
                runFilter = nil
            }
            .onChange(of: activeState) { _, state in
                guard state == .key else { return }
                Task { await store.load(connection: connection, scope: preferences.scope) }
            }
    }

    @ViewBuilder private var content: some View {
        if case .unreachable(let description) = connection.state {
            NotConnectedView(
                address: connection.address, error: description, retry: connection.retry)
        } else {
            // A fixed tree keeps the detail pane's width predictable; long names
            // wrap inside the tree instead of pushing the divider around.
            HStack(spacing: 0) {
                treePane
                    .frame(width: 300)
                Divider()
                detailPane
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }

    @ViewBuilder private var treePane: some View {
        VStack(spacing: 0) {
            WorkloadsPaneHeader(collapseAll: collapseAll, expandAll: expandAll)
                .padding(.horizontal, 10)
                .explained("paneHeader")
            filterField
                .padding(.horizontal, 10)
                .padding(.bottom, 8)
            if preferences.scope.isEmpty {
                Text("No cluster is in scope. Check a cluster in the sidebar.")
                    .font(.system(size: 12))
                    .foregroundStyle(.tertiary)
                    .lineLimit(nil)
                    .padding(.horizontal, 10)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            } else {
                // The tree is a flat list of its own rows rather than nested
                // DisclosureGroups: SwiftUI backs those with an outline view
                // that crashes when a parent and its children close in one
                // update, which is what the Collapse button does.
                List {
                    ForEach(flatWorkloadTree(clusterGroups, collapsed: tree.collapsed)) { row in
                        treeLine(row)
                            .padding(.leading, CGFloat(row.level) * 16)
                    }
                }
                // The plain style takes the column's own background; the sidebar
                // material reads as a second, darker sidebar in light appearance.
                .listStyle(.plain)
            }
        }
        .onChange(of: selected) { _, key in
            guard let key else { return }
            route = .workload(
                cluster: key.cluster, namespace: key.namespace, kind: key.kind, name: key.name)
        }
        .confirmationDialog(
            acknowledgeTitle,
            isPresented: Binding(
                get: { pendingAcknowledge != nil },
                set: { open in if !open { pendingAcknowledge = nil } }),
            presenting: pendingAcknowledge
        ) { pending in
            Button("Acknowledge") { acknowledge(pending) }
            Button("Cancel", role: .cancel) {}
                .keyboardShortcut(.cancelAction)
        } message: { _ in
            Text(
                "Each incident is written on its own, so one that fails leaves the "
                    + "others done.")
        }
    }

    @ViewBuilder private func treeLine(_ row: WorkloadTreeRow) -> some View {
        switch row {
        case .cluster(let cluster):
            disclosureRow(
                id: clusterRowID(cluster.clusterID),
                label: treeRow(
                    weight: .semibold,
                    glyph: Circle().fill(clusterDotColor(cluster.clusterID)),
                    title: clusterName(cluster.clusterID), count: cluster.openIncidents))
        case .namespace(let group):
            disclosureRow(
                id: group.id,
                label: treeRow(
                    weight: .regular,
                    glyph: Circle().fill(
                        group.openIncidents > 0 ? BadgeStyle.red.text : BadgeStyle.grey.text),
                    title: group.namespace, count: group.openIncidents))
        case .kind(let kind):
            kindCaptionRow(kind)
                .explained("kindCaption", when: kind.id == firstKind)
        case .barePod(let bare):
            barePodRow(bare)
                .explained("barePods", when: bare.id == firstBareRow)
        case .workload(let workload):
            workloadRow(workload)
        }
    }

    // The chevron is the row's own: the tree no longer nests, so nothing draws
    // one for it.
    private func disclosureRow<Label: View>(id: String, label: Label) -> some View {
        let open = !tree.collapsed.contains(id)
        return HStack(alignment: .firstTextBaseline, spacing: 4) {
            Image(systemName: "chevron.right")
                .font(.system(size: 9, weight: .semibold))
                .foregroundStyle(.secondary)
                .rotationEffect(.degrees(open ? 90 : 0))
                .frame(width: 10)
                .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 3 }
            label
        }
        .contentShape(Rectangle())
        .onTapGesture {
            if open {
                tree.collapsed.insert(id)
            } else {
                tree.collapsed.remove(id)
            }
        }
        .listRowSeparator(.hidden)
    }

    private var acknowledgeTitle: String {
        guard let pending = pendingAcknowledge else { return "Acknowledge?" }
        return "Acknowledge \(plural(pending.count, "incident")) of \(pending.name)?"
    }

    private func acknowledge(_ pending: PendingAcknowledge) {
        pendingAcknowledge = nil
        Task {
            if let ids = pending.ids {
                _ = await store.acknowledge(ids: ids, connection: connection)
            } else {
                _ = await store.acknowledgeOpen(
                    key: pending.key, podUID: pending.podUID, connection: connection)
            }
        }
    }

    private func collapseAll() {
        var ids: Set<String> = []
        for cluster in clusterGroups {
            ids.insert(clusterRowID(cluster.clusterID))
            for group in cluster.namespaces { ids.insert(group.id) }
        }
        tree.collapsed = ids
    }

    private func expandAll() {
        tree.collapsed = []
    }

    private var filterField: some View {
        HStack(spacing: 5) {
            Image(systemName: "line.3.horizontal.decrease").foregroundStyle(.secondary)
            TextField("Filter workloads", text: $tree.filter)
                .textFieldStyle(.plain)
        }
        .font(.system(size: 12))
        .padding(.horizontal, 9)
        .frame(height: 24)
        .background(RoundedRectangle(cornerRadius: 6).fill(.quaternary.opacity(0.5)))
    }

    // Selection is the row's own: a List selection binding draws faintly when
    // the list is not the key view, and its table keeps drawing a selected row
    // that a filter has removed over whichever row took its place.
    private func workloadRow(_ row: Workload) -> some View {
        let isSelected = row.key == selected
        // The kind caption above the row already names the kind.
        let label = Text(row.workloadName).fontWeight(isSelected ? .semibold : .regular)
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            RoundedRectangle(cornerRadius: 2)
                .fill(isSelected ? Color.primary : BadgeStyle.grey.text)
                .frame(width: 7, height: 7)
                .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
            label
                .lineLimit(nil)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 4)
            count(row.openIncidents)
        }
        .padding(.vertical, 3)
        .padding(.horizontal, 6)
        .background(
            RoundedRectangle(cornerRadius: 5)
                .fill(isSelected ? Color.accentColor.opacity(0.18) : Color.clear))
        .contentShape(Rectangle())
        .onTapGesture { tree.selected = row.key }
        .contextMenu {
            rowMenu(
                row.key, name: row.workloadName, podUID: nil, openIncidents: row.openIncidents)
        }
        .listRowSeparator(.hidden)
    }

    // A caption is a heading and not a level: it sits at the indentation of the
    // rows it heads, and it is not selectable.
    private func kindCaptionRow(_ kind: KindGroup) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(kindCaption(kind: kind.kind, count: kind.captionCount).uppercased())
                .font(.system(size: 10, weight: .semibold))
                .kerning(0.5)
                .foregroundStyle(.secondary)
                .lineLimit(nil)
            Spacer(minLength: 4)
            count(kind.openIncidents)
        }
        .padding(.top, 6)
        .padding(.horizontal, 6)
        .help(
            kind.kind == "none"
                ? "pods no controller owns; one row per pod name" : kind.kind)
        .listRowSeparator(.hidden)
    }

    // The row is one name, so it opens the pod screen; a name that stands for
    // more than one kept pod asks which, and a daemon that did not say which
    // pod leaves nothing to open.
    private func barePodRow(_ row: BarePodRow) -> some View {
        let uid = row.live?.podUID ?? row.rows.first?.podUID
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            RoundedRectangle(cornerRadius: 2)
                .fill(BadgeStyle.grey.text)
                .frame(width: 7, height: 7)
                .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
            Text(row.name.isEmpty ? "pod" : row.name)
                .font(.system(size: 12, design: .monospaced))
                .foregroundStyle(
                    row.live == nil ? AnyShapeStyle(.tertiary) : AnyShapeStyle(.primary))
                .lineLimit(nil)
                .fixedSize(horizontal: false, vertical: true)
            if row.podCount > 1 {
                Text(plural(row.podCount, "pod"))
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 4)
            count(row.openIncidents)
        }
        .padding(.vertical, 3)
        .padding(.horizontal, 6)
        .contentShape(Rectangle())
        .onTapGesture {
            if row.needsChooser {
                chooser = row
            } else if let uid {
                openPod(uid)
            }
        }
        .help(uid == nil ? "the daemon did not say which pod this is" : "opens the pod")
        .popover(
            isPresented: Binding(
                get: { chooser == row }, set: { open in if !open { chooser = nil } }),
            arrowEdge: .trailing
        ) {
            chooserList(row)
        }
        .contextMenu {
            rowMenu(nil, name: row.name, podUID: uid, openIncidents: row.openIncidents)
        }
        .listRowSeparator(.hidden)
    }

    private func chooserList(_ row: BarePodRow) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(row.rows) { kept in
                Button {
                    chooser = nil
                    if let uid = kept.podUID { openPod(uid) }
                } label: {
                    HStack(spacing: 8) {
                        Text(kept.podName ?? "pod")
                            .font(.system(size: 12, design: .monospaced))
                        Spacer(minLength: 8)
                        Text(keptState(kept))
                            .font(.system(size: 11))
                            .foregroundStyle(.secondary)
                    }
                }
                .buttonStyle(.plain)
                .disabled(kept.podUID == nil)
            }
        }
        .padding(10)
        .frame(minWidth: 220)
    }

    // The row carries no deletion time, so a kept pod is live or it is gone.
    private func keptState(_ row: Workload) -> String {
        row.livePods > 0 ? "live" : "deleted"
    }

    /// rowMenu is the four things a tree row can do without leaving the tree.
    @ViewBuilder private func rowMenu(
        _ key: WorkloadKey?, name: String, podUID: String?, openIncidents: Int32
    ) -> some View {
        Button("Show incidents") { show(key, podUID: podUID, tab: .incidents) }
        Button("Show pods") { show(key, podUID: podUID, tab: .pods) }
        Button("Copy name") {
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(name, forType: .string)
        }
        Button("Acknowledge all open") {
            pendingAcknowledge = PendingAcknowledge(
                key: key, podUID: podUID, ids: nil, name: name,
                count: Int(openIncidents))
        }
        .disabled(openIncidents == 0)
    }

    // A bare-pod row has no workload node: its incidents and its pod are the
    // same page.
    private func show(_ key: WorkloadKey?, podUID: String?, tab: WorkloadTab) {
        guard let key else {
            if let podUID { openPod(podUID) }
            return
        }
        tree.selected = key
        tree.tab = tab
    }

    private func treeRow<Glyph: View>(
        weight: Font.Weight, glyph: Glyph, title: String, count value: Int32
    ) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            glyph
                .frame(width: 7, height: 7)
                .alignmentGuide(.firstTextBaseline) { $0[.bottom] - 2 }
            Text(title)
                .fontWeight(weight)
                .lineLimit(nil)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 4)
            count(value)
        }
        .listRowSeparator(.hidden)
    }

    private func count(_ value: Int32) -> some View {
        Text(value > 0 ? String(value) : "")
            .font(.system(size: 11, design: .monospaced))
            .monospacedDigit()
            .foregroundStyle(.secondary)
    }

    @ViewBuilder private var detailPane: some View {
        // The tree pane names the empty scope; a sentence here would say it
        // twice, and the no-workload text would say something untrue.
        if preferences.scope.isEmpty {
            Color.clear
        } else if let detail = store.detail, detail.workload.key == selected, let selected {
            WorkloadDetailView(
                detail: detail, runs: store.runs(of: selected),
                strip: store.strip(of: selected),
                incidents: store.incidents(of: selected),
                clusterName: clusterName(detail.workload.clusterID), tab: $tree.tab,
                podFilter: $podFilter, runFilter: $runFilter, openPod: openPod,
                openIncident: openIncident, openRun: openRun,
                acknowledge: { ids in
                    pendingAcknowledge = PendingAcknowledge(
                        key: nil, podUID: nil, ids: ids,
                        name: "the runs you dragged", count: ids.count)
                })
        } else if let error = store.error {
            VStack(spacing: 8) {
                Text("workloads").font(.title3.weight(.semibold))
                WrapText(value: error.message).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        // A selection with no detail yet, and a list still arriving, are the only
        // two waits; with neither, a spinner would say the screen is stuck.
        } else if selected != nil || store.isLoading {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if store.workloads.isEmpty {
            Text("No workload has been seen in the window.")
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            Text("Select a workload.")
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    // Read before the load and carried across it as a value: the tree state
    // lives in the screen that hosts this pane, and a binding read after an
    // await answers with the state as the body last saw it.
    private func wantedSelection() -> WorkloadKey? {
        if let selected { return selected }
        guard case .workload(let cluster, let namespace, let kind, let name) = route
        else { return nil }
        // The route spells the cluster by its name; every workload key carries
        // the cluster id, so an unresolved name would never match a row.
        let id = clusters.first { $0.name == cluster }?.id ?? cluster
        return WorkloadKey(cluster: id, namespace: namespace, kind: kind, name: name)
    }

    // A selection the daemon no longer reports (another daemon behind the same
    // address, a swept workload) is dropped, or the detail pane keeps asking
    // for a row that is not there.
    private func adoptSelection(_ wanted: WorkloadKey?) {
        // The route spells the cluster by name while keys carry the id, and
        // the clusters list loads beside this tree, so the name may not be
        // resolvable yet when the first load lands: a unique namespace, kind
        // and name match stands in until it is.
        if let wanted {
            let rows = store.workloads.filter { row in
                let key = row.key
                return key.namespace == wanted.namespace && key.kind == wanted.kind
                    && key.name == wanted.name
            }
            let resolved = rows.first { row in
                row.key.cluster == wanted.cluster
                    || clusterName(row.key.cluster) == wanted.cluster
            }
            if let row = resolved ?? (rows.count == 1 ? rows[0] : nil) {
                tree.selected = row.key
                return
            }
        }
        // A kind-none row is a pod and not a selection, so the first workload of
        // the tree is what the screen enters on.
        let rows = clusterGroups.flatMap { cluster in
            cluster.namespaces.flatMap { namespace in
                namespace.kinds.filter { $0.kind != "none" }.flatMap(\.rows)
            }
        }
        tree.selected = rows.first?.key
    }

    private var visibleRows: [Workload] {
        let needle = tree.filter.trimmingCharacters(in: .whitespaces).lowercased()
        guard !needle.isEmpty else { return store.workloads }
        return store.workloads.filter { row in
            // A kind-none row is a pod, so its pod name is the name a person
            // would type.
            [
                row.workloadName, row.podName ?? "", row.namespace, row.workloadKind,
                clusterName(row.clusterID),
            ]
            .contains { $0.lowercased().contains(needle) }
        }
    }

    private var clusterGroups: [ClusterGroup] {
        let byCluster = Dictionary(grouping: visibleRows, by: \.clusterID)
        return byCluster.keys.sorted { clusterName($0) < clusterName($1) }.map { clusterID in
            let byNamespace = Dictionary(grouping: byCluster[clusterID] ?? [], by: \.namespace)
            return ClusterGroup(
                clusterID: clusterID,
                namespaces: byNamespace.keys.sorted().map { namespace in
                    NamespaceGroup(
                        clusterID: clusterID, namespace: namespace,
                        kinds: kindGroups(
                            clusterID: clusterID, namespace: namespace,
                            rows: byNamespace[namespace] ?? []))
                })
        }
    }

    private func kindGroups(clusterID: String, namespace: String, rows: [Workload])
        -> [KindGroup]
    {
        let byKind = Dictionary(grouping: rows, by: \.workloadKind)
        return byKind.keys.sorted { (kindRank($0), $0) < (kindRank($1), $1) }.map { kind in
            KindGroup(
                clusterID: clusterID, namespace: namespace, kind: kind,
                // A pod carries no creation time here, so its own liveness is
                // the only recency the row has to sort by.
                rows: (byKind[kind] ?? []).sorted { left, right in
                    guard kind == "none" else { return left.workloadName < right.workloadName }
                    let (leftLive, rightLive) = (left.livePods > 0, right.livePods > 0)
                    guard leftLive == rightLive else { return leftLive }
                    return (left.podName ?? "") < (right.podName ?? "")
                })
        }
    }

    private func clusterName(_ clusterID: String) -> String {
        clusters.first { $0.id == clusterID }?.name ?? clusterID
    }

    private func clusterDotColor(_ clusterID: String) -> Color {
        guard let cluster = clusters.first(where: { $0.id == clusterID }) else {
            return BadgeStyle.grey.text
        }
        return clusterDot(ready: cluster.ready, hasError: cluster.lastError != nil)
    }
}

/// PendingAcknowledge is the row a person asked to acknowledge, held until the
/// dialog answers.
private struct PendingAcknowledge {
    let key: WorkloadKey?
    let podUID: String?
    /// ids are the incidents a drag over the run strip already holds; a row of
    /// the tree holds counts instead and has its ids looked up.
    let ids: [String]?
    let name: String
    let count: Int
}

/// WorkloadsPaneHeader names what the tree's trailing number counts and carries
/// the two controls that move every group at once.
private struct WorkloadsPaneHeader: View {
    let collapseAll: () -> Void
    let expandAll: () -> Void

    var body: some View {
        // The tree pane is 300pt wide and the full "Collapse all" and
        // "Expand all" do not fit beside the count's name; the verb alone
        // does, and the help says the rest.
        HStack(spacing: 8) {
            Text("open incidents")
                .foregroundStyle(.secondary)
            Spacer(minLength: 6)
            Button("Collapse") { collapseAll() }
                .help("Collapse all: fold every group")
            Button("Expand") { expandAll() }
                .help("Expand all: open every group")
        }
        .font(.system(size: 11))
        .lineLimit(1)
        .buttonStyle(.link)
        .frame(height: 28)
    }
}

private struct ListKey: Hashable {
    let generation: Int
    let scope: ClusterScope
}

private struct DetailKey: Hashable {
    let generation: Int
    let workload: WorkloadKey?
    var podsLive = false
}

private struct RunsKey: Hashable {
    let generation: Int
    let workload: WorkloadKey?
    let live: Bool
    let failed: Bool
}
