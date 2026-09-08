import IdiosModel
import SwiftUI

/// AttemptsCard is the run attempt by attempt: one line per pod the Job
/// started, in order, with the incident it opened and the way into that pod's
/// logs.
struct AttemptsCard: View {
    let attempts: [RunAttempt]
    let job: Job?
    let openPod: (String, PodTab) -> Void
    let openIncident: (String) -> Void

    var body: some View {
        DetailCard(
            title: "Attempts", meta: "pods WHERE job_uid = <uid>, one line per pod"
        ) {
            if attempts.isEmpty {
                Text("idios kept no attempt of this run.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                heading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(attempts) { attempt in
                        Divider()
                        row(attempt)
                    }
                }
            }
            if let pruned { prunedLine(pruned) }
        }
        .explained("attemptsCard")
    }

    private var heading: some View {
        HStack(alignment: .top, spacing: AttemptColumns.gap) {
            Text("#").frame(width: AttemptColumns.number, alignment: .leading)
            Text("POD").frame(width: AttemptColumns.pod, alignment: .leading)
            Text("CATEGORY").frame(width: AttemptColumns.category, alignment: .leading)
            Text("EXIT").frame(width: AttemptColumns.exit, alignment: .trailing)
            Text("REASON")
                .frame(minWidth: AttemptColumns.reason, maxWidth: .infinity, alignment: .leading)
            Text("OPENED").frame(width: AttemptColumns.opened, alignment: .leading)
            Text("INCIDENT").frame(width: AttemptColumns.incident, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    private func row(_ attempt: RunAttempt) -> some View {
        HStack(alignment: .top, spacing: AttemptColumns.gap) {
            // A row whose subject is the Job is not an attempt; it is the row
            // that closes the run.
            Text(attempt.number == 0 ? "job" : "\(attempt.number)")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(attempt.number == 0 ? .secondary : .primary)
                .frame(width: AttemptColumns.number, alignment: .leading)
            podCell(attempt)
                .frame(width: AttemptColumns.pod, alignment: .leading)
            categoryCell(attempt)
                .frame(width: AttemptColumns.category, alignment: .leading)
            Text(attempt.exitCode.map(String.init) ?? "-")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.secondary)
                .frame(width: AttemptColumns.exit, alignment: .trailing)
            ExpandableText(value: attempt.reason)
                .font(.system(size: 11, design: .monospaced))
                .frame(minWidth: AttemptColumns.reason, maxWidth: .infinity, alignment: .leading)
            Text(clockTime(attempt.openedAt))
                .font(.system(size: 10.5, design: .monospaced))
                .foregroundStyle(.secondary)
                .frame(width: AttemptColumns.opened, alignment: .leading)
            incidentCell(attempt)
                .frame(width: AttemptColumns.incident, alignment: .leading)
        }
        .padding(.vertical, 5)
    }

    @ViewBuilder private func podCell(_ attempt: RunAttempt) -> some View {
        if let uid = attempt.podUID {
            Button { openPod(uid, .logs) } label: {
                MiddleElidedText(value: attempt.podSuffix, keeping: 10)
                    .font(.system(size: 11, design: .monospaced))
            }
            .buttonStyle(.link)
        } else {
            Text("no pod")
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(.tertiary)
        }
    }

    @ViewBuilder private func categoryCell(_ attempt: RunAttempt) -> some View {
        if let category = attempt.category {
            CategoryBadge(category: category)
        } else {
            dash
        }
    }

    // An attempt that opened no incident has no id and no incident state; what
    // it has is how its pod ended, which the pill says instead.
    @ViewBuilder private func incidentCell(_ attempt: RunAttempt) -> some View {
        HStack(spacing: 5) {
            if let id = attempt.incidentID, let state = attempt.state {
                Button("#\(id)") { openIncident(id) }
                    .buttonStyle(.link)
                    .font(.system(size: 11, design: .monospaced))
                StateBadge(state: state)
            } else {
                dash
                if let outcome = attempt.outcome {
                    Badge(text: outcome.text, style: outcomeStyle(outcome))
                }
            }
        }
    }

    private var dash: some View {
        Text("-")
            .font(.system(size: 11, design: .monospaced))
            .foregroundStyle(.tertiary)
    }

    // Green is a live healthy thing and a run that completed; an attempt that
    // succeeded is the second of those, and anything else takes the colour its
    // kubelet state carries everywhere.
    private func outcomeStyle(_ outcome: AttemptOutcome) -> BadgeStyle {
        switch outcome {
        case .succeeded: .green
        case .state(let state): state.badge
        case .phase: .neutral
        }
    }

    private func prunedLine(_ failed: Int32) -> some View {
        Text(
            "The Job counted \(failed) failed pods; idios kept \(plural(drawn, "failed attempt")). "
                + "The rest were pruned before it saw them.")
            .font(.system(size: 11))
            .foregroundStyle(.tertiary)
            .fixedSize(horizontal: false, vertical: true)
    }

    private var drawn: Int {
        attempts.filter { $0.number > 0 && $0.outcome != .succeeded }.count
    }

    // The Job's failed counter counts pods; what it counts beyond the lines
    // here is a record idios never had, and that is said rather than hidden.
    private var pruned: Int32? {
        guard let job, job.failed > Int32(drawn) else { return nil }
        return job.failed
    }
}

/// AttemptColumns is the card's column budget: a Table on macOS 15 clips a
/// column at its ideal width instead of shrinking it, so the rows are drawn
/// and measured here.
private enum AttemptColumns {
    static let number: CGFloat = 26
    static let pod: CGFloat = 150
    static let category: CGFloat = 96
    static let exit: CGFloat = 44
    static let reason: CGFloat = 140
    static let opened: CGFloat = 52
    static let incident: CGFloat = 96
    static let gap: CGFloat = 10
}
