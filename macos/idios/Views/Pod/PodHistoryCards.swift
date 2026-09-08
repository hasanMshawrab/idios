import IdiosModel
import SwiftUI

/// PodHistoryCards is everything the history endpoint returns, oldest first: the
/// condition readings idios recorded and the container states behind them. The
/// rows carry prose, so they are custom rows and not a Table: one row height for
/// every row would cut the messages.
struct PodHistoryCards: View {
    let history: PodHistory?

    var body: some View {
        if let history {
            conditions(history.conditions)
            transitions(history.transitions)
        } else {
            ProgressView().frame(maxWidth: .infinity)
        }
    }

    private func conditions(_ rows: [PodCondition]) -> some View {
        DetailCard(title: "Conditions", meta: "pod_condition_history WHERE pod_uid = ?") {
            if rows.isEmpty {
                Text("idios recorded no condition for this pod.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                conditionHeading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(rows, id: \.id) { condition in
                        Divider()
                        conditionRow(condition)
                    }
                }
            }
        }
    }

    private var conditionHeading: some View {
        HStack(alignment: .top, spacing: 10) {
            Text("TYPE").frame(width: 196, alignment: .leading)
            Text("STATUS").frame(width: 54, alignment: .leading)
            Text("REASON").frame(width: 150, alignment: .leading)
            Text("K8S TRANSITION / OBSERVED, THEN MESSAGE")
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    // The message is prose and the two timestamps are 27 characters each: side
    // by side they leave the message a column too narrow to read, so the row is
    // two lines rather than six columns.
    private func conditionRow(_ condition: PodCondition) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .top, spacing: 10) {
                Text(condition.type)
                    .font(.system(size: 11, design: .monospaced))
                    .frame(width: 196, alignment: .leading)
                Text(condition.status)
                    .font(.system(size: 11))
                    .foregroundStyle(
                        condition.status == "True" ? BadgeStyle.green.text : BadgeStyle.red.text)
                    .frame(width: 54, alignment: .leading)
                Text(condition.reason ?? "-")
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .frame(width: 150, alignment: .leading)
                Text(
                    "\(condition.k8sTransitionAt?.raw ?? "null") / \(condition.observedAt.raw)")
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            if let message = condition.message {
                WrapText(value: message)
                    .font(.system(size: 11))
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .padding(.vertical, 5)
    }

    private func transitions(_ rows: [ContainerTransition]) -> some View {
        DetailCard(title: "Container transitions", meta: "container_state_history WHERE pod_uid = ?")
        {
            if rows.isEmpty {
                Text("idios recorded no container transition for this pod.")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            } else {
                transitionHeading
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(rows, id: \.id) { transition in
                        Divider()
                        transitionRow(transition)
                    }
                }
            }
        }
    }

    private var transitionHeading: some View {
        HStack(alignment: .top, spacing: 10) {
            Text("CONTAINER").frame(width: 110, alignment: .leading)
            Text("STATE / REASON").frame(width: 190, alignment: .leading)
            Text("EXIT / SIGNAL").frame(width: 110, alignment: .leading)
            Text("RESTARTS").frame(width: 96, alignment: .trailing)
            Text("K8S TIMES, OBSERVED, GAP BELOW")
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .font(.system(size: 9.5, weight: .semibold))
        .foregroundStyle(.tertiary)
    }

    // Three timestamps and a badge do not fit beside the state; the row carries
    // them on a second line rather than cutting any of them.
    private func transitionRow(_ transition: ContainerTransition) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .top, spacing: 10) {
                Text(transition.containerName)
                    .font(.system(size: 11, design: .monospaced))
                    .frame(width: 110, alignment: .leading)
                ContainerStateBadge(
                    state: transition.state, reason: transition.reason,
                    exitCode: transition.exitCode)
                    .frame(width: 190, alignment: .leading)
                Text(exitSignal(transition))
                    .font(.system(size: 11))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .frame(width: 110, alignment: .leading)
                Text("\(transition.restartCount)")
                    .font(.system(size: 11))
                    .monospacedDigit()
                    .foregroundStyle(.secondary)
                    .frame(width: 96, alignment: .trailing)
                Spacer(minLength: 0)
            }
            HStack(alignment: .top, spacing: 8) {
                Text(times(transition))
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(.tertiary)
                    .textSelection(.enabled)
                if transition.gapReconstructed {
                    Badge(text: "gap_reconstructed", style: .gap)
                }
                Spacer(minLength: 0)
            }
        }
        .padding(.vertical, 5)
    }

    private func times(_ transition: ContainerTransition) -> String {
        "k8s_started \(transition.k8sStartedAt?.raw ?? "null")"
            + " - k8s_finished \(transition.k8sFinishedAt?.raw ?? "null")"
            + " - observed \(transition.observedAt.raw)"
    }

    // The kubelet reports signal 0 when a container was not signalled; saying
    // "signal 0" would read as a signal that was sent.
    private func exitSignal(_ transition: ContainerTransition) -> String {
        guard let exit = transition.exitCode else { return "-" }
        guard let signal = transition.signal, signal != 0 else { return "exit \(exit)" }
        return "exit \(exit), signal \(signal)"
    }
}
