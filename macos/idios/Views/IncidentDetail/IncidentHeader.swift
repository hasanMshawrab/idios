import Foundation
import IdiosModel
import SwiftUI

/// IncidentHeader says in one sentence what failed, tagged with its category
/// and state, plus the explanation when one is available.
struct IncidentHeader: View {
    let detail: IncidentDetail
    let now: Date

    private var incident: Incident { detail.incident }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            tags
            sentence
            // fixedSize here, as WrapText applies it, makes this VStack
            // report an ideal width that overflows the split view and
            // pushes the header off the window; lineLimit alone wraps.
            if let explanation {
                explanationText(explanation)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 20)
        .padding(.top, 16)
        .padding(.bottom, 14)
    }

    private var tags: some View {
        HStack(spacing: 9) {
            Badge(text: incident.category.rawValue.uppercased(), style: incident.category.badge)
            stateBadge
            if settling {
                Badge(text: "settling", style: .neutral)
                    .help(
                        "The pod has already succeeded; the closer confirms that before it "
                            + "closes the incident.")
            }
            Text("incident \(incident.id) - occurrences \(incident.occurrences)")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.tertiary)
            GrafanaLinkButton(urlString: detail.grafanaURL)
        }
    }

    // A closed incident is the whole screen, so its tag has nothing left to
    // signal and drops the colour the list rows still scan by.
    @ViewBuilder private var stateBadge: some View {
        if let reason = incident.closeReason, let closedAt = incident.closedAt {
            Badge(text: "closed - \(reason.label)", style: .neutral)
                .help("closed_at \(closedAt.raw) - close_reason \(reason.rawValue)")
        } else {
            Badge(text: stateTag, style: incident.state.badge)
                .help("opened_at \(incident.openedAt.raw)")
        }
    }

    // A pod that reached Succeeded while the incident is still open is a
    // container that finished its work, not one that is still failing.
    private var settling: Bool {
        incident.closedAt == nil && detail.pod?.phase == "Succeeded"
    }

    private func explanationText(_ explanation: Explanation) -> some View {
        Text(explanation.message)
            .font(.system(size: 13))
            .foregroundStyle(.secondary)
            .lineLimit(nil)
            .textSelection(.enabled)
            .help(explanation.provenance)
    }

    private var explanation: Explanation? {
        IdiosModel.explanation(for: incident, events: detail.events, pod: detail.pod)
    }

    private var sentence: some View {
        headerSentence(incident, jobName: detail.job?.name)
            .font(.system(size: 22, weight: .semibold))
            .textSelection(.enabled)
    }

    private var stateTag: String {
        guard let opened = incident.openedAt.date else { return incident.state.rawValue.uppercased() }
        return "\(incident.state.rawValue.uppercased()) \(durationText(from: opened, to: now))"
    }
}

/// headerSentence states what the kubelet reported as a person would say it.
/// jobName names a job incident's subject; the incident row only knows the
/// job's uid.
func headerSentence(_ incident: Incident, jobName: String? = nil) -> Text {
    let owner = incident.workloadName.isEmpty ? (incident.podName ?? "") : incident.workloadName
    if incident.subjectKind == .job {
        return Text("Job ") + mono(jobName ?? owner) + Text(" is ") + Text(incident.lastReason)
    }
    guard let container = incident.containerName else {
        // The incident id is not a name; a pod-level incident whose pod name
        // was never recorded reads better under its workload.
        return Text("Pod ") + mono(incident.podName ?? owner) + Text(" is ")
            + Text(incident.lastReason)
    }
    let head = Text("Container ") + mono(container) + Text(" of ") + mono(owner)
    guard let exit = incident.exitCode else { return head + Text(" is ") + Text(incident.lastReason) }
    return head + Text(" exits \(exit) and is in ") + Text(incident.lastReason)
}

// Internal rather than private: PodPageHeader's title is the same treatment
// at the same size, on the pod's name instead of a container or workload's.
func mono(_ value: String) -> Text {
    Text(value).font(.system(size: 20, weight: .semibold, design: .monospaced))
}
