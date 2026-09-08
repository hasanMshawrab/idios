import Foundation

/// RunOutcome is what one cell of the run strip says happened to a run.
public enum RunOutcome: Hashable, Sendable {
    /// complete is a run whose condition is Complete.
    case complete
    /// failed carries the tone of the incidents the run opened.
    case failed(StateTone)
    /// running is a run with no condition yet.
    case running
    /// swept is a run known only from an incident's job uid, because the
    /// sweeper removed the jobs row.
    case swept
}

/// RunCell is one run as the strip and the table both read it: what it did,
/// what a hover says, what a click opens and what a drag acknowledges.
public struct RunCell: Identifiable, Hashable, Sendable {
    public let id: String
    public let jobUID: String
    /// name is the run's suffix after "<cronjob>-", or the whole job name
    /// when it does not carry that prefix.
    public let name: String
    public let outcome: RunOutcome
    public let startedAt: Timestamp?
    public let finishedAt: Timestamp?
    public let condition: String
    public let reason: String?
    public let exitCode: Int32?
    public let attempts: Int32
    public let backoffLimit: Int32
    public let incidentIDs: [String]
    public let podUID: String?
    public let podName: String?
}

extension RunCell {
    /// openIncidentIDs is what a drag over this cell acknowledges.
    public var openIncidentIDs: [String] {
        // The cell carries its rows' ids, not their states; the tone is the
        // one thing that says whether any of them is still open.
        guard case .failed(let tone) = outcome, tone != .closed else { return [] }
        return incidentIDs
    }

    /// hover is the one line the strip shows on a cell: the run, its
    /// condition, how long it ran and how it exited.
    public func hover(now: Date) -> String {
        var parts = [name]
        if let reason { parts.append("\(condition) (\(reason))") } else { parts.append(condition) }
        if let started = startedAt?.date {
            if let finished = finishedAt?.date {
                parts.append("ran \(durationText(from: started, to: finished))")
            } else if outcome == .running {
                parts.append("still going")
            } else {
                parts.append("ran \(durationText(from: started, to: now))")
            }
        }
        if let exitCode { parts.append("exit \(exitCode)") }
        return parts.joined(separator: " - ")
    }

    fileprivate var isFailed: Bool {
        if case .failed = outcome { return true }
        return false
    }
}

/// runStripLimit is how many runs the strip draws one cell each before it
/// becomes an hour-by-minute matrix.
public let runStripLimit = 200

/// runCells reads a page of runs and the incidents of the same workload into
/// one cell per run, oldest first, adding a cell for every run that only an
/// incident's job uid still names.
public func runCells(jobs: [Job], incidents: [Incident], cronjobName: String) -> [RunCell] {
    var order: [String] = []
    var byJob: [String: [Incident]] = [:]
    for row in incidents {
        // An incident with no job uid is not a run: a run is a Job.
        guard let uid = row.jobUID else { continue }
        if byJob[uid] == nil { order.append(uid) }
        byJob[uid, default: []].append(row)
    }

    let recorded = Set(jobs.map(\.uid))
    let cells =
        jobs.map { jobCell($0, rows: byJob[$0.uid] ?? [], cronjobName: cronjobName) }
        + order.filter { !recorded.contains($0) }
        .map { sweptCell(jobUID: $0, rows: byJob[$0] ?? [], cronjobName: cronjobName) }
    return cells.sorted(by: runCellOrder)
}

private func jobCell(_ job: Job, rows: [Incident], cronjobName: String) -> RunCell {
    let outcome: RunOutcome
    if rows.isEmpty, job.complete {
        outcome = .complete
    } else if rows.isEmpty, job.conditionType == nil {
        outcome = .running
    } else {
        outcome = .failed(groupBadge(rows).tone)
    }
    let condition: String
    if job.complete {
        condition = "Complete"
    } else if job.conditionType == nil {
        condition = "running"
    } else {
        condition = "Failed"
    }
    let lead = runLead(rows)
    return RunCell(
        id: job.uid, jobUID: job.uid, name: runName(job.name, cronjobName: cronjobName),
        outcome: outcome, startedAt: job.startedAt, finishedAt: job.finishedAt,
        condition: condition, reason: job.conditionReason,
        exitCode: rows.compactMap(\.exitCode).first, attempts: job.failed,
        backoffLimit: job.backoffLimit, incidentIDs: rows.map(\.id), podUID: lead?.podUID,
        podName: lead?.podName)
}

private func sweptCell(jobUID: String, rows: [Incident], cronjobName: String) -> RunCell {
    let lead = runLead(rows)
    let suffix = lead?.podName.flatMap {
        runSuffix(podName: $0, workloadName: cronjobName, workloadKind: "CronJob")
    }
    return RunCell(
        id: jobUID, jobUID: jobUID, name: suffix ?? middleElided(jobUID, keeping: 12),
        outcome: .swept, startedAt: nil, finishedAt: nil, condition: "not recorded", reason: nil,
        exitCode: rows.compactMap(\.exitCode).first, attempts: 0, backoffLimit: 0,
        incidentIDs: rows.map(\.id), podUID: lead?.podUID, podName: lead?.podName)
}

private func runName(_ name: String, cronjobName: String) -> String {
    let prefix = cronjobName + "-"
    guard !cronjobName.isEmpty, name.hasPrefix(prefix) else { return name }
    return String(name.dropFirst(prefix.count))
}

// A click opens the pod that failed, which is the newest of the run's rows
// that still names one.
private func runLead(_ rows: [Incident]) -> Incident? {
    rows.filter { $0.podUID != nil }.min(by: foldOrder)
}

private func runCellOrder(_ a: RunCell, _ b: RunCell) -> Bool {
    let first = a.startedAt?.raw
    let second = b.startedAt?.raw
    if (first == nil) != (second == nil) { return second == nil }
    if let first, let second, first != second { return first < second }
    return a.jobUID < b.jobUID
}

/// RunMatrixRow is one hour of the matrix: sixty minutes, each holding the
/// run that started in it.
public struct RunMatrixRow: Identifiable, Hashable, Sendable {
    public let id: String
    public let hour: Timestamp
    public let minutes: [RunCell?]
}

/// RunMatrix is the strip past its limit: one row per hour of the window,
/// oldest first, with no hour left out.
public struct RunMatrix: Hashable, Sendable {
    public let rows: [RunMatrixRow]
}

/// runMatrix buckets the cells by the hour and minute they started, because
/// past two hundred runs a line of cells is thinner than a hair.
public func runMatrix(_ cells: [RunCell]) -> RunMatrix {
    var byHour: [String: [Int: RunCell]] = [:]
    for cell in cells {
        // The daemon writes one fixed-width UTC layout, so the hour is the
        // first thirteen characters and the minute the two after the colon.
        guard let raw = cell.startedAt?.raw, raw.count >= 16 else { continue }
        let characters = Array(raw)
        guard let minute = Int(String(characters[14..<16])), minute < 60 else { continue }
        byHour[String(characters[0..<13]), default: [:]][minute] = cell
    }
    guard let first = byHour.keys.min(), let last = byHour.keys.max(),
        let start = Timestamp(first + ":00:00Z").date, let end = Timestamp(last + ":00:00Z").date
    else { return RunMatrix(rows: []) }

    var rows: [RunMatrixRow] = []
    var at = start
    while at <= end {
        let hour = hourStamp(at)
        let minutes = byHour[String(hour.raw.prefix(13))] ?? [:]
        rows.append(
            RunMatrixRow(id: hour.raw, hour: hour, minutes: (0..<60).map { minutes[$0] }))
        at = at.addingTimeInterval(3600)
    }
    return RunMatrix(rows: rows)
}

private let utcCalendar: Calendar = {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "UTC") ?? .gmt
    return calendar
}()

private func hourStamp(_ date: Date) -> Timestamp {
    let parts = utcCalendar.dateComponents([.year, .month, .day, .hour], from: date)
    return Timestamp(
        String(
            format: "%04d-%02d-%02dT%02d:00:00Z", parts.year ?? 0, parts.month ?? 0,
            parts.day ?? 0, parts.hour ?? 0))
}

/// RunTableRow is one line of the Runs table: a run, and the consecutive
/// runs after it that failed for the same reason, which it stands for.
public struct RunTableRow: Identifiable, Hashable, Sendable {
    public let id: String
    public let run: RunCell
    public let folded: [RunCell]
}

/// runTableRows reads the cells newest first and folds each stretch of
/// consecutive runs that share a reason into the first of them, because
/// fifty rows of one sentence answer nothing the first row did not.
public func runTableRows(_ cells: [RunCell]) -> [RunTableRow] {
    var leads: [RunCell] = []
    var folded: [[RunCell]] = []
    for cell in cells.reversed() {
        if let lead = leads.last, lead.isFailed, cell.isFailed, lead.reason == cell.reason {
            folded[folded.count - 1].append(cell)
            continue
        }
        leads.append(cell)
        folded.append([])
    }
    return zip(leads, folded).map { RunTableRow(id: $0.jobUID, run: $0, folded: $1) }
}
