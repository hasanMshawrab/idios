import Foundation

/// runTitle is what the page is called: a Job a CronJob created is a run of
/// that CronJob and is named by the segment after the CronJob's name, and a
/// Job nothing created is a Job.
public func runTitle(jobName: String?, cronjobName: String?, jobUID: String) -> String {
    guard let jobName, !jobName.isEmpty else {
        return "Job \(middleElided(jobUID, keeping: 12))"
    }
    guard let cronjobName, !cronjobName.isEmpty else { return "Job \(jobName)" }
    // A Job a CronJob owns is a run whatever it was named, so a name without
    // the CronJob's prefix still reads as a run.
    return "Run \(podNameSuffix(name: jobName, workloadName: cronjobName)) of \(cronjobName)"
}

/// runStateTag is the Job's condition as the header's tag; the run's outcome
/// is the condition and never the counters.
public func runStateTag(_ job: Job?) -> String {
    guard let job else { return "NOT RECORDED" }
    if job.deletedAt != nil { return "DELETED" }
    guard let type = job.conditionType, !type.isEmpty else { return "RUNNING" }
    guard let reason = job.conditionReason, !reason.isEmpty else { return type.uppercased() }
    return "\(type.uppercased()), \(spacedOnCapitals(reason).uppercased())"
}

// Kubernetes writes a condition reason as one camel-cased word; a tag reads
// it as the words it is made of.
private func spacedOnCapitals(_ value: String) -> String {
    var words: [String] = []
    var word = ""
    for character in value {
        if character.isUppercase, !word.isEmpty {
            words.append(word)
            word = ""
        }
        word.append(character)
    }
    if !word.isEmpty { words.append(word) }
    return words.joined(separator: " ")
}

/// runCountsLine is the line beside the tag: how many attempts the run made,
/// how many incidents they opened and how many are still open, and how long
/// the run ran.
public func runCountsLine(job: Job?, attempts: [RunAttempt], rows: [Incident], now: Date)
    -> String
{
    let open = rows.filter { $0.closedAt == nil }.count
    var parts = [
        plural(attemptCount(attempts), "attempt"),
        plural(rows.count, "incident") + ", \(open) open",
    ]
    if let ran = runDuration(job: job, now: now) { parts.append(ran) }
    return parts.joined(separator: " - ")
}

// The Job's own row closes the Attempts card but is not an attempt: a run of
// two pods whose Job then gave up made two attempts and opened three incidents.
private func attemptCount(_ attempts: [RunAttempt]) -> Int {
    attempts.filter { $0.number > 0 }.count
}

private func succeededCount(_ attempts: [RunAttempt]) -> Int {
    attempts.filter { $0.outcome == .succeeded }.count
}

private func runDuration(job: Job?, now: Date) -> String? {
    guard let started = job?.startedAt?.date else { return nil }
    guard let finished = job?.finishedAt?.date else {
        return "running for \(durationText(from: started, to: now))"
    }
    return "ran \(durationText(from: started, to: finished))"
}

/// AttemptOutcome is what an attempt that opened no incident says for itself.
public enum AttemptOutcome: Hashable, Sendable {
    case succeeded
    case state(ContainerState)
    case phase(String)

    /// text is the outcome in the words a pill carries.
    public var text: String {
        switch self {
        case .succeeded: "succeeded"
        case .state(let state): state.rawValue
        case .phase(let phase): phase.lowercased()
        }
    }
}

/// RunAttempt is one line of the Attempts card: one pod of the run, or the
/// Job's own row that closes the card when the Job gave up.
public struct RunAttempt: Identifiable, Hashable, Sendable {
    public let id: String
    /// number is the attempt's place in the run, oldest first; zero for the
    /// Job's own row, which is not an attempt.
    public let number: Int
    public let podUID: String?
    /// podSuffix is the pod's name after the Job's, which is all that tells
    /// one attempt of a run from the next.
    public let podSuffix: String
    /// category, reason, exitCode, incidentID and state are the incident the
    /// attempt opened; an attempt that opened none carries an outcome instead.
    public let category: Category?
    public let reason: String
    public let exitCode: Int32?
    /// openedAt is when the incident opened, or when the pod was created for
    /// an attempt that opened none.
    public let openedAt: Timestamp
    public let incidentID: String?
    public let state: IncidentState?
    public let outcome: AttemptOutcome?
}

/// runAttempts reads the run's pods and its incidents into one line per
/// attempt, oldest pod first, closing with the Job's own row when one is
/// there.
public func runAttempts(pods: [PodRow], rows: [Incident], jobName: String?) -> [RunAttempt] {
    // An attempt is a pod, so a pod that opened two incidents is one line and
    // the first of them is the one it names; the counts line says how many
    // incidents there were.
    var byPod: [String: Incident] = [:]
    for row in rows.filter({ $0.subjectKind != .job }).sorted(by: incidentOrder) {
        guard let uid = row.podUID, byPod[uid] == nil else { continue }
        byPod[uid] = row
    }
    let held = Set(pods.map(\.uid))
    var lines = pods.map { AttemptLine(key: $0.createdAt.raw, pod: $0, row: byPod[$0.uid]) }
    // A pod the sweep removed leaves its incident behind, and that attempt is
    // still one the run made: it takes its place by the time it opened.
    for row in byPod.values where !held.contains(row.podUID ?? "") {
        lines.append(AttemptLine(key: row.openedAt.raw, pod: nil, row: row))
    }
    lines.sort { a, b in a.key == b.key ? a.uid < b.uid : a.key < b.key }
    let attempts = lines.enumerated().map { index, line in
        RunAttempt(
            id: line.pod?.uid ?? line.row?.id ?? line.uid, number: index + 1,
            podUID: line.pod?.uid ?? line.row?.podUID,
            podSuffix: attemptSuffix(name: line.pod?.name ?? line.row?.podName, jobName: jobName),
            category: line.row?.category, reason: line.row?.lastReason ?? "",
            exitCode: line.row?.exitCode, openedAt: line.openedAt,
            incidentID: line.row?.id, state: line.row?.state,
            outcome: line.row == nil ? line.pod.map(attemptOutcome) : nil)
    }
    let jobRows = rows.filter { $0.subjectKind == .job }.map { row in
        RunAttempt(
            id: row.id, number: 0, podUID: row.podUID, podSuffix: "", category: row.category,
            reason: row.lastReason, exitCode: row.exitCode, openedAt: row.openedAt,
            incidentID: row.id, state: row.state, outcome: nil)
    }
    return attempts + jobRows
}

// One line of the card before it is numbered: a pod, the incident it opened,
// or an incident whose pod is gone.
private struct AttemptLine {
    let key: String
    let pod: PodRow?
    let row: Incident?

    // The uid breaks ties, so two pods created in the same second keep one
    // order between reloads.
    var uid: String { pod?.uid ?? row?.podUID ?? row?.id ?? "" }
    var openedAt: Timestamp { row?.openedAt ?? pod?.createdAt ?? Timestamp(key) }
}

private func incidentOrder(_ a: Incident, _ b: Incident) -> Bool {
    a.openedAt.raw == b.openedAt.raw ? a.id < b.id : a.openedAt.raw < b.openedAt.raw
}

private func attemptSuffix(name: String?, jobName: String?) -> String {
    guard let name else { return "pod unknown" }
    return podNameSuffix(name: name, workloadName: jobName ?? "")
}

// A pod that opened no incident says how it ended in its own fields: the
// phase is the only success a pod row knows, and the worst state its
// containers reached is what a failure that opened nothing looks like.
private func attemptOutcome(_ pod: PodRow) -> AttemptOutcome {
    if pod.phase == "Succeeded" { return .succeeded }
    guard let state = pod.worstState else { return .phase(pod.phase) }
    return .state(state)
}

/// RunCapture is what the page could read of the attempts' captured logs,
/// which is bounded by how many attempt details it fetched.
public struct RunCapture: Hashable, Sendable {
    public let attemptsRead: Int
    public let attemptsWithLog: Int
    public let lastLine: String?

    /// init takes what the page read and what it found.
    public init(attemptsRead: Int, attemptsWithLog: Int, lastLine: String?) {
        self.attemptsRead = attemptsRead
        self.attemptsWithLog = attemptsWithLog
        self.lastLine = lastLine
    }
}

/// runVerdictSentences is what the Run page leads with: how the run ended,
/// what it was allowed, how its attempts exited, what they captured, what
/// image ran and whether this has happened before, each sentence left out
/// when its fields are not there.
public func runVerdictSentences(
    job: Job?, attempts: [RunAttempt], capture: RunCapture, imageTag: String?,
    sameReasonRuns: Int?, now: Date
) -> [String] {
    var sentences = [runOutcome(job: job, attempts: attempts)]
    if let job {
        // The counters are context: the sentence names the limit and what was
        // counted against it, and leaves the outcome to the condition.
        sentences.append(
            "The backoff limit is \(job.backoffLimit); the Job counted \(job.failed) failed pods.")
    }
    if let exits = runExits(attempts) { sentences.append(exits) }
    if let captured = runCaptureSentence(capture) { sentences.append(captured) }
    if let imageTag, !imageTag.isEmpty { sentences.append("Image tag \(imageTag).") }
    if let sameReasonRuns, sameReasonRuns > 0 {
        sentences.append(
            "\(plural(sameReasonRuns, "earlier run")) of this CronJob failed the same way.")
    }
    return sentences
}

private func runOutcome(job: Job?, attempts: [RunAttempt]) -> String {
    let made = plural(attemptCount(attempts), "attempt")
    guard let job else { return "The run is not recorded; what is left is \(made)." }
    let ran = job.startedAt?.date.flatMap { started in
        job.finishedAt?.date.map { " in \(durationText(from: started, to: $0))" }
    }
    switch job.conditionType {
    case "Failed": return "Failed after \(made)\(ran ?? "")."
    case "Complete":
        // The succeeded attempts are drawn, so they are counted from the rows;
        // the Job's counter is kept only where it exceeds them, which is a pod
        // pruned before idios saw it.
        let won = max(succeededCount(attempts), Int(job.succeeded))
        guard won > 0 else { return "Complete after \(made)\(ran ?? "")." }
        let failed = plural(attemptCount(attempts) - succeededCount(attempts), "failed attempt")
        return "Complete after \(failed) and \(won) that succeeded\(ran ?? "")."
    default:
        guard let started = job.startedAt else { return "Still going, \(made)." }
        return "Still going, \(made) since \(clockTime(started))."
    }
}

// A succeeded attempt has no exit code worth a sentence, so once one exists
// the sentence is about the failed attempts and says so.
private func runExits(_ attempts: [RunAttempt]) -> String? {
    let codes = distinct(attempts.compactMap(\.exitCode))
    guard let only = codes.first else { return nil }
    let subject = succeededCount(attempts) > 0 ? "failed attempt" : "attempt"
    guard codes.count > 1 else { return "Every \(subject) exits \(only)." }
    return "Attempts exit \(codes.map(String.init).joined(separator: ", "))."
}

// A page that has read no attempt has not found out whether anything was
// captured, which is not the same as nothing having been.
private func runCaptureSentence(_ capture: RunCapture) -> String? {
    guard capture.attemptsRead > 0 else { return nil }
    var sentence: String
    if capture.attemptsWithLog == 0 {
        sentence = "Nothing was captured."
    } else if capture.attemptsWithLog == capture.attemptsRead {
        sentence = "Every attempt captured a log."
    } else {
        sentence =
            "\(capture.attemptsWithLog) of \(capture.attemptsRead) attempts captured a log."
    }
    if let lastLine = capture.lastLine { sentence += " The last line is \"\(lastLine)\"." }
    return sentence
}
