import Foundation
import IdiosModel
import SwiftUI

/// RunPageHeader is the run's own header: the Job's condition, what it cost,
/// and the chain that owns it.
struct RunPageHeader: View {
    let job: Job?
    let jobUID: String
    let attempts: [RunAttempt]
    let rows: [Incident]
    let clusterName: String
    let namespace: String
    let cronjobName: String?
    let openWorkload: (Route, WorkloadTab) -> Void
    let revealNamespace: (String, String) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 9) {
                Badge(text: runStateTag(job), style: tagStyle)
                    .explained("runStateTag")
                Text(runCountsLine(job: job, attempts: attempts, rows: rows, now: Date()))
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundStyle(.tertiary)
                    .explained("runCounts")
            }
            title
                .font(.system(size: 20, weight: .semibold))
                .textSelection(.enabled)
            identityLine
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 20)
        .padding(.top, 16)
        .padding(.bottom, 14)
    }

    // A run that completed is live and healthy, and a swept record is grey
    // whatever it once said.
    private var tagStyle: BadgeStyle {
        guard let job else { return .neutral }
        if job.deletedAt != nil { return IncidentState.podDeleted.badge }
        switch job.conditionType {
        case "Complete": return BadgeStyle.green
        case "Failed": return BadgeStyle.red
        default: return .neutral
        }
    }

    // The title is drawn from the same parts the page's name is made of, so
    // the names in it can carry the monospaced treatment every name gets.
    private var title: Text {
        guard let name = job?.name, !name.isEmpty else {
            return Text("Job ") + mono(middleElided(jobUID, keeping: 12))
        }
        guard let cronjobName, !cronjobName.isEmpty else { return Text("Job ") + mono(name) }
        return Text("Run ") + mono(podNameSuffix(name: name, workloadName: cronjobName))
            + Text(" of ") + mono(cronjobName)
    }

    // The line is where a person leaves the run: each segment opens what it
    // names. The Job itself is not a segment; it is the page.
    private var identityLine: some View {
        HStack(spacing: 0) {
            segment(clusterName) { revealNamespace(clusterID, namespace) }
            separator
            segment(namespace) { revealNamespace(clusterID, namespace) }
            if let cronjobName, !cronjobName.isEmpty {
                separator
                segment("CronJob \(cronjobName)") {
                    openWorkload(
                        .workload(
                            cluster: clusterID, namespace: namespace, kind: "CronJob",
                            name: cronjobName), .runs)
                }
            }
            Spacer(minLength: 0)
        }
        .font(.system(size: 12, design: .monospaced))
    }

    private var clusterID: String { job?.clusterID ?? rows.first?.clusterID ?? "" }

    private var separator: some View {
        Text(" / ").font(.system(size: 12, design: .monospaced)).foregroundStyle(.secondary)
    }

    @ViewBuilder private func segment(_ text: String, open: @escaping () -> Void) -> some View {
        if text.isEmpty {
            Text(text).foregroundStyle(.secondary)
        } else {
            Button(text, action: open)
                .buttonStyle(.link)
                .font(.system(size: 12, design: .monospaced))
        }
    }
}
