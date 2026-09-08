/// RunFold is one Job's incidents inside a CronJob's or Job's group: the
/// job_failed row and the pod rows of that run, read as one line.
public struct RunFold: Identifiable, Hashable, Sendable {
    public let id: String
    public let jobUID: String
    /// suffix is the run's name after "<cronjob>-", or nil when no pod row is
    /// kept to read it from or the workload is the Job itself.
    public let suffix: String?
    public let lead: Incident
    public let rows: [Incident]
}

extension RunFold {
    /// podCount is how many pods the run's rows name.
    public var podCount: Int { Set(rows.compactMap(\.podUID)).count }

    /// categories is every category among the rows, worst first, once each.
    public var categories: [Category] {
        distinct(rows.map(\.category)).sorted { $0.rank < $1.rank }
    }

    /// reasons is every last reason among the rows, once each, first seen first.
    public var reasons: [String] { distinct(rows.map(\.lastReason)) }

    /// exitCodes is every exit code among the rows, once each, first seen first.
    public var exitCodes: [Int32] { distinct(rows.compactMap(\.exitCode)) }

    /// openCount is how many of the rows are still open.
    public var openCount: Int { rows.filter { $0.closedAt == nil }.count }

    /// podSuffixes is each pod's name after the run's name, in served order,
    /// for a Job whose run is the Job itself.
    public var podSuffixes: [String] {
        distinct(
            rows.compactMap { row -> String? in
                guard let name = row.podName, row.podUID != nil else { return nil }
                guard
                    let run = runSuffix(
                        podName: name, workloadName: row.workloadName,
                        workloadKind: row.workloadKind)
                else { return podNameSuffix(name: name, workloadName: row.workloadName) }
                return podNameSuffix(name: name, workloadName: "\(row.workloadName)-\(run)")
            })
    }
}

/// runSuffix is the part of a pod name that names its run: a CronJob's pod is
/// "<cronjob>-<run>-<hash>", so the run is the segment between the workload
/// prefix and the pod's own hash; a Job's pod has no run segment because the
/// Job is the run.
public func runSuffix(podName: String, workloadName: String, workloadKind: String) -> String? {
    guard workloadKind == "CronJob", !workloadName.isEmpty else { return nil }
    let prefix = workloadName + "-"
    guard podName.hasPrefix(prefix) else { return nil }
    let segments = podName.dropFirst(prefix.count).split(separator: "-")
    guard segments.count == 2 else { return nil }
    return String(segments[0])
}

/// runFolds gathers rows by job uid in the order their first row was served,
/// which is newest first, and leads each with the pod row the list would have
/// led with, or the job row when no pod row is kept, so opening a run opens
/// the pod that failed.
public func runFolds(_ rows: [Incident]) -> [RunFold] {
    buckets(rows) { $0.jobUID ?? "" }.map { bucket in
        let podRows = bucket.rows.filter { $0.podUID != nil }
        let lead = podRows.min(by: foldOrder) ?? bucket.rows[0]
        let named = lead.podName ?? podRows.first?.podName
        return RunFold(
            id: "run/\(bucket.key)", jobUID: bucket.key,
            suffix: named.flatMap {
                runSuffix(
                    podName: $0, workloadName: lead.workloadName,
                    workloadKind: lead.workloadKind)
            },
            lead: lead, rows: bucket.rows)
    }
}

/// shownRuns cuts a group's runs to the newest few and counts the rest, which
/// the "+ N more runs" line names.
public func shownRuns(_ entries: [GroupEntry], limit: Int) -> (shown: [GroupEntry], more: Int) {
    var seen = 0
    var shown: [GroupEntry] = []
    var more = 0
    for entry in entries {
        guard case .run = entry else {
            shown.append(entry)
            continue
        }
        seen += 1
        if seen <= limit { shown.append(entry) } else { more += 1 }
    }
    return (shown, more)
}
