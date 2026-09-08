import IdiosModel
import SwiftUI

/// JobCard is what the Job itself reports for an incident whose subject is a
/// Job; its pods are transient and may be pruned before anyone reads this.
struct JobCard: View {
    let job: Job?
    let jobUID: String?
    let lastPodName: String?

    var body: some View {
        DetailCard(title: "WHAT THE JOB REPORTS", meta: meta) {
            if let job {
                facts(job)
            } else {
                Text("No jobs row is kept for job_uid \(jobUID ?? "none recorded").")
                    .font(.system(size: 11.5))
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func facts(_ job: Job) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            FactRow(label: "Job") {
                Text(job.name).font(.system(size: 11.5, design: .monospaced))
            }
            if let type = job.conditionType {
                FactRow(label: "Condition") { Text(condition(type, reason: job.conditionReason)) }
            }
            if let message = job.conditionMessage {
                FactRow(label: "Condition message") { WrapText(value: message) }
            }
            FactRow(label: "Counters") {
                Text(jobCounters(job))
                    .help("jobs.succeeded, completions, parallelism, failed and backoff_limit")
            }
            if let deadline = job.activeDeadlineSeconds {
                FactRow(label: "Deadline") {
                    Text(deadlineText(deadline))
                        .help("jobs.active_deadline_seconds, the Job's spec.activeDeadlineSeconds")
                }
            }
            if let started = job.startedAt {
                FactRow(label: "Started") { Text("\(started.raw) (k8s)") }
            }
            if let finished = job.finishedAt {
                FactRow(label: "Finished") { Text(finishedText(finished, startedAt: job.startedAt)) }
            }
            if let cronjob = job.cronjobName {
                FactRow(label: "CronJob") {
                    Text(cronjob).font(.system(size: 11.5, design: .monospaced))
                }
            }
            if let lastPodName {
                FactRow(label: "Last pod") {
                    VStack(alignment: .leading, spacing: 1) {
                        Text(lastPodName).font(.system(size: 11.5, design: .monospaced))
                        Text("the newest pod seen for this Job; it may since have been pruned")
                            .font(.system(size: 10.5))
                            .foregroundStyle(.tertiary)
                    }
                }
            }
        }
    }

    private var meta: String {
        let source = "jobs WHERE uid = incidents.job_uid"
        guard let job else { return source }
        return source
            + ". Success and failure come from condition_type and condition_reason; the failed "
            + "counter is context only, because it counts pods, not attempts, and restart_policy "
            + "\(job.restartPolicy ?? "none recorded") decides whether a retry reaches it at all."
    }

    private func condition(_ type: String, reason: String?) -> String {
        guard let reason else { return type }
        return "\(type) - \(reason)"
    }

    // The deadline is what DeadlineExceeded exceeded; the raw seconds stay
    // next to the readable form because the spec is written in seconds.
    private func deadlineText(_ seconds: Int64) -> String {
        let readable = durationText(
            from: Date(timeIntervalSince1970: 0),
            to: Date(timeIntervalSince1970: TimeInterval(seconds)))
        return "active for at most \(readable) (\(seconds)s)"
    }

    private func finishedText(_ finished: Timestamp, startedAt: Timestamp?) -> String {
        let stamp = "\(finished.raw) (k8s)"
        guard let from = startedAt?.date, let to = finished.date else { return stamp }
        return "\(stamp) - ran \(durationText(from: from, to: to))"
    }
}
