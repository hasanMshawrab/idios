import IdiosModel
import SwiftUI

/// SearchHitRow is one hit as the palette draws it: a glyph or badge, the
/// name with what disambiguates it, and the state or time at the right.
struct SearchHitRow: View {
    let hit: SearchHit
    let now: Date
    let selected: Bool

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            switch hit {
            case .pod(let row): podCell(row)
            case .run(let fold): runCell(fold)
            case .workload(let workload): workloadCell(workload)
            case .incident(let incident): incidentCell(incident)
            case .command(let command): commandCell(command)
            }
        }
        .font(.system(size: 12))
        .lineLimit(1)
        .foregroundStyle(primary)
        .padding(.horizontal, 9)
        .padding(.vertical, 5)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 6).fill(selected ? Color.accentColor : .clear))
        .contentShape(Rectangle())
    }

    private var primary: AnyShapeStyle {
        selected
            ? AnyShapeStyle(Color(nsColor: .alternateSelectedControlTextColor))
            : AnyShapeStyle(.primary)
    }

    // The accent fill swallows a grey secondary, so a selected row says the
    // same thing with the selection's own colour, held back.
    private var muted: AnyShapeStyle {
        selected
            ? AnyShapeStyle(Color(nsColor: .alternateSelectedControlTextColor).opacity(0.75))
            : AnyShapeStyle(.secondary)
    }

    @ViewBuilder private func podCell(_ row: PodRow) -> some View {
        if let state = row.worstState {
            RowDot(color: podBadge(worstState: state, phase: row.phase).text)
        }
        HStack(spacing: 0) {
            Text(namePrefix(name: row.name, workloadName: row.workloadName))
                .foregroundStyle(muted)
            Text(podNameSuffix(name: row.name, workloadName: row.workloadName))
        }
        .font(.system(size: 12, design: .monospaced))
        Text(row.namespace).foregroundStyle(muted)
        Spacer(minLength: 8)
        Text(durationText(from: row.lastSeenAt.date ?? now, to: now)).foregroundStyle(muted)
    }

    @ViewBuilder private func runCell(_ fold: RunFold) -> some View {
        Text("run").foregroundStyle(muted)
        Text(fold.suffix ?? middleElided(fold.jobUID, keeping: 12))
            .font(.system(size: 12, design: .monospaced))
        Text("\(fold.podCount) pods").foregroundStyle(muted)
        ForEach(fold.categories, id: \.self) { category in
            CategoryBadge(category: category)
        }
        Text(fold.reasons.joined(separator: ", ")).foregroundStyle(muted)
        Spacer(minLength: 8)
        let badge = groupBadge(fold.rows)
        Badge(text: badge.text, style: badge.tone.badge)
    }

    @ViewBuilder private func workloadCell(_ workload: Workload) -> some View {
        Text(workload.workloadKind).foregroundStyle(muted)
        Text(workload.workloadName)
        Text(workload.namespace).foregroundStyle(muted)
        Spacer(minLength: 8)
        if workload.openIncidents > 0 {
            Badge(text: "\(workload.openIncidents) open", style: StateTone.open.badge)
        }
    }

    @ViewBuilder private func incidentCell(_ incident: Incident) -> some View {
        Text("#\(incident.id)").font(.system(size: 12, design: .monospaced))
        CategoryBadge(category: incident.category)
        Text(
            scopeLabel(
                containerName: incident.containerName, subjectKind: incident.subjectKind))
            .font(.system(size: 11.5, design: .monospaced))
            .foregroundStyle(muted)
        Text(workloadTitle(incident))
        if let podName = incident.podName, !incident.workloadName.isEmpty {
            Text(podNameSuffix(name: podName, workloadName: incident.workloadName))
                .font(.system(size: 11.5, design: .monospaced))
                .foregroundStyle(muted)
        }
        Spacer(minLength: 8)
        StateBadge(state: incident.state)
        Text(
            durationText(
                from: (incident.closedAt ?? incident.openedAt).date ?? now, to: now))
            .foregroundStyle(muted)
    }

    @ViewBuilder private func commandCell(_ command: SearchCommand) -> some View {
        Text(command.title)
        Spacer(minLength: 8)
        if let key = command.key {
            KeySymbols(key: key).foregroundStyle(muted)
        }
    }

    private func namePrefix(name: String, workloadName: String) -> String {
        let suffix = podNameSuffix(name: name, workloadName: workloadName)
        return String(name.dropLast(suffix.count))
    }
}

/// KeySymbols draws a key the way a menu does: the modifier as its glyph and
/// the character after it.
struct KeySymbols: View {
    let key: String

    var body: some View {
        HStack(spacing: 1) {
            ForEach(Array(parts.enumerated()), id: \.offset) { part in
                switch part.element {
                case .symbol(let name): Image(systemName: name)
                case .text(let text): Text(text)
                }
            }
        }
        .font(.system(size: 11))
    }

    private enum Part {
        case symbol(String)
        case text(String)
    }

    private var parts: [Part] {
        key.split(separator: "-", omittingEmptySubsequences: false).map { word in
            switch word {
            case "cmd": .symbol("command")
            case "shift": .symbol("shift")
            case "return": .symbol("return")
            default: .text(String(word))
            }
        }
    }
}
