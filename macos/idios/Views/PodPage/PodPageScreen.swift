import AppKit
import IdiosModel
import SwiftUI

/// PodPageOpening is what a route asked the page to show first.
enum PodPageOpening: Hashable {
    case incident(id: String, tab: ContainerTab)
    case pod(uid: String, pane: PodPane)
}

/// PodPageScreen is one pod: its containers on the left, the selection in
/// the middle, the rail on the right.
struct PodPageScreen: View {
    let opening: PodPageOpening
    let clusters: [Cluster]
    let openPod: (String) -> Void
    let openIncident: (String) -> Void
    let openRun: (String) -> Void
    let openWorkloadPods: (PodRow) -> Void
    let openWorkload: (Route, WorkloadTab) -> Void
    let revealNamespace: (String, String) -> Void
    let deleted: (String) -> Void

    @Environment(DaemonConnection.self) private var connection
    @Environment(Navigator.self) private var navigator
    @Environment(\.controlActiveState) private var activeState
    @Environment(StatusStore.self) private var status
    @Environment(ExplainState.self) private var explain

    @State private var store = PodPageStore()
    @State private var podUID: String?
    @State private var selection: PodSelection
    /// podLit is the incident the Pod card's own control chose; nil defers
    /// to the fold order's lead.
    @State private var podLit: String?
    @State private var selectedArtifactID: String?
    @State private var wrap = false
    @State private var showingNoteSheet = false
    @State private var showingDeleteAlert = false
    @State private var askAI: AskAIRequest?

    /// init sets the pod and selection for a pod opening; an incident opening
    /// leaves the pod nil until resolve names it.
    init(
        opening: PodPageOpening, clusters: [Cluster], openPod: @escaping (String) -> Void,
        openIncident: @escaping (String) -> Void, openRun: @escaping (String) -> Void,
        openWorkloadPods: @escaping (PodRow) -> Void,
        openWorkload: @escaping (Route, WorkloadTab) -> Void,
        revealNamespace: @escaping (String, String) -> Void,
        deleted: @escaping (String) -> Void
    ) {
        self.opening = opening
        self.clusters = clusters
        self.openPod = openPod
        self.openIncident = openIncident
        self.openRun = openRun
        self.openWorkloadPods = openWorkloadPods
        self.openWorkload = openWorkload
        self.revealNamespace = revealNamespace
        self.deleted = deleted
        switch opening {
        case .pod(let uid, let pane):
            _podUID = State(initialValue: uid)
            _selection = State(initialValue: .pod(pane))
        case .incident:
            _podUID = State(initialValue: nil)
            _selection = State(initialValue: .pod(.events))
        }
    }

    var body: some View {
        content
            // The three letters act on the lit incident; the screen itself
            // must be focusable for onKeyPress to see them.
            .focusable()
            .focusEffectDisabled()
            .onKeyPress(keys: ["a", "d", "r", "?"]) { press in
                // "?" arrives with shift held, so it is answered before the
                // no-modifier guard the three verbs need.
                if press.key == "?" {
                    explain.toggle(explainScreen)
                    return .handled
                }
                guard press.modifiers.isEmpty, let litID else { return .ignored }
                switch press.key {
                case "a": act(litID, store.writes.acknowledge)
                case "d": act(litID, store.writes.dismiss)
                default: act(litID, store.writes.resolve)
                }
                return .handled
            }
            // The overlay is installed on the window's split view, so the
            // sidebar is under its scrim too; the page publishes which of its
            // tables that overlay draws.
            .onAppear { explain.current = explainScreen }
            .onChange(of: selection) {
                explain.current = explainScreen
                if explain.screen != nil { explain.open(explainScreen) }
            }
            .onDisappear {
                if explain.screen == explainScreen { explain.close() }
                explain.current = nil
            }
            .navigationTitle(title)
            // The stack's own chevron pops the path behind the screen's back,
            // which would leave Forward with nothing to return to.
            .navigationBarBackButtonHidden(true)
            .toolbar {
                ToolbarItemGroup(placement: .navigation) { backForward }
                ToolbarItemGroup { copyUID }
            }
            .sheet(item: $askAI) { request in
                AskAISheet(id: litID ?? "", request: request)
            }
            .sheet(isPresented: $showingNoteSheet) {
                NoteSheet(note: litDetail?.incident.note) { text in
                    guard let litID else { return }
                    Task {
                        if let updated = await store.writes.setNote(
                            text, id: litID, connection: connection)
                        {
                            await apply(updated)
                        }
                    }
                }
            }
            .alert("Delete incident \(litID ?? "")?", isPresented: $showingDeleteAlert) {
                Button("Delete", role: .destructive) {
                    guard let litID else { return }
                    Task {
                        if await store.writes.delete(id: litID, connection: connection) {
                            deleted(litID)
                        }
                    }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(
                    "The row and its captured files are removed now. History and events stay "
                        + "until their own sweep. This cannot be undone.")
            }
            .task(id: opening) {
                guard case .incident(let id, let tab) = opening else { return }
                guard let resolved = await store.resolve(incidentID: id, connection: connection)
                else { return }
                switch resolved {
                case .run(let jobUID):
                    openRun(jobUID)
                case .pod(let uid, let container):
                    podUID = uid
                    guard let container else {
                        // A pod-level incident has no container to select;
                        // without this, the fold lead of the pod-level rows
                        // would light instead of the incident the route named.
                        podLit = id
                        selection = .pod(.events)
                        return
                    }
                    selection = .container(name: container, incidentID: id, tab: tab)
                }
            }
            .task(id: PodKey(generation: connection.generation, podUID: podUID)) {
                guard let podUID else { return }
                await store.watch(uid: podUID, connection: connection)
            }
            .task(id: LitKey(generation: connection.generation, litID: litID)) {
                guard let litID, store.lit?.incident.id != litID else { return }
                await store.light(incidentID: litID, connection: connection)
            }
            .task(
                id: TimelineKey(
                    generation: connection.generation, litID: litID, showing: showingTimeline)
            ) {
                guard showingTimeline, let litID else { return }
                await store.loadTimeline(incidentID: litID, connection: connection)
            }
            // The header's tag needs the readiness history whichever pane is
            // showing, so the page asks for it on arrival rather than when
            // the Conditions tab opens.
            .task(id: HistoryKey(generation: connection.generation, podUID: podUID)) {
                guard let podUID else { return }
                await store.loadHistory(uid: podUID, connection: connection)
            }
            .task(
                id: RelatedKey(
                    generation: connection.generation, jobUID: jobUID, showing: showingRelated)
            ) {
                guard showingRelated, store.lit == nil, let jobUID else { return }
                await store.loadRelated(jobUID: jobUID, connection: connection)
            }
            .task(id: paneArtifact?.id) {
                guard let artifact = paneArtifact else { return }
                await store.content(of: artifact, connection: connection)
            }
            .onChange(of: activeState) { _, state in
                guard state == .key else { return }
                Task {
                    if let podUID { await store.reload(uid: podUID, connection: connection) }
                    if let litID { await store.light(incidentID: litID, connection: connection) }
                    if showingTimeline, let litID {
                        await store.loadTimeline(incidentID: litID, connection: connection)
                    }
                }
            }
    }

    @ViewBuilder private var content: some View {
        if case .unreachable(let description) = connection.state {
            NotConnectedView(
                address: connection.address, error: description, retry: connection.retry)
        } else if let detail = store.detail {
            loaded(detail)
        } else if store.missing {
            VStack(spacing: 8) {
                Text("pod \(podUID ?? "")").font(.title3.weight(.semibold))
                Text("pod \(podUID ?? "") is no longer stored; the sweep removed it.")
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if let error = store.error {
            VStack(spacing: 8) {
                Text("pod \(podUID ?? "")").font(.title3.weight(.semibold))
                WrapText(value: error.message).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else if podUID == nil, let litError = store.litError {
            // An incident route whose GetIncident failed never names a pod,
            // so nothing else here will ever load; without this the screen
            // spins forever instead of saying what resolve found.
            VStack(spacing: 8) {
                Text("pod \(podUID ?? "")").font(.title3.weight(.semibold))
                WrapText(value: litError.message).foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func loaded(_ detail: PodDetail) -> some View {
        VStack(spacing: 0) {
            PodPageHeader(
                detail: detail, clusterName: clusterName(detail),
                flips: readinessFlips(store.history?.conditions ?? []),
                openWorkload: openWorkload, openRun: openRun,
                revealNamespace: revealNamespace)
            if detail.pod.deletedAt != nil {
                DeletedBanner(
                    deletedAt: detail.pod.deletedAt, source: detail.pod.deletionSource,
                    reason: detail.pod.deletionReason, retentionDays: status.retentionDays)
                    .padding(.horizontal, 20)
                    .padding(.bottom, 12)
            }
            Divider()
            HStack(alignment: .top, spacing: 0) {
                PodColumn(
                    detail: detail, eventCount: store.events?.count, selection: $selection,
                    podLevel: podLevelIncidents)
                    .frame(width: 240)
                    .background(.quaternary.opacity(0.25))
                Divider()
                ScrollView { middle(detail) }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                Divider()
                ScrollView {
                    PodPageRail(
                        detail: detail, container: selectedContainer(detail),
                        lit: litDetail, now: Date(),
                        openPod: openPod, openRun: openRun,
                        openWorkloadPods: openWorkloadPods)
                        .padding(.horizontal, 15)
                        .padding(.vertical, 16)
                }
                .frame(width: 310)
                .background(.quaternary.opacity(0.25))
            }
        }
    }

    /// explainScreen is which table the overlay draws: the pod page has one
    /// set of regions per selection, because a container's pane and the pod
    /// card's share the header, the column and the rail but not the middle.
    private var explainScreen: ExplainedScreen {
        switch selection {
        case .container(_, _, let tab): .container(tab)
        case .pod(let pane): .podCard(pane)
        }
    }

    @ViewBuilder private func middle(_ detail: PodDetail) -> some View {
        switch selection {
        case .container(let name, _, _):
            if let container = detail.containers.first(where: { $0.name == name }) {
                ContainerPane(
                    detail: detail, container: container,
                    incidents: containerIncidents(name), lit: litDetail, litID: litID,
                    podUID: podUID ?? "", store: store, selection: $selection,
                    selectedArtifactID: $selectedArtifactID, wrap: $wrap,
                    openNote: { showingNoteSheet = true },
                    openDelete: { showingDeleteAlert = true },
                    askAI: { askAI = $0 })
            }
        case .pod:
            PodCardPane(
                detail: detail, clusterName: clusterName(detail), store: store,
                podLevel: podLevelIncidents, lit: litDetail, podUID: podUID ?? "",
                podLit: $podLit, selectedArtifactID: $selectedArtifactID, wrap: $wrap,
                selection: $selection, openIncident: openIncident,
                openNote: { showingNoteSheet = true },
                openDelete: { showingDeleteAlert = true },
                askAI: { askAI = $0 })
        }
    }

    // The stack's own chevron is the window's, not the page's; these two are
    // the same walk the Go menu names, so the keys and the buttons agree.
    private var backForward: some View {
        HStack(spacing: 2) {
            Button { navigator.goBack() } label: { Image(systemName: "chevron.left") }
                .help("Back (Cmd-[)")
            Button { navigator.goForward() } label: { Image(systemName: "chevron.right") }
                .help("Forward (Cmd-])")
        }
    }

    private var copyUID: some View {
        Button("Copy uid") {
            guard let subjectUID else { return }
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(subjectUID, forType: .string)
        }
        .help(subjectUID ?? "")
    }

    private var subjectUID: String? { podUID }

    private var title: String { store.detail?.pod.name ?? "pod" }

    // A pod-level incident has no container of its own; the card lights the
    // first row of the fold order unless the control has already chosen one.
    private var podLevelIncidents: [Incident] {
        (store.detail?.incidents ?? []).filter { $0.containerName == nil }
    }

    private func containerIncidents(_ name: String) -> [Incident] {
        guard let detail = store.detail else { return [] }
        return segmentOrder(detail.incidents.filter { $0.containerName == name })
    }

    private var litID: String? {
        switch selection {
        case .container(_, let incidentID, _): return incidentID
        case .pod: return podLit ?? segmentOrder(podLevelIncidents).first?.id
        }
    }

    // A pane or the rail must not show the previously lit incident's facts
    // for a container or the Pod card that has none of its own; nil here is
    // what tells them so, while litID itself keeps store.lit until the next
    // segment's answer arrives.
    private var litDetail: IncidentDetail? { litID == nil ? nil : store.lit }

    private var showingTimeline: Bool {
        if case .container(_, _, let tab) = selection { return tab == .timeline }
        return false
    }

    private var showingRelated: Bool {
        if case .pod(let pane) = selection { return pane == .related }
        return false
    }

    // Only a Job's pod carries a job_uid worth asking about; every other pod
    // has no Related tab to feed.
    private var jobUID: String? {
        guard let pod = store.detail?.pod, pod.controllerKind == "Job" else { return nil }
        return pod.controllerUID
    }

    private var paneArtifact: Artifact? {
        guard let detail = store.detail else { return nil }
        switch selection {
        case .container(let name, _, let tab):
            guard tab == .overview || tab == .logs else { return nil }
            let chips = containerLogArtifacts(name, detail: detail)
            return chips.first { $0.id == selectedArtifactID } ?? defaultArtifact(chips)
        case .pod(let pane):
            guard pane == .podJSON else { return nil }
            return detail.artifacts.first { $0.kind == .podJSON }
        }
    }

    private func selectedContainer(_ detail: PodDetail) -> Container? {
        guard case .container(let name, _, _) = selection else { return nil }
        return detail.containers.first { $0.name == name }
    }

    private func clusterName(_ detail: PodDetail) -> String {
        clusters.first { $0.id == detail.pod.clusterID }?.name ?? detail.pod.clusterID
    }

    private func act(_ id: String, _ call: @escaping (String, DaemonConnection) async -> Incident?)
    {
        Task {
            if let updated = await call(id, connection) {
                await apply(updated)
            }
        }
    }

    private func apply(_ incident: Incident) async {
        await store.apply(incident, uid: podUID, connection: connection)
    }
}

/// containerLogArtifacts is the container's own log artifacts, oldest dead
/// instance first; pod.json belongs to the Pod card and never appears here.
func containerLogArtifacts(_ name: String, detail: PodDetail) -> [Artifact] {
    detail.artifacts.filter { $0.containerName == name && $0.kind != .podJSON }
        .sorted(by: artifactOrder)
}

private struct PodKey: Hashable {
    let generation: Int
    let podUID: String?
}

private struct LitKey: Hashable {
    let generation: Int
    let litID: String?
}

private struct TimelineKey: Hashable {
    let generation: Int
    let litID: String?
    let showing: Bool
}

private struct HistoryKey: Hashable {
    let generation: Int
    let podUID: String?
}

private struct RelatedKey: Hashable {
    let generation: Int
    let jobUID: String?
    let showing: Bool
}
