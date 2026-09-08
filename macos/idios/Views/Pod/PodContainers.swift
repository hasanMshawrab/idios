import IdiosModel
import SwiftUI

/// CapturedFilesCard is every capture attempt for a container: the file idios
/// wrote, or the gap that says why there is none.
struct CapturedFilesCard: View {
    var title = "Captured files"
    let artifacts: [Artifact]
    /// grafanaURL is the fallback link of a gap row; the Pod card's Files
    /// tab passes the map so each row finds its own container's.
    let grafanaURL: (Artifact) -> String?
    var showsContainer = false
    var emptyText = "idios captured no file for this container."

    var body: some View {
        DetailCard(title: title) {
            if artifacts.isEmpty {
                Text(emptyText)
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                heading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(artifacts) { artifact in
                        Divider()
                        row(artifact)
                    }
                }
            }
        }
    }

    private var heading: some View {
        HStack(alignment: .top, spacing: 7) {
            Text("FILE").frame(width: 84, alignment: .leading)
            Text("KIND").frame(width: 84, alignment: .leading)
            if showsContainer {
                Text("CONTAINER").frame(width: 96, alignment: .leading)
            }
            Text("INDEX").frame(width: 36, alignment: .trailing)
            Text("SIZE").frame(width: 56, alignment: .trailing)
            Text("TRUNCATED").frame(width: 70, alignment: .leading)
            Text("EARLY").frame(width: 44, alignment: .leading)
            Text("CAPTURED / GAP").frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    private func row(_ artifact: Artifact) -> some View {
        let missing = artifact.filePath == nil
        return HStack(alignment: .top, spacing: 7) {
            Text(artifactLabel(artifact))
                .font(.system(size: 11, design: .monospaced))
                .strikethrough(missing)
                .frame(width: 84, alignment: .leading)
                .help(artifact.filePath ?? "no file was written")
            Text(artifact.kind.rawValue)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .frame(width: 84, alignment: .leading)
            if showsContainer {
                Text(artifact.containerName ?? "")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .frame(width: 96, alignment: .leading)
            }
            Text(index(artifact))
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 36, alignment: .trailing)
            Text(artifact.sizeBytes.map(byteCount) ?? "-")
                .font(.system(size: 11))
                .monospacedDigit()
                .foregroundStyle(.secondary)
                .frame(width: 56, alignment: .trailing)
            Text(missing ? "-" : (artifact.truncated ? "yes" : "no"))
                .font(.system(size: 11))
                .foregroundStyle(
                    artifact.truncated
                        ? AnyShapeStyle(BadgeStyle.orange.text) : AnyShapeStyle(.secondary))
                .frame(width: 70, alignment: .leading)
            Text(missing ? "-" : (artifact.capturedEarly ? "yes" : "no"))
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .frame(width: 44, alignment: .leading)
            when(artifact)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 5)
        .opacity(missing ? 0.75 : 1)
    }

    // restart_count -1 labels the running container, which has no dead instance
    // index; the mockup writes it as a dash.
    private func index(_ artifact: Artifact) -> String {
        artifact.restartCount < 0 ? "-" : "\(artifact.restartCount)"
    }

    @ViewBuilder private func when(_ artifact: Artifact) -> some View {
        if let gap = artifact.captureGap {
            HStack(alignment: .top, spacing: 6) {
                GapBadge(gap: gap)
                GrafanaLinkButton(urlString: grafanaURL(artifact))
                if let note = artifact.captureNote {
                    WrapText(value: "\"\(note)\"")
                        .font(.system(size: 10.5, design: .monospaced))
                        .foregroundStyle(.tertiary)
                }
            }
        } else {
            Text(artifact.capturedAt?.raw ?? "null")
                .font(.system(size: 9.5, design: .monospaced))
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
        }
    }
}

/// PodIncidentsCard lists the incidents idios opened on this pod, open and
/// closed, in the row the triage list uses.
struct PodIncidentsCard: View {
    let incidents: [Incident]
    let clusterName: String
    let open: (String) -> Void
    var title = "Incidents on this pod"
    var meta = "incidents WHERE pod_uid = ?"

    var body: some View {
        DetailCard(title: title, meta: meta) {
            if incidents.isEmpty {
                Text("idios opened no incident on this pod.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(incidents) { incident in
                        Divider()
                        Button { open(incident.id) } label: {
                            IncidentRowView(
                                incident: incident, clusterName: clusterName,
                                selectedClusters: 1, now: Date())
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }
}
