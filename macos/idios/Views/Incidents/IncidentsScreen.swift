import AppKit
import IdiosModel
import SwiftUI

/// IncidentsScreen is the home screen: the cluster scope and the filters on the
/// left, the grouped incidents the filter keeps on the right.
struct IncidentsScreen: View {
    @Environment(Preferences.self) private var preferences
    @Environment(DaemonConnection.self) private var connection
    @Environment(DaemonManager.self) private var daemonManager
    @Environment(Navigator.self) private var navigator
    @Environment(StatusStore.self) private var status
    @Environment(PaletteState.self) private var palette
    @Environment(ExplainState.self) private var explain

    @State private var clusters = ClustersStore()
    @State private var search = SearchStore()
    @State private var searchText = ""
    @State private var launchQueryApplied = false
    @State private var incidents = IncidentsStore()
    // A delete or a note from the list is the same call the detail screen
    // makes, so it borrows that store rather than a second client.
    @State private var writes = IncidentWriteStore()
    @State private var filter: IncidentFilter
    @State private var path: [Route]
    // What Back popped, for Forward to put back; any other push discards it.
    @State private var forward: [Route] = []
    @State private var screen: RootScreen
    @State private var workloadRoute: Route
    // The workloads pane is the stack's root, which SwiftUI tears down while a
    // pod route sits on top of it; its state is held here, above the stack, so
    // Back returns to the tree a person left.
    @State private var workloads = WorkloadsStore()
    @State private var workloadsTree = WorkloadsTreeState()
    // What a person folded or opened, for as long as the window lives: it is
    // not a stored preference. It lives above the list because the filter and
    // the grouping rebuild the list without changing what is open.
    @State private var expansion = ListExpansion()
    @State private var facts = GroupFactsStore()
    // Which row the keys act on: an incident, or the group, run or rollup that
    // stands for several. It lives above the list because the filter and the
    // grouping rebuild the list without changing what is selected.
    @State private var selection: String?
    // Which group a menu bar row asked to be scrolled to; only a reveal
    // scrolls the list.
    @State private var revealed: String?
    @State private var showAddCluster = false
    @State private var showClusters = false
    @FocusState private var filterFocused: Bool

    /// init opens on the route the application was launched with.
    init(route: Route) {
        _filter = State(initialValue: IncidentFilter(state: .attention))
        _screen = State(initialValue: .incidents)
        _workloadRoute = State(initialValue: .workloads)
        switch route {
        case .incidents(let state):
            _filter = State(initialValue: IncidentFilter(state: state ?? .attention))
            _path = State(initialValue: [])
        case .incident(let id):
            _path = State(initialValue: [.incident(id)])
        case .workloads, .workload:
            _screen = State(initialValue: .workloads)
            _workloadRoute = State(initialValue: route)
            _path = State(initialValue: [])
        case .status:
            _screen = State(initialValue: .status)
            _path = State(initialValue: [])
        case .addCluster:
            _showAddCluster = State(initialValue: true)
            _path = State(initialValue: [])
        case .clusters:
            _showClusters = State(initialValue: true)
            _path = State(initialValue: [])
        case .menubar:
            _path = State(initialValue: [])
        default:
            _path = State(initialValue: [route])
        }
    }

    /// explainScreen is the table the window's overlay draws. A pushed page's
    /// regions depend on a selection that lives inside it, so the page in view
    /// publishes its screen rather than this view guessing it from the path.
    private var explainScreen: ExplainedScreen? {
        // The palette carries its own overlay, and a second one behind it
        // would be read through the palette's own dim.
        if palette.isPresented { return nil }
        guard path.isEmpty, screen == .incidents else { return explain.current }
        return .incidents(incidents.visibleRows.isEmpty ? .empty : .rows)
    }

    var body: some View {
        @Bindable var preferences = preferences
        NavigationSplitView {
            IncidentsSidebar(
                counts: incidents.counts,
                attentionWindowSeconds: status.status?.attentionWindowSeconds,
                showFilters: screen == .incidents && path.isEmpty,
                filter: sidebarFilter, screen: sidebarScreen, openStatus: { show(.status) })
                .navigationSplitViewColumnWidth(min: 220, ideal: 240, max: 320)
        } detail: {
            NavigationStack(path: $path) {
                content
                    .navigationDestination(for: Route.self, destination: destination)
            }
        }
        .explainable({ explainScreen })
        .overlay {
            if palette.isPresented {
                SearchPalette(
                    sections: searchSections, now: Date(), text: $searchText,
                    perform: perform, copy: copy,
                    dismiss: { palette.isPresented = false })
            }
        }
        .sheet(isPresented: $showAddCluster) {
            AddClusterSheet(store: clusters)
        }
        .sheet(isPresented: $showClusters) {
            ClustersSheet(store: clusters)
        }
        .sheet(isPresented: needsSetup) {
            // The first run has nothing to fall back to behind the sheet;
            // leaving is finishing it, or skipping its second step inside.
            SetupSheet(manager: daemonManager, clusters: clusters)
                .interactiveDismissDisabled()
        }
        .navigationTitle(screen == .incidents ? filter.title : screen.title)
        .toolbar {
            ToolbarItem(placement: .navigation) {
                clusterScopeMenu
            }
            // Grouping and the filter act on the list; a pushed detail is one
            // incident, so they leave with it.
            if path.isEmpty && screen == .incidents {
                // sharedBackgroundVisibility (macOS 26+) drops the glass pill
                // the toolbar draws behind a custom item by default; the
                // filter field already carries its own background and does
                // not want a second one.
                if #available(macOS 26.0, *) {
                    ToolbarItem { groupByMenu }
                    ToolbarItem { viewMenu }
                    ToolbarItem { filterField }.sharedBackgroundVisibility(.hidden)
                } else {
                    ToolbarItem { groupByMenu }
                    ToolbarItem { viewMenu }
                    ToolbarItem { filterField }
                }
            }
        }
        .onChange(of: navigator.serial) {
            if let route = navigator.route { show(route) }
        }
        .onChange(of: navigator.groupSerial) {
            // A menu bar row names a group the list computes, so the list has
            // to be showing the view and the grouping that produce it before
            // the id means anything; the scope widens for the same reason,
            // because the menu bar ignores it and the list does not.
            guard let target = navigator.group else { return }
            screen = .incidents
            path = []
            filter = IncidentFilter(state: .attention)
            preferences.grouping = .workload
            preferences.scope = preferences.scope.including(target.clusterID)
            expansion.collapsedGroups.remove(target.id)
            selection = target.id
            revealed = target.id
        }
        .onChange(of: navigator.backSerial) {
            guard !path.isEmpty else { return }
            forward.append(path.removeLast())
        }
        .onChange(of: navigator.forwardSerial) {
            guard let route = forward.popLast() else { return }
            path.append(route)
        }
        .task(id: connection.generation) {
            await clusters.watch(connection: connection, preferences: preferences)
        }
        .task(id: WatchKey(
            generation: connection.generation, filter: filter, scope: preferences.scope))
        {
            // The workload rows are what the headers count their live pods
            // against, and the scope that decides them is this task's own.
            await facts.loadWorkloads(connection: connection, scope: preferences.scope)
            await incidents.watch(
                connection: connection, filter: filter, scope: preferences.scope)
        }
        // A streamed row can be the first of a run the totals do not count yet.
        .task(id: RunsKey(keys: runKeys, rows: incidents.rows.count)) {
            await facts.loadRuns(keys: runKeys, connection: connection)
        }
        .task(id: palette.isPresented) {
            guard palette.isPresented else {
                searchText = ""
                search.clearLookup()
                return
            }
            if !launchQueryApplied {
                searchText = palette.launchQuery ?? ""
                launchQueryApplied = true
            }
            // The workload rows are the palette's Workloads section and the
            // "Show <kind> <name> in Workloads" commands; nothing has loaded
            // them until a person opens that screen.
            await workloads.load(connection: connection, scope: preferences.scope)
            await search.loadPods(connection: connection, scope: preferences.scope)
        }
        .task(id: searchText) {
            guard palette.isPresented else { return }
            let query = SearchQuery(parsing: searchText)
            guard !query.isEmpty else {
                search.clearLookup()
                return
            }
            // One lookup per pause in the typing; the task's own cancellation
            // drops the keystrokes before it.
            try? await Task.sleep(for: .milliseconds(250))
            guard !Task.isCancelled else { return }
            await search.lookup(query: query, connection: connection, scope: preferences.scope)
        }
    }

    private var searchSections: [SearchSectionHits] {
        searchResults(
            query: SearchQuery(parsing: searchText),
            pods: deduped(search.pods + search.lookupPods),
            incidents: deduped(
                incidents.rows + search.lookupIncidents + workloadIncidents),
            workloads: workloads.workloads,
            commands: commandList(views: IncidentsSidebar.views + IncidentState.closedStates),
            recent: search.recent)
    }

    // The workload the tree has open holds the only rows of its runs the
    // application ever loaded; the list's own rows stop at its limit.
    private var workloadIncidents: [Incident] {
        guard let key = workloadsTree.selected else { return [] }
        return workloads.incidents(of: key) ?? []
    }

    // searchResults is pure, so a row the list holds and the daemon returned
    // again is one hit only if it arrives once.
    private func deduped<Row: Identifiable>(_ rows: [Row]) -> [Row] {
        var seen = Set<Row.ID>()
        return rows.filter { seen.insert($0.id).inserted }
    }

    private func perform(_ action: SearchAction) {
        switch action {
        case .open(let route):
            switch route {
            case .incident(let id):
                openIncident(id)
            case .pod, .timeline, .run:
                guard path.last != route else { return }
                push(route)
            default:
                show(resolvedWorkload(route))
            }
        case .openWorkloadPods(let route):
            openWorkload(route, tab: .pods)
        case .openWorkloadRuns(let route):
            openWorkload(route, tab: .runs)
        case .back:
            navigator.goBack()
        }
    }

    private func copy(_ text: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }

    // The tree keys its nodes on the cluster's name; a row of the search and
    // of the list carries the cluster id.
    private func resolvedWorkload(_ route: Route) -> Route {
        guard case .workload(let cluster, let namespace, let kind, let name) = route else {
            return route
        }
        return .workload(
            cluster: clusters.cluster(id: cluster)?.name ?? cluster, namespace: namespace,
            kind: kind, name: name)
    }

    private func recentID(_ route: Route) -> String {
        switch route {
        case .incident(let id): "incident/\(id)"
        case .run(let uid): "run/\(uid)"
        case .workload(let cluster, let namespace, let kind, let name):
            "workload/\(cluster)/\(namespace)/\(kind)/\(name)"
        case .workloads: "workloads"
        case .status: "status"
        default: ""
        }
    }

    private var runKeys: Set<WorkloadKey> {
        Set(
            incidents.visibleRows.compactMap { row in
                guard row.workloadKind == "CronJob" || row.workloadKind == "Job",
                    !row.workloadName.isEmpty
                else { return nil }
                return WorkloadKey(
                    cluster: row.clusterID, namespace: row.namespace, kind: row.workloadKind,
                    name: row.workloadName)
            })
    }

    private var needsSetup: Binding<Bool> {
        Binding(
            get: { daemonManager.needsSetup },
            set: { daemonManager.needsSetup = $0 })
    }

    // The sidebar names a list, never a detail: setting a filter or choosing a
    // screen leaves whatever was pushed on top of it.
    private var sidebarFilter: Binding<IncidentFilter> {
        Binding(get: { filter }, set: { filter = $0; clearPath() })
    }

    private var sidebarScreen: Binding<RootScreen> {
        Binding(get: { screen }, set: { screen = $0; clearPath() })
    }

    private func show(_ route: Route) {
        switch route {
        case .incidents(let state):
            screen = .incidents
            filter = IncidentFilter(state: state ?? .attention)
            clearPath()
        case .workloads, .workload:
            screen = .workloads
            workloadRoute = route
            clearPath()
            if route == .workloads { visited(.workloads, title: "Workloads") }
        case .status:
            screen = .status
            clearPath()
            visited(.status, title: "Status")
        case .addCluster:
            showAddCluster = true
        case .clusters:
            showClusters = true
        case .menubar:
            break
        default:
            screen = .incidents
            clearPath()
            path = [route]
        }
    }

    @ViewBuilder private var content: some View {
        switch screen {
        case .incidents: incidentsContent
        case .workloads: workloadsPane
        case .status: statusPane
        }
    }

    private var workloadsPane: some View {
        WorkloadsScreen(
            clusters: clusters.clusters, store: workloads, tree: $workloadsTree,
            route: $workloadRoute, openPod: { push(.pod($0, .containers)) },
            openIncident: { push(.incident($0)) }, openRun: { push(.run($0)) })
    }

    // The rail's "+ N more" line opens Workloads at that pod's controller's
    // Pods tab; the pod page's own path is dropped, the same as any other
    // route switching screens.
    private func openWorkloadPods(_ pod: PodRow) {
        openWorkload(
            .workload(
                cluster: pod.clusterID, namespace: pod.namespace, kind: pod.workloadKind,
                name: pod.workloadName),
            tab: .pods)
    }

    // A group's "+ N more runs" line opens Workloads at that workload's Runs
    // tab; the incident list's path is dropped, the same as any other route
    // switching screens.
    private func openWorkloadRuns(_ row: Incident) {
        openWorkload(
            .workload(
                cluster: row.clusterID, namespace: row.namespace, kind: row.workloadKind,
                name: row.workloadName),
            tab: .runs)
    }

    // A new direction discards the way back to where Back came from, which
    // is what a forward stack means.
    private func push(_ route: Route) {
        forward = []
        path.append(route)
    }

    private func clearPath() {
        path = []
        forward = []
    }

    // The identity line's cluster and namespace segments open Workloads with
    // that node showing; the tree keys its groups on the cluster id.
    private func revealNamespace(cluster: String, namespace: String) {
        screen = .workloads
        workloadsTree.collapsed.remove("cluster/\(cluster)")
        workloadsTree.collapsed.remove("\(cluster)/\(namespace)")
        clearPath()
    }

    private func openWorkload(_ route: Route, tab: WorkloadTab) {
        screen = .workloads
        workloadsTree.selected = nil
        workloadsTree.tab = tab
        workloadRoute = resolvedWorkload(route)
        clearPath()
        guard case .workload(_, _, let kind, let name) = route, !name.isEmpty else { return }
        search.visited(
            SearchCommand(
                id: recentID(workloadRoute), title: "\(kind) \(name)", key: nil,
                action: tab == .runs ? .openWorkloadRuns(route) : .openWorkloadPods(route)))
    }

    private var statusPane: some View {
        StatusScreen(clusters: clusters.clusters)
    }

    @ViewBuilder private var incidentsContent: some View {
        listOrError
    }

    @ViewBuilder private var listOrError: some View {
        if case .unreachable(let description) = connection.state {
            NotConnectedView(
                address: connection.address, error: description, retry: connection.retry)
        } else {
            let groups = incidentGroups(
                rows: incidents.visibleRows, grouping: preferences.grouping,
                cluster: clusters.cluster(id:), selectedClusters: selectedClusters,
                facts: facts.facts(for:))
            IncidentsList(
                clusterGroups: groups,
                summary: listSummary(groups: groups.flatMap { $0.groups.map(\.rows) }),
                cluster: clusters.cluster(id:), selectedClusters: selectedClusters,
                isLoading: incidents.isLoading, hasRows: !incidents.rows.isEmpty,
                scopeIsEmpty: preferences.scope.isEmpty, filterText: incidents.filter,
                filter: filter, density: preferences.density, expansion: $expansion,
                selection: $selection, reveal: revealed, open: openIncident,
                openRun: { push(.run($0)) },
                acknowledge: { id in
                    Task { await incidents.acknowledge(id: id, connection: connection) }
                },
                dismiss: { id in
                    Task { await incidents.dismiss(id: id, connection: connection) }
                },
                resolve: { id in
                    Task { await incidents.resolve(id: id, connection: connection) }
                },
                acknowledgeAll: acknowledgeAll,
                // The write answers with each changed row and the stream
                // repeats it, so the note reaches the list on its own.
                note: { ids, text in
                    Task {
                        for id in ids {
                            _ = await writes.setNote(text, id: id, connection: connection)
                        }
                    }
                },
                clearFilter: { incidents.filter = "" },
                focusFilter: { filterFocused = true },
                showAttention: { filter = IncidentFilter(state: .attention); clearPath() },
                openRuns: openWorkloadRuns,
                delete: { id in
                    Task {
                        if await writes.delete(id: id, connection: connection) {
                            await incidents.remove(id: id, connection: connection)
                        }
                    }
                })
        }
    }

    // The whole view, not the rows on screen: the ones a group folds away are
    // acknowledged too.
    private func acknowledgeAll() {
        let rows = incidents.visibleRows.filter {
            $0.closedAt == nil && $0.acknowledgedAt == nil
        }
        Task {
            for row in rows {
                await incidents.acknowledge(id: row.id, connection: connection)
            }
        }
    }

    // A double click and a Return can both reach the opener for one row, so the
    // route is pushed only when it is not already on top. A job-subject
    // incident is about the run, not about the pod the daemon borrowed to
    // describe it.
    private func openIncident(_ id: String) {
        let rows = incidents.rows + search.lookupIncidents
        let route = rows.first { $0.id == id }.map(incidentRoute) ?? .incident(id)
        visited(route, title: incidentTitle(id))
        guard path.last != route else { return }
        push(route)
    }

    private func visited(_ route: Route, title: String) {
        search.visited(
            SearchCommand(
                id: recentID(route), title: title, key: nil, action: .open(route)))
    }

    // A hit the daemon's lookup found is not in the list's rows, and an id
    // that names nothing kept is still a page a person opened.
    private func incidentTitle(_ id: String) -> String {
        let rows = incidents.rows + search.lookupIncidents
        guard let row = rows.first(where: { $0.id == id }) else { return "#\(id)" }
        return "#\(id) \(row.category.label) - \(workloadTitle(row))"
    }

    @ViewBuilder private func destination(_ route: Route) -> some View {
        switch route {
        case .incident(let id):
            PodPageScreen(
                opening: .incident(id: id, tab: .overview), clusters: clusters.clusters,
                openPod: { push(.pod($0, .containers)) }, openIncident: openIncident,
                openRun: { push(.run($0)) }, openWorkloadPods: openWorkloadPods,
                openWorkload: openWorkload,
                revealNamespace: revealNamespace, deleted: deleteIncident)
        case .timeline(let id):
            PodPageScreen(
                opening: .incident(id: id, tab: .timeline), clusters: clusters.clusters,
                openPod: { push(.pod($0, .containers)) }, openIncident: openIncident,
                openRun: { push(.run($0)) }, openWorkloadPods: openWorkloadPods,
                openWorkload: openWorkload,
                revealNamespace: revealNamespace, deleted: deleteIncident)
        case .run(let uid):
            RunPageScreen(
                jobUID: uid, clusters: clusters.clusters,
                openPod: { push(.pod($0, $1)) }, openWorkload: openWorkload,
                revealNamespace: revealNamespace, deleted: deleteIncident)
        case .pod(let uid, let tab):
            PodPageScreen(
                opening: .pod(uid: uid, pane: tab.pane), clusters: clusters.clusters,
                openPod: { push(.pod($0, .containers)) }, openIncident: openIncident,
                openRun: { push(.run($0)) }, openWorkloadPods: openWorkloadPods,
                openWorkload: openWorkload,
                revealNamespace: revealNamespace, deleted: deleteIncident)
        case .workloads, .workload:
            workloadsPane
        case .status:
            statusPane
        // None of these routes are ever pushed onto the stack: init and
        // show() open a sheet instead of the path for addCluster and
        // clusters, and menubar never reaches the main window.
        case .incidents, .menubar, .addCluster, .clusters:
            incidentsContent
        }
    }

    // Leaving the detail route cancels its watch task before the next reload
    // would hit the 404 the delete just caused.
    private func deleteIncident(id: String) {
        clearPath()
        Task { await incidents.remove(id: id, connection: connection) }
    }

    // A menu button reads as one control at the toolbar's other sizes, unlike
    // a label glued beside a native Picker (the .menu style swallows the
    // Picker's own label).
    private var groupByMenu: some View {
        Menu {
            Picker("Group by", selection: grouping) {
                ForEach(Grouping.allCases, id: \.self) { mode in
                    Text(mode.title).tag(mode)
                }
            }
            .pickerStyle(.inline)
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "square.stack.3d.up")
                Text(preferences.grouping.title)
            }
            .font(.system(size: 11))
        }
        .menuStyle(.button)
        .buttonStyle(.bordered)
        .fixedSize()
        .help("Group by")
    }

    private var viewMenu: some View {
        Menu {
            Picker("Density", selection: density) {
                ForEach(Density.allCases, id: \.self) { mode in
                    Text(mode.title).tag(mode)
                }
            }
            .pickerStyle(.inline)
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "list.bullet")
                Text("View")
            }
            .font(.system(size: 11))
        }
        .menuStyle(.button)
        .buttonStyle(.bordered)
        .fixedSize()
        .help("View")
    }

    private var filterField: some View {
        HStack(spacing: 5) {
            Image(systemName: "line.3.horizontal.decrease")
                .foregroundStyle(.secondary)
            TextField("Filter", text: rowFilter)
                .textFieldStyle(.plain)
                .frame(width: 150)
                .focused($filterFocused)
        }
        .font(.system(size: 12))
        .padding(.horizontal, 9)
        .frame(width: 200, height: 24)
        .background(RoundedRectangle(cornerRadius: 6).fill(.quaternary.opacity(0.5)))
    }

    private var rowFilter: Binding<String> {
        Binding(get: { incidents.filter }, set: { incidents.filter = $0 })
    }

    private var grouping: Binding<Grouping> {
        Binding(get: { preferences.grouping }, set: { preferences.grouping = $0 })
    }

    private var density: Binding<Density> {
        Binding(get: { preferences.density }, set: { preferences.density = $0 })
    }

    private var selectedClusters: Int {
        preferences.scope.isAll ? clusters.clusters.count : preferences.scope.selected.count
    }

    // The scope moved here from the sidebar, so it is the one place a cluster
    // is switched in or out; management stays a separate sheet, one menu item
    // away, rather than folded into the checkable rows.
    private var clusterScopeMenu: some View {
        Menu {
            ForEach(clusters.clusters) { cluster in
                Toggle(cluster.name + (cluster.lastError != nil ? " (unreachable)" : ""),
                    isOn: scopeToggle(cluster))
            }
            if !clusters.clusters.isEmpty {
                Divider()
            }
            Button("Manage clusters...") { showClusters = true }
            Button("Add cluster...") { showAddCluster = true }
        } label: {
            HStack(spacing: 5) {
                if let one = singleSelectedCluster {
                    Circle()
                        .fill(clusterDot(ready: one.ready, hasError: one.lastError != nil))
                        .frame(width: 7, height: 7)
                    Text(one.name)
                } else {
                    Text(clusterScopeLabel)
                }
            }
            .font(.system(size: 11.5))
        }
        .menuStyle(.button)
        .buttonStyle(.bordered)
        .fixedSize()
        .help(scopeTooltip)
    }

    // Every cluster checked is the all scope, so a cluster added later joins
    // it; unchecking the last one puts no cluster in scope, which the screens
    // say out loud rather than answering with every cluster.
    private func scopeToggle(_ cluster: Cluster) -> Binding<Bool> {
        Binding(
            get: { preferences.scope.includes(cluster.id) },
            set: { _ in
                var selected =
                    preferences.scope.isAll
                    ? Set(clusters.clusters.map(\.id)) : preferences.scope.selected
                if selected.contains(cluster.id) {
                    selected.remove(cluster.id)
                } else {
                    selected.insert(cluster.id)
                }
                preferences.scope =
                    selected.count == clusters.clusters.count
                    ? .all : ClusterScope(selected: selected)
            })
    }

    private var singleSelectedCluster: Cluster? {
        guard selectedClusters == 1 else { return nil }
        return clusters.clusters.first { preferences.scope.includes($0.id) }
    }

    private var clusterScopeLabel: String {
        if clusters.clusters.isEmpty { return "Add cluster..." }
        if selectedClusters == clusters.clusters.count { return "All clusters" }
        return "\(selectedClusters) clusters"
    }

    private var scopeTooltip: String {
        let selected = clusters.clusters.filter { preferences.scope.includes($0.id) }
        guard !selected.isEmpty else { return "no cluster selected" }
        return selected.map(\.name).joined(separator: ", ") + " selected"
    }
}

private struct WatchKey: Hashable {
    let generation: Int
    let filter: IncidentFilter
    let scope: ClusterScope
}

private struct RunsKey: Hashable {
    let keys: Set<WorkloadKey>
    let rows: Int
}
