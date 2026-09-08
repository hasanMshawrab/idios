import IdiosModel
import SwiftUI

/// PodCardPane is the middle column when the Pod card is selected: the
/// pod-level incidents' control and header when it has any, then Events,
/// Conditions, Files, pod.json and, for a Job's pod, Related incidents.
struct PodCardPane: View {
    let detail: PodDetail
    let clusterName: String
    let store: PodPageStore
    let podLevel: [Incident]
    let lit: IncidentDetail?
    let podUID: String
    @Binding var podLit: String?
    @Binding var selectedArtifactID: String?
    @Binding var wrap: Bool
    @Binding var selection: PodSelection
    let openIncident: (String) -> Void
    let openNote: () -> Void
    let openDelete: () -> Void
    let askAI: (AskAIRequest) -> Void

    private var pane: PodPane {
        guard case .pod(let pane) = selection else { return .events }
        return pane
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            if !podLevel.isEmpty { controlRow }
            // store.lit keeps the previous segment's incident until the next
            // one arrives, so the header reads it directly rather than the
            // selected id: gating on equality would blank the header for
            // every switch's async gap.
            if let lit {
                // IncidentHeader pads 20 where the pane pads 16, so the
                // verdict shifts 4 to stand over the tags it answers with.
                VerdictBlock(
                    incident: lit.incident, container: nil, artifacts: litArtifacts(lit))
                    .padding(.horizontal, 4)
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
            if podLevel.count >= 2 {
                Picker("", selection: podLitBinding) {
                    ForEach(podLevel) { incident in
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

    // A pod-level incident has no container of its own, so what it captured
    // is what hangs off the incident rather than off one container.
    private func litArtifacts(_ lit: IncidentDetail) -> [Artifact] {
        detail.artifacts.filter { $0.incidentID == lit.incident.id && $0.kind != .podJSON }
    }

    private var tabStrip: some View {
        HStack(spacing: 2) {
            tabButton(title: eventsTitle, item: .events)
            tabButton(title: "Conditions", item: .conditions)
            tabButton(title: "Files \(filesCount)", item: .files)
            tabButton(title: "pod.json", item: .podJSON)
            if detail.pod.controllerKind == "Job" {
                tabButton(title: "Related incidents", item: .related)
            }
            Spacer()
        }
        .padding(.horizontal, 14)
        .frame(height: 38)
    }

    private func tabButton(title: String, item: PodPane) -> some View {
        Button { selection = .pod(item) } label: {
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

    // The header already shows the fold lead before any click chooses a
    // segment, so the control must open bound to it too.
    private var podLitBinding: Binding<String?> {
        Binding(
            get: { podLit ?? segmentOrder(podLevel).first?.id },
            set: { podLit = $0 })
    }

    private var eventsTitle: String {
        store.events.map { "Events \($0.count)" } ?? "Events"
    }

    private var filesCount: Int {
        detail.artifacts.filter { $0.kind != .podJSON && $0.filePath != nil }.count
    }

    @ViewBuilder private var tabContent: some View {
        switch pane {
        case .events:
            VStack(alignment: .leading, spacing: 12) {
                EventsCard(
                    events: store.events ?? [], title: "Events on this pod",
                    meta: "k8s_events WHERE involved_uid = pod",
                    emptyText: "idios kept no event for this pod.", showsContainer: true)
                if store.eventsTruncated {
                    Text("The list was cut short by the daemon.")
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
            }
            .explained("podEvents")
        case .conditions:
            VStack(alignment: .leading, spacing: 12) {
                DetailCard(
                    title: "Conditions (latest per type)",
                    meta: "pod_condition_history, latest per type"
                ) {
                    VStack(alignment: .leading, spacing: 3) {
                        ForEach(detail.conditions, id: \.id) { condition in
                            FactRow(label: condition.type) {
                                Text(conditionText(condition))
                                    .foregroundStyle(
                                        condition.status == "True"
                                            ? BadgeStyle.green.text : BadgeStyle.red.text)
                            }
                        }
                    }
                }
                PodHistoryCards(history: store.history)
            }
            .explained("podConditions")
        case .files:
            CapturedFilesCard(
                title: "Captured files", artifacts: detail.artifacts.sorted(by: artifactOrder),
                grafanaURL: { artifact in
                    detail.containers.first { $0.name == artifact.containerName }?.grafanaURL
                }, showsContainer: true, emptyText: "idios captured no file for this pod.")
                .explained("podFiles")
        case .podJSON:
            podJSONContent.explained("podJSON")
        case .related:
            PodIncidentsCard(
                incidents: store.related, clusterName: clusterName, open: openIncident,
                title: "Related incidents",
                meta: "incidents of this pod's Job, from GetIncident when one is lit")
                .explained("relatedIncidents")
        }
    }

    private func conditionText(_ condition: PodCondition) -> String {
        guard let reason = condition.reason else { return condition.status }
        return "\(condition.status) - \(reason)"
    }

    @ViewBuilder private var podJSONContent: some View {
        if let artifact = detail.artifacts.first(where: { $0.kind == .podJSON }) {
            CapturedLogsCard(
                artifacts: [artifact], selected: artifact, contents: store.contents, wrap: $wrap,
                select: { selectedArtifactID = $0 })
        } else {
            DetailCard(title: "pod.json") {
                Text("No pod.json is attached to this pod.").foregroundStyle(.secondary)
            }
        }
    }
}
