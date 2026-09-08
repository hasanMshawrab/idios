import IdiosModel
import SwiftUI

/// PodColumn is the left column of the pod page: the Pod as a card, then its
/// containers as cards indented under it, each carrying the badges of the
/// incidents open on it.
struct PodColumn: View {
    let detail: PodDetail
    /// eventCount is nil until the pod's events answered, so the Pod card
    /// does not claim a count it has not seen.
    let eventCount: Int?
    @Binding var selection: PodSelection
    /// podLevel are the incidents whose badge sits on the Pod card rather
    /// than on any container.
    let podLevel: [Incident]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                podCard
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(containerOrder(detail.containers)) { container in
                        containerCard(container)
                    }
                }
                .explained("containerCards")
            }
            .padding(12)
        }
    }

    private var podCard: some View {
        Button {
            guard case .pod = selection else {
                selection = .pod(.events)
                return
            }
        } label: {
            VStack(alignment: .leading, spacing: 4) {
                Text("POD")
                    .font(.system(size: 9.5, weight: .bold))
                    .foregroundStyle(secondaryStyle(podSelected, opacity: 0.8))
                Text(detail.pod.name)
                    .font(.system(size: 12, weight: .semibold, design: .monospaced))
                Text(podSubline)
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(secondaryStyle(podSelected, opacity: 0.75))
                if !podLevel.isEmpty {
                    badgeRow(segmentOrder(podLevel), selected: podSelected)
                }
            }
            .padding(10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 8).fill(podSelected ? Color.accentColor : .clear))
            .foregroundStyle(podSelected ? Color.white : Color.primary)
        }
        .buttonStyle(.plain)
        .help(detail.pod.name)
    }

    private var podSelected: Bool {
        if case .pod = selection { return true }
        return false
    }

    private var podSubline: String {
        let events = eventCount.map { "events \($0)" } ?? "events"
        let files = detail.artifacts.filter { $0.kind != .podJSON && $0.filePath != nil }.count
        return "\(events) - conditions - files \(files) - pod.json"
    }

    private func containerCard(_ container: Container) -> some View {
        let selected = isSelected(container)
        let rows = segmentOrder(incidents(of: container))
        return Button {
            selectContainer(container, rows: rows)
        } label: {
            HStack(alignment: .top, spacing: 8) {
                Rectangle().fill(.tertiary).frame(width: 1)
                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        Text(container.name)
                            .font(.system(size: 11.5, weight: .semibold, design: .monospaced))
                        Text(container.kind.rawValue.uppercased())
                            .font(.system(size: 9, weight: .semibold))
                            .foregroundStyle(secondaryStyle(selected, opacity: 0.75))
                    }
                    Text(containerStateLine(container))
                        .font(.system(size: 11, design: .monospaced))
                        .foregroundStyle(secondaryStyle(selected, opacity: 0.85))
                    if !rows.isEmpty { badgeRow(rows, selected: selected) }
                }
            }
            .padding(.vertical, 6)
            .padding(.trailing, 8)
            .padding(.leading, 16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: 8).fill(selected ? Color.accentColor : .clear))
            .foregroundStyle(selected ? Color.white : Color.primary)
        }
        .buttonStyle(.plain)
        .help(container.name)
    }

    // The selected card fills with the accent, so its secondary text needs
    // white at a lower opacity instead of the system secondary style.
    private func secondaryStyle(_ selected: Bool, opacity: Double) -> AnyShapeStyle {
        selected ? AnyShapeStyle(Color.white.opacity(opacity)) : AnyShapeStyle(Color.secondary)
    }

    private func isSelected(_ container: Container) -> Bool {
        if case .container(let name, _, _) = selection { return name == container.name }
        return false
    }

    // Re-clicking the already-selected card must not drop an incident id
    // the card's own rows do not carry, such as a job's borrowed segment.
    private func selectContainer(_ container: Container, rows: [Incident]) {
        guard !isSelected(container) else { return }
        selection = .container(name: container.name, incidentID: rows.first?.id, tab: .overview)
    }

    private func incidents(of container: Container) -> [Incident] {
        detail.incidents.filter { $0.containerName == container.name }
    }

    // On the selected card the accent fill sits under every badge, so a
    // translucent capsule loses its own colour; an opaque white capsule
    // underneath keeps the category colour readable.
    private func badgeRow(_ incidents: [Incident], selected: Bool) -> some View {
        HStack(spacing: 4) {
            ForEach(incidents) { incident in
                Badge(
                    text: "\(incident.category.label) \(incident.occurrences)",
                    style: incident.category.badge)
                    .background(selected ? Capsule().fill(Color.white) : nil)
                    .opacity(incident.closedAt == nil ? 1 : 0.7)
            }
        }
    }
}
