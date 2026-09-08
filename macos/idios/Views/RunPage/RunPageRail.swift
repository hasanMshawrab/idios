import Foundation
import IdiosModel
import SwiftUI

/// RunPageRail is the run's identity: what owns it, where and how it was told
/// to run, its times, and the other runs of the same CronJob.
struct RunPageRail: View {
    let job: Job?
    let jobUID: String
    let attempts: [RunAttempt]
    let rows: [Incident]
    let siblingRuns: [Job]
    let lead: Incident?
    let now: Date
    let openPod: (String, PodTab) -> Void
    let openRun: (String) -> Void
    let openWorkload: (Route, WorkloadTab) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            ownerChain.explained("ownerChain")
            Divider()
            context
            Divider()
            times
            if let cronjobName, !cronjobName.isEmpty {
                Divider()
                otherRuns(cronjobName).explained("otherRuns")
            }
        }
    }

    private var cronjobName: String? { job?.cronjobName }

    private var ownerChain: some View {
        VStack(alignment: .leading, spacing: 10) {
            RailHeading(text: "OWNER CHAIN")
            VStack(alignment: .leading, spacing: 0) {
                if let cronjobName, !cronjobName.isEmpty {
                    chainLink(color: BadgeStyle.blue.text, last: false) {
                        railLink(label: "CRONJOB", value: cronjobName, note: nil)
                    }
                }
                chainLink(color: BadgeStyle.blue.text, last: podLinks.isEmpty) {
                    jobLink
                }
                if podLinks.isEmpty {
                    // The chain ends where the record does: the dot is grey
                    // because there is no pod to be alive or dead.
                    chainLink(color: BadgeStyle.grey.text, last: true) {
                        railLink(
                            label: "POD", value: "none kept",
                            note: "every pod of this run was pruned or swept")
                    }
                } else {
                    ForEach(podLinks) { attempt in
                        chainLink(
                            color: dotColor(attempt), last: attempt.id == podLinks.last?.id
                        ) {
                            podLink(attempt)
                        }
                    }
                }
            }
        }
    }

    private var jobLink: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 4) {
                Text("JOB (uid").font(.system(size: 9.5, weight: .bold))
                MiddleElidedText(value: jobUID, keeping: 12).font(.system(size: 9.5))
                Text(")").font(.system(size: 9.5, weight: .bold))
            }
            .foregroundStyle(.tertiary)
            Text(job?.name ?? "not recorded")
                .font(.system(size: 11.5, design: .monospaced))
                .textSelection(.enabled)
        }
    }

    private func podLink(_ attempt: RunAttempt) -> some View {
        Button {
            guard let uid = attempt.podUID else { return }
            openPod(uid, .containers)
        } label: {
            railLink(
                label: "POD (attempt \(attempt.number))", value: attempt.podSuffix,
                note: attempt.incidentID.map { "incident \($0)" } ?? attempt.outcome?.text ?? "")
        }
        .buttonStyle(.plain)
    }

    // An attempt that opened no incident is not a failure to point at: one
    // that succeeded is green and anything else is grey.
    private func dotColor(_ attempt: RunAttempt) -> Color {
        guard attempt.incidentID == nil else { return BadgeStyle.red.text }
        return attempt.outcome == .succeeded ? BadgeStyle.green.text : BadgeStyle.grey.text
    }

    private var podLinks: [RunAttempt] {
        attempts.filter { $0.number > 0 && $0.podUID != nil }
    }

    private var context: some View {
        VStack(alignment: .leading, spacing: 3) {
            RailHeading(text: "CONTEXT")
            RailRow(label: "Node") {
                Text(nodes.isEmpty ? "not scheduled" : nodes.joined(separator: ", "))
                    .help("incidents.node_name of the run's rows")
            }
            if !imageTags.isEmpty {
                RailRow(label: "Image") {
                    ExpandableText(value: imageTags.joined(separator: ", "))
                        .help("incidents.image_tag of the run's rows")
                }
            }
            if let job {
                RailRow(label: "Backoff limit") {
                    Text("\(job.backoffLimit)").help("jobs.backoff_limit")
                }
            }
        }
    }

    private var nodes: [String] {
        distinctValues(rows.map(\.nodeName).filter { !$0.isEmpty })
    }

    private var imageTags: [String] {
        distinctValues(rows.compactMap(\.imageTag).filter { !$0.isEmpty })
    }

    // The run's times and the lead incident's answer different questions, so
    // the incident's group stands under the run's rather than in its place.
    private var times: some View {
        VStack(alignment: .leading, spacing: 14) {
            runTimes
            if let lead {
                Divider()
                IncidentTimesRail(incident: lead, now: now)
            }
        }
    }

    private var runTimes: some View {
        VStack(alignment: .leading, spacing: 3) {
            RailHeading(text: "TIMES run")
            RailRow(label: "scheduled") {
                optionalStamp(job?.createdAt, column: "jobs.created_at", source: "k8s")
            }
            RailRow(label: "first attempt") {
                optionalStamp(
                    podLinks.first?.openedAt, column: "incidents.opened_at", source: "k8s")
            }
            RailRow(label: "finished") {
                optionalStamp(job?.finishedAt, column: "jobs.finished_at", source: "k8s")
            }
            if let lead {
                RailRow(label: "open for") { Text(openFor(lead, now: now)) }
            }
        }
    }

    private func otherRuns(_ cronjobName: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            RailHeading(text: "OTHER RUNS")
            ForEach(neighbours(cronjobName)) { cell in
                Button { openRun(cell.jobUID) } label: {
                    HStack(spacing: 6) {
                        Text(cell.name).font(.system(size: 11, design: .monospaced))
                        Spacer(minLength: 6)
                        Badge(text: cell.condition, style: runCellStyle(cell.outcome))
                    }
                }
                .buttonStyle(.plain)
                .help(cell.hover(now: now))
            }
            let more = siblingRuns.filter { $0.uid != jobUID }.count - Self.neighbourLimit
            if more > 0 {
                Button("+ \(more) more of this CronJob") {
                    openWorkload(
                        .workload(
                            cluster: job?.clusterID ?? "", namespace: job?.namespace ?? "",
                            kind: "CronJob", name: cronjobName), .runs)
                }
                .buttonStyle(.link)
                .font(.system(size: 10.5))
                .help("opens Workloads > \(cronjobName) > Runs")
            }
        }
    }

    // The neighbours are read as the strip reads them, so a run's pill in the
    // rail says what its cell in the line says.
    private func neighbours(_ cronjobName: String) -> [RunCell] {
        let others = runCells(jobs: siblingRuns, incidents: rows, cronjobName: cronjobName)
            .filter { $0.jobUID != jobUID }
        return Array(others.suffix(Self.neighbourLimit).reversed())
    }

    private static let neighbourLimit = 5
}

// The rail names what a run's rows agree or disagree on, in the order the
// rows first said it.
private func distinctValues(_ values: [String]) -> [String] {
    var seen: Set<String> = []
    return values.filter { seen.insert($0).inserted }
}
