import IdiosModel
import SwiftUI

/// ContainerPane is the middle column when a container card is selected: the
/// segment for each incident on it, the lit incident's header and actions,
/// and the container's Overview, Timeline and Logs.
struct ContainerPane: View {
    let detail: PodDetail
    let container: Container
    let incidents: [Incident]
    /// lit is the store's currently loaded incident, kept until the next
    /// segment's arrives.
    let lit: IncidentDetail?
    let litID: String?
    let podUID: String
    let store: PodPageStore
    @Binding var selection: PodSelection
    @Binding var selectedArtifactID: String?
    @Binding var wrap: Bool
    let openNote: () -> Void
    let openDelete: () -> Void
    let askAI: (AskAIRequest) -> Void

    private var tab: ContainerTab {
        guard case .container(_, _, let tab) = selection else { return .overview }
        return tab
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !incidents.isEmpty { controlRow }
            // store.lit keeps the previous segment's incident until the next
            // one arrives, so the header reads it directly rather than the
            // selected id: gating on equality would blank the header for
            // every switch's async gap.
            if let lit {
                // IncidentHeader pads 20 where the pane pads 16, so the
                // verdict shifts 4 to stand over the tags it answers with.
                VerdictBlock(
                    incident: lit.incident, container: container, artifacts: logArtifacts)
                    .padding(.horizontal, 4)
                    .explained("verdict")
                IncidentHeader(detail: lit, now: Date())
                if let note = lit.incident.note {
                    WrapText(value: note)
                        .font(.system(size: 11.5))
                        .foregroundStyle(.secondary)
                }
                if let actionError = store.writes.actionError {
                    WrapText(value: actionError.message)
                        .font(.system(size: 11))
                        .foregroundStyle(BadgeStyle.red.text)
                }
            } else if let litError = store.litError {
                WrapText(value: litError.message)
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            }
            tabStrip
            Divider()
            tabContent
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var controlRow: some View {
        HStack {
            // One segment painted as a button says nothing the header does
            // not already say.
            if incidents.count >= 2 {
                Picker("", selection: incidentIDBinding) {
                    ForEach(incidents) { incident in
                        Text("\(incident.category.label) - incident \(incident.id)")
                            .tag(Optional(incident.id))
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .fixedSize()
            }
            Spacer()
            if let lit {
                PaneActionsRow(
                    incident: lit.incident, store: store, podUID: podUID, openNote: openNote,
                    openDelete: openDelete, askAI: askAI)
            }
        }
    }

    private var incidentIDBinding: Binding<String?> {
        Binding(
            get: {
                guard case .container(_, let id, _) = selection else { return nil }
                return id
            },
            set: { newID in
                guard case .container(let name, _, let tab) = selection else { return }
                selection = .container(name: name, incidentID: newID, tab: tab)
            })
    }

    private var tabStrip: some View {
        HStack(spacing: 2) {
            tabButton(title: "Overview", item: .overview)
            tabButton(title: "Timeline", item: .timeline)
            tabButton(title: "Logs \(plural(logCount, "file"))", item: .logs)
            Spacer()
            // A container with no incident has no header to carry the link,
            // so the tab strip is where the pane offers it.
            GrafanaLinkButton(urlString: container.grafanaURL)
        }
        .padding(.horizontal, 14)
        .frame(height: 38)
    }

    private func tabButton(title: String, item: ContainerTab) -> some View {
        Button {
            guard case .container(let name, let id, _) = selection else { return }
            selection = .container(name: name, incidentID: id, tab: item)
        } label: {
            Text(title)
                .font(.system(size: 12.5, weight: tab == item ? .semibold : .regular))
                .padding(.horizontal, 10)
                .padding(.vertical, 4)
                .background(
                    RoundedRectangle(cornerRadius: 6)
                        .fill(tab == item ? AnyShapeStyle(.quaternary) : AnyShapeStyle(.clear)))
        }
        .buttonStyle(.plain)
    }

    private var logCount: Int {
        detail.artifacts.filter {
            $0.containerName == container.name && $0.kind != .podJSON && $0.filePath != nil
        }.count
    }

    @ViewBuilder private var tabContent: some View {
        switch tab {
        case .overview:
            if let lit, lit.incident.subjectKind == .job, lit.job != nil {
                JobCard(job: lit.job, jobUID: lit.incident.jobUID, lastPodName: lit.lastPodName)
            }
            KubeletCard(
                container: container, phase: detail.pod.phase, qosClass: detail.pod.qosClass,
                incident: lit?.incident, job: lit?.job, now: Date())
                .explained("kubeletFacts")
            CapturedLogsCard(
                artifacts: logArtifacts, selected: selectedArtifact, contents: store.contents,
                wrap: $wrap, select: { selectedArtifactID = $0 },
                emptyText: "idios captured no log for this container.")
                .explained("capturedLogs")
            EventsCard(
                events: containerEvents(store.events ?? [], container: container.name),
                title: "Events of container \(container.name)",
                meta: "k8s_events WHERE involved_uid = pod AND (field_path names the container "
                    + "OR field_path IS NULL)",
                emptyText: "idios kept no event for this container.", incidentID: litID,
                marksPodScope: true)
        case .timeline:
            if litID != nil {
                TimelineView(
                    entries: store.timeline, truncated: store.timelineTruncated,
                    isLoading: store.timelineLoading, error: store.timelineError)
            } else {
                Text("Select an incident to see its timeline.").foregroundStyle(.secondary)
            }
        case .logs:
            CapturedLogsCard(
                artifacts: logArtifacts, selected: selectedArtifact, contents: store.contents,
                wrap: $wrap, select: { selectedArtifactID = $0 },
                emptyText: "idios captured no log for this container.")
                .explained("capturedLogs")
        }
    }

    private var logArtifacts: [Artifact] {
        containerLogArtifacts(container.name, detail: detail)
    }

    private var selectedArtifact: Artifact? {
        logArtifacts.first { $0.id == selectedArtifactID } ?? defaultArtifact(logArtifacts)
    }
}

/// PaneActionsRow is the lit incident's verbs: Acknowledge with its key,
/// Note beside it, and everything else behind Actions.
struct PaneActionsRow: View {
    let incident: Incident
    let writes: IncidentWriteStore
    /// apply hands the row a write answered back to the page that owns it.
    let apply: @MainActor (Incident, DaemonConnection) async -> Void
    /// bulk is a pane whose Acknowledge acts on more than this one incident:
    /// how many it acts on and the call that acknowledges them all.
    var bulk: (count: Int, acknowledge: @MainActor (DaemonConnection) async -> Void)?
    let openNote: () -> Void
    let openDelete: () -> Void
    let askAI: (AskAIRequest) -> Void

    @Environment(DaemonConnection.self) private var connection

    var body: some View {
        HStack(spacing: 10) {
            acknowledge
            Button("Note...") { openNote() }
            askAIButton
            actionsMenu
        }
    }

    private var acknowledge: some View {
        Button {
            guard let bulk else {
                act(writes.acknowledge)
                return
            }
            Task { await bulk.acknowledge(connection) }
        } label: {
            HStack(spacing: 6) {
                Text(acknowledgeTitle)
                keyCap
            }
        }
        .buttonStyle(.borderedProminent)
        .disabled(bulk.map { $0.count == 0 } ?? (incident.acknowledgedAt != nil))
        .help(incident.acknowledgedAt.map { "acknowledged \(clockTime($0))" } ?? "")
    }

    // A button that acts on more than the incident beside it says how many, so
    // no one presses it expecting one.
    private var acknowledgeTitle: String {
        guard let bulk, bulk.count > 1 else { return "Acknowledge" }
        return "Acknowledge \(bulk.count)"
    }

    // The letter that does this is on the button, so the key is learned
    // where it is used.
    private var keyCap: some View {
        Text("a")
            .font(.system(size: 10, design: .monospaced))
            .frame(width: 14, height: 14)
            .background(RoundedRectangle(cornerRadius: 3).fill(.white.opacity(0.22)))
    }

    // Ask AI is the page's way out to an agent, so it stands in the row on
    // its own rather than behind Actions.
    private var askAIButton: some View {
        Menu {
            askAIItems
        } label: {
            HStack(spacing: 5) {
                Image(systemName: "sparkles")
                Text("Ask AI")
            }
            .font(.system(size: 13, weight: .medium))
            .foregroundStyle(.white)
            .padding(.horizontal, 10)
            .padding(.vertical, 3)
            .background(RoundedRectangle(cornerRadius: 6).fill(BadgeStyle.askAI))
        }
        // The bordered menu styles repaint the label in their own chrome; the
        // plain button style draws it as given, so the gradient survives.
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .fixedSize()
        .help("copy or save a prompt about this incident for an AI agent")
    }

    private var actionsMenu: some View {
        Menu("Actions") {
            dismissButton
            if incident.closedAt == nil {
                Button("Mark resolved") { act(writes.resolve) }
            }
            if incident.acknowledgedAt != nil {
                Button("Unacknowledge") { act(writes.unacknowledge) }
            }
            // Manual is the one close a person chose, so it is the one a
            // person can take back; every other close is the recorder's own
            // finding.
            if incident.closeReason == .manual {
                Button("Unresolve") { act(writes.unresolve) }
            }
            Divider()
            Button("Delete incident") { openDelete() }
        }
        .fixedSize()
    }

    @ViewBuilder private var askAIItems: some View {
        Button("Copy prompt for a local agent (MCP)") {
            askAI(AskAIRequest(mode: .mcp, action: .copy))
        }
        Button("Copy full snapshot") { askAI(AskAIRequest(mode: .snapshot, action: .copy)) }
        Button("Save snapshot as...") { askAI(AskAIRequest(mode: .snapshot, action: .save)) }
    }

    @ViewBuilder private var dismissButton: some View {
        if incident.dismissedAt != nil {
            Button("Undismiss") { act(writes.undismiss) }
        } else {
            Button("Dismiss") { act(writes.dismiss) }
        }
    }

    private func act(_ call: @escaping (String, DaemonConnection) async -> Incident?) {
        let id = incident.id
        Task {
            if let updated = await call(id, connection) {
                await apply(updated, connection)
            }
        }
    }
}

extension PaneActionsRow {
    /// init is the row on a pod page pane: the writes are the page's, and a
    /// write's answer goes back through the store that loaded the pod.
    init(
        incident: Incident, store: PodPageStore, podUID: String?,
        openNote: @escaping () -> Void, openDelete: @escaping () -> Void,
        askAI: @escaping (AskAIRequest) -> Void
    ) {
        self.init(
            incident: incident, writes: store.writes,
            apply: { row, connection in
                await store.apply(row, uid: podUID, connection: connection)
            },
            openNote: openNote, openDelete: openDelete, askAI: askAI)
    }
}
