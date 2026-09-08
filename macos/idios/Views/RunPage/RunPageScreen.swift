import AppKit
import IdiosModel
import SwiftUI

/// RunPane is the tab a Run page shows.
enum RunPane: Hashable {
    case overview, timeline, logs, events

    /// part is the tab as the screen's explanation names it: the header and
    /// the rail are the same on every tab and the pane is not.
    var part: RunPart {
        switch self {
        case .overview: .overview
        case .timeline: .timeline
        case .logs: .logs
        case .events: .events
        }
    }
}

/// RunPageScreen is one Job: what it did, one line per attempt, and the
/// actions that reach every incident it opened.
struct RunPageScreen: View {
    let jobUID: String
    let clusters: [Cluster]
    let openPod: (String, PodTab) -> Void
    let openWorkload: (Route, WorkloadTab) -> Void
    let revealNamespace: (String, String) -> Void
    let deleted: (String) -> Void

    @Environment(DaemonConnection.self) private var connection
    @Environment(Navigator.self) private var navigator
    @Environment(ExplainState.self) private var explain
    @Environment(\.controlActiveState) private var activeState

    @State private var store = RunPageStore()
    @State private var pane: RunPane = .overview
    @State private var selectedArtifactID: String?
    @State private var wrap = false
    @State private var showingNoteSheet = false
    @State private var showingDeleteAlert = false
    @State private var askAI: AskAIRequest?

    var body: some View {
        content
            // The three letters act on the lead incident; the screen itself
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
                guard press.modifiers.isEmpty, let lead = store.lead else { return .ignored }
                switch press.key {
                // Acknowledge on a run acts on every incident the run opened,
                // which is what the button beside it says it does.
                case "a": Task { _ = await store.acknowledgeRun(connection: connection) }
                case "d": act(lead.id, store.writes.dismiss)
                default: act(lead.id, store.writes.resolve)
                }
                return .handled
            }
            // The overlay is installed on the window's split view, so the
            // sidebar is under its scrim too; the page publishes which of its
            // tables that overlay draws.
            .onAppear { explain.current = explainScreen }
            .onChange(of: pane) {
                explain.current = explainScreen
                if explain.screen != nil { explain.open(explainScreen) }
            }
            .onDisappear {
                if explain.screen == explainScreen { explain.close() }
                explain.current = nil
            }
            .navigationTitle(
                runTitle(
                    jobName: store.job?.name, cronjobName: store.job?.cronjobName,
                    jobUID: jobUID))
            // The stack's own chevron pops the path behind the screen's back,
            // which would leave Forward with nothing to return to.
            .navigationBarBackButtonHidden(true)
            .toolbar {
                ToolbarItemGroup(placement: .navigation) { backForward }
                ToolbarItemGroup { copyUID }
            }
            .sheet(item: $askAI) { request in
                AskAISheet(id: store.lead?.id ?? "", request: request)
            }
            .sheet(isPresented: $showingNoteSheet) {
                NoteSheet(note: store.lead?.note) { text in
                    guard let id = store.lead?.id else { return }
                    Task {
                        if let updated = await store.writes.setNote(
                            text, id: id, connection: connection)
                        {
                            store.apply(updated)
                        }
                    }
                }
            }
            .alert(
                "Delete incident \(store.lead?.id ?? "")?", isPresented: $showingDeleteAlert
            ) {
                Button("Delete", role: .destructive) {
                    guard let id = store.lead?.id else { return }
                    Task {
                        if await store.writes.delete(id: id, connection: connection) {
                            deleted(id)
                        }
                    }
                }
                Button("Cancel", role: .cancel) {}
            } message: {
                Text(
                    "The row and its captured files are removed now. History and events stay "
                        + "until their own sweep. This cannot be undone.")
            }
            .task(id: RunKey(generation: connection.generation, jobUID: jobUID)) {
                await store.watch(jobUID: jobUID, connection: connection)
            }
            .task(
                id: TimelineKey(
                    generation: connection.generation, leadID: store.lead?.id,
                    showing: pane == .timeline)
            ) {
                guard pane == .timeline, let id = store.lead?.id else { return }
                await store.loadTimeline(incidentID: id, connection: connection)
            }
            .task(id: selectedArtifact?.id) {
                guard let artifact = selectedArtifact else { return }
                await store.content(of: artifact, connection: connection)
            }
            .onChange(of: activeState) { _, state in
                guard state == .key else { return }
                Task {
                    await store.reload(jobUID: jobUID, connection: connection)
                    if pane == .timeline, let id = store.lead?.id {
                        await store.loadTimeline(incidentID: id, connection: connection)
                    }
                }
            }
    }

    @ViewBuilder private var content: some View {
        if case .unreachable(let description) = connection.state {
            NotConnectedView(
                address: connection.address, error: description, retry: connection.retry)
        } else if store.job != nil || !store.rows.isEmpty {
            loaded
        } else if let error = store.error {
            message(error.message)
        } else if store.jobMissing {
            message("run \(jobUID) is no longer stored; the sweep removed it.")
        } else {
            ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }

    private func message(_ text: String) -> some View {
        VStack(spacing: 8) {
            Text("run \(jobUID)").font(.title3.weight(.semibold))
            WrapText(value: text).foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    // The attempts are a card and not a source list, so the page has no left
    // column: a header, the middle, and the rail the pod page has.
    private var loaded: some View {
        VStack(spacing: 0) {
            RunPageHeader(
                job: store.job, jobUID: jobUID, attempts: attempts, rows: store.rows,
                clusterName: clusterName, namespace: namespace,
                cronjobName: store.job?.cronjobName, openWorkload: openWorkload,
                revealNamespace: revealNamespace)
            Divider()
            HStack(alignment: .top, spacing: 0) {
                ScrollView { middle }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                Divider()
                ScrollView {
                    RunPageRail(
                        job: store.job, jobUID: jobUID, attempts: attempts, rows: store.rows,
                        siblingRuns: store.siblingRuns, lead: store.lead, now: Date(),
                        openPod: openPod, openRun: { navigator.open(.run($0)) },
                        openWorkload: openWorkload)
                        .padding(.horizontal, 15)
                        .padding(.vertical, 16)
                }
                .frame(width: 310)
                .background(.quaternary.opacity(0.25))
            }
        }
    }

    private var middle: some View {
        VStack(alignment: .leading, spacing: 12) {
            if let lead = store.lead {
                HStack {
                    Spacer()
                    PaneActionsRow(
                        incident: lead, writes: store.writes,
                        apply: { row, _ in store.apply(row) },
                        bulk: (
                            count: store.unacknowledged.count,
                            acknowledge: { connection in
                                _ = await store.acknowledgeRun(connection: connection)
                            }
                        ),
                        openNote: { showingNoteSheet = true },
                        openDelete: { showingDeleteAlert = true },
                        askAI: { askAI = $0 })
                }
            }
            // IncidentHeader pads 20 where the pane pads 16, so the verdict
            // shifts 4 to stand over the tags it answers with.
            verdict
                .padding(.horizontal, 4)
                .explained("verdict")
            if let detail = leadDetail {
                IncidentHeader(detail: detail, now: Date())
            }
            if let note = store.lead?.note {
                WrapText(value: note)
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            }
            if let actionError = store.writes.actionError {
                WrapText(value: actionError.message)
                    .font(.system(size: 11))
                    .foregroundStyle(BadgeStyle.red.text)
            }
            tabStrip
            Divider()
            tabContent
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // VerdictBlock reads one incident and one container, which is not what a
    // run is; the sentences are drawn the same way so the two pages read
    // alike.
    private var verdict: some View {
        VStack(alignment: .leading, spacing: 3) {
            ForEach(Array(sentences.enumerated()), id: \.offset) { index, sentence in
                Text(sentence)
                    .font(.system(size: 12, weight: index == 0 ? .semibold : .regular))
                    .foregroundStyle(
                        index == 0 ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary))
                    .lineLimit(nil)
                    .textSelection(.enabled)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var sentences: [String] {
        runVerdictSentences(
            job: store.job, attempts: attempts, capture: store.capture, imageTag: imageTag,
            sameReasonRuns: sameReasonRuns, now: Date())
    }

    private var tabStrip: some View {
        HStack(spacing: 2) {
            tabButton(title: "Overview", item: .overview)
            tabButton(title: "Timeline", item: .timeline)
            tabButton(title: "Logs \(plural(store.logArtifacts.count, "file"))", item: .logs)
            tabButton(title: "Events \(store.events.count)", item: .events)
            Spacer()
        }
        .padding(.horizontal, 14)
        .frame(height: 38)
    }

    private func tabButton(title: String, item: RunPane) -> some View {
        Button { pane = item } label: {
            Text(title)
                .font(.system(size: 12.5, weight: pane == item ? .semibold : .regular))
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(pane == item ? AnyShapeStyle(.quaternary) : AnyShapeStyle(.clear)))
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder private var tabContent: some View {
        switch pane {
        case .overview:
            AttemptsCard(
                attempts: attempts, job: store.job, openPod: openPod,
                openIncident: { navigator.open(.incident($0)) })
            JobCard(job: store.job, jobUID: jobUID, lastPodName: nil)
                .explained("jobCard")
            if let cronjobName = store.job?.cronjobName, !cronjobName.isEmpty {
                RunStripView(
                    cells: runCells(
                        jobs: store.siblingRuns, incidents: store.rows,
                        cronjobName: cronjobName),
                    now: Date(), openRun: { navigator.open(.run($0)) },
                    acknowledge: { ids in
                        Task { await store.acknowledge(ids: ids, connection: connection) }
                    },
                    marked: jobUID)
            }
        case .timeline:
            TimelineView(
                entries: store.timeline, truncated: store.timelineTruncated,
                isLoading: store.timelineLoading, error: store.timelineError)
                .explained("runTimeline")
        case .logs:
            CapturedLogsCard(
                artifacts: store.logArtifacts, selected: selectedArtifact,
                contents: store.contents, wrap: $wrap, select: { selectedArtifactID = $0 },
                emptyText: "idios captured no log for this run.")
                .explained("runLogs")
        case .events:
            EventsCard(
                events: store.events, title: "Events of this run",
                meta: "k8s_events of the Job and of its attempts' pods",
                emptyText: "idios kept no event for this run.", showsContainer: true)
                .explained("runEvents")
        }
    }

    /// explainScreen is which table the overlay draws: the tab in view,
    /// because the header and the rail are on screen on every one of them and
    /// the pane is not.
    private var explainScreen: ExplainedScreen { .run(pane.part) }

    private var attempts: [RunAttempt] {
        runAttempts(pods: store.pods, rows: store.rows, jobName: store.job?.name)
    }

    private var leadDetail: IncidentDetail? {
        store.lead.flatMap { store.details[$0.id] }
    }

    private var selectedArtifact: Artifact? {
        store.logArtifacts.first { $0.id == selectedArtifactID }
            ?? defaultArtifact(store.logArtifacts)
    }

    private var imageTag: String? {
        store.rows.compactMap(\.imageTag).first { !$0.isEmpty }
    }

    // The neighbours worth naming are the ones the Job's own condition names,
    // so a run with no reason of its own claims no history.
    private var sameReasonRuns: Int? {
        guard let reason = store.job?.conditionReason, !reason.isEmpty else { return nil }
        return store.siblingRuns.filter { $0.uid != jobUID && $0.conditionReason == reason }.count
    }

    private var clusterName: String {
        let id = store.job?.clusterID ?? store.rows.first?.clusterID ?? ""
        return clusters.first { $0.id == id }?.name ?? id
    }

    private var namespace: String {
        store.job?.namespace ?? store.rows.first?.namespace ?? ""
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
            NSPasteboard.general.clearContents()
            NSPasteboard.general.setString(jobUID, forType: .string)
        }
        .help(jobUID)
    }

    private func act(
        _ id: String, _ call: @escaping (String, DaemonConnection) async -> Incident?
    ) {
        Task {
            if let updated = await call(id, connection) {
                store.apply(updated)
            }
        }
    }
}

private struct RunKey: Hashable {
    let generation: Int
    let jobUID: String
}

private struct TimelineKey: Hashable {
    let generation: Int
    let leadID: String?
    let showing: Bool
}
