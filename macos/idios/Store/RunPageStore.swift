import Foundation
import IdiosAPI
import IdiosModel
import Observation
import OpenAPIRuntime

/// RunPageStore holds one Run page: the Job as the job list answers it by
/// uid, the run's incidents, the detail of each attempt, the other runs of
/// the same CronJob, the lead incident's timeline and the bytes of the files
/// a person opened.
@Observable @MainActor
final class RunPageStore {
    private(set) var job: Job?
    /// jobMissing is the jobs row having been swept while an incident that
    /// names it outlived it: the page still has the attempts.
    private(set) var jobMissing = false
    private(set) var rows: [Incident] = []
    /// pods are every pod the Job started, the ones it pruned included, which
    /// is what the Attempts card draws a line per.
    private(set) var pods: [PodRow] = []
    /// details is the GetIncident of each attempt, by incident id: what the
    /// Logs tab, the Events tab and the verdict's capture sentence read.
    private(set) var details: [String: IncidentDetail] = [:]
    private(set) var siblingRuns: [Job] = []
    private(set) var timeline: [TimelineEntry] = []
    private(set) var timelineTruncated = false
    private(set) var timelineLoading = false
    private(set) var timelineError: APIError?
    private(set) var isLoading = false
    private(set) var error: APIError?

    let writes = IncidentWriteStore()
    private let artifacts = ArtifactContentStore()
    var contents: [String: Result<Data, APIError>] { artifacts.contents }

    /// attemptLimit bounds the per-attempt fetches: a backoff limit is single
    /// digits, and a run that somehow made hundreds of attempts must not make
    /// hundreds of calls.
    static let attemptLimit = 12

    /// watch loads the run and reloads it on every stream row that names one
    /// of its incidents, until cancelled; it reloads before every resubscribe
    /// because the stream has no replay and a reconnect gap loses rows.
    func watch(jobUID: String, connection: DaemonConnection) async {
        while !Task.isCancelled {
            await reload(jobUID: jobUID, connection: connection)
            do {
                let output = try await connection.streamClient.StreamIncidents(query: .init())
                let body = try output.ok.body.text_event_hyphen_stream
                for try await event in body.asDecodedServerSentEventsWithJSONData(
                    of: Components.Schemas.IncidentRow.self)
                {
                    guard event.data?.jobUid == jobUID else { continue }
                    await reload(jobUID: jobUID, connection: connection)
                }
            } catch {
                report(apiError(error), connection: connection)
            }
            guard !Task.isCancelled else { return }
            try? await Task.sleep(for: .seconds(2))
        }
    }

    /// reload fetches the Job, the run's incidents, each attempt's detail and
    /// the CronJob's other runs again.
    func reload(jobUID: String, connection: DaemonConnection) async {
        isLoading = true
        defer { isLoading = false }
        await loadJob(jobUID: jobUID, connection: connection)
        await loadRows(jobUID: jobUID, connection: connection)
        await loadPods(jobUID: jobUID, connection: connection)
        await loadDetails(connection: connection)
        await loadSiblings(connection: connection)
        Screenshot.noteFirstLoad()
    }

    /// loadTimeline fetches the lead incident's timeline.
    func loadTimeline(incidentID: String, connection: DaemonConnection) async {
        timelineLoading = true
        defer { timelineLoading = false }
        do {
            let output = try await connection.client.IncidentTimeline(path: .init(id: incidentID))
            switch output {
            case .ok(let ok):
                let page = try Page<TimelineEntry>(wire: try ok.body.json)
                timeline = page.rows
                timelineTruncated = page.truncated
                reportTimeline(nil, connection: connection)
            case .badRequest(let bad):
                reportTimeline(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportTimeline(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportTimeline(apiError(error), connection: connection)
        }
        Screenshot.noteFirstLoad()
    }

    /// acknowledgeRun marks every open, unacknowledged incident of the run
    /// seen and answers how many it changed.
    func acknowledgeRun(connection: DaemonConnection) async -> Int {
        var changed = 0
        for row in unacknowledged {
            guard let updated = await writes.acknowledge(id: row.id, connection: connection)
            else { continue }
            apply(updated)
            changed += 1
        }
        return changed
    }

    /// acknowledge marks the named incidents seen, for a drag over the run
    /// strip.
    func acknowledge(ids: [String], connection: DaemonConnection) async {
        for id in ids {
            guard let updated = await writes.acknowledge(id: id, connection: connection) else {
                continue
            }
            apply(updated)
        }
    }

    /// content fetches a captured file's bytes once.
    func content(of artifact: Artifact, connection: DaemonConnection) async {
        await artifacts.content(of: artifact, connection: connection)
    }

    /// apply replaces the run's row with what a write answered, so the header
    /// and the cards show the write before the stream carries it back.
    func apply(_ incident: Incident) {
        guard let index = rows.firstIndex(where: { $0.id == incident.id }) else { return }
        rows[index] = incident
        details[incident.id] = details[incident.id]?.replacingIncident(incident)
    }

    /// unacknowledged is every open incident of the run that no one has
    /// marked seen, which is what one Acknowledge acts on.
    var unacknowledged: [Incident] {
        rows.filter { $0.closedAt == nil && $0.acknowledgedAt == nil }
    }

    /// lead is the incident the header's actions, the timeline and Ask AI act
    /// on: the Job's own row when it has one, because that is the incident
    /// about the run, and the newest attempt's otherwise.
    var lead: Incident? {
        if let jobRow = rows.first(where: { $0.subjectKind == .job }) { return jobRow }
        return attemptRows.last
    }

    /// capture is what the fetched attempt details say about the run's logs,
    /// for the verdict.
    var capture: RunCapture {
        let read = attemptRows.compactMap { details[$0.id] }
        let withLog = read.filter { !logFiles($0).isEmpty }.count
        return RunCapture(
            attemptsRead: read.count, attemptsWithLog: withLog, lastLine: lastLine)
    }

    /// logArtifacts is every attempt's captured file, oldest attempt first,
    /// for the Logs tab's one scrubber.
    var logArtifacts: [Artifact] {
        attemptRows.flatMap { row in details[row.id].map(logFiles) ?? [] }
    }

    /// events is the Job's events and its attempts', newest stamp first, for
    /// the Events tab.
    var events: [Event] {
        var seen: Set<String> = []
        var all: [Event] = []
        for detail in details.values {
            for event in detail.events where seen.insert(event.id).inserted {
                all.append(event)
            }
        }
        return all.sorted { eventStamp($0) > eventStamp($1) }
    }

    // The sweeper removes a jobs row before the incidents that name it, so an
    // answer without the Job is retention working and never the connection's
    // business. The match is by uid inside the answer, because a daemon that
    // ignores the filter must show no Job rather than another run's.
    private func loadJob(jobUID: String, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.ListJobs(
                query: .init(limit: 1, job_uid: jobUID))
            switch output {
            case .ok(let ok):
                let page = try Page<Job>(wire: try ok.body.json)
                job = page.rows.first { $0.uid == jobUID }
                jobMissing = job == nil
                report(nil, connection: connection)
            case .badRequest(let bad):
                noteJob(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                noteJob(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            noteJob(apiError(error), connection: connection)
        }
    }

    private func noteJob(_ error: APIError, connection: DaemonConnection) {
        guard case .notFound = error else {
            report(error, connection: connection)
            return
        }
        job = nil
        jobMissing = true
        report(nil, connection: connection)
    }

    private func loadRows(jobUID: String, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.ListIncidents(query: .init(job_uid: jobUID))
            switch output {
            case .ok(let ok):
                rows = try Page<Incident>(wire: try ok.body.json).rows
                report(nil, connection: connection)
            case .badRequest(let bad):
                report(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            report(apiError(error), connection: connection)
        }
    }

    // A run's own pods, live and deleted both: a Job prunes its attempts as it
    // retries, and an attempt that ran is an attempt whether its pod is still
    // there or not.
    private func loadPods(jobUID: String, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.ListPods(query: .init(job_uid: jobUID))
            switch output {
            case .ok(let ok):
                pods = try Page<PodRow>(wire: try ok.body.json).rows
                report(nil, connection: connection)
            case .badRequest(let bad):
                report(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            report(apiError(error), connection: connection)
        }
    }

    // The newest attempts are the ones a reader is on, and the Job's own row
    // carries the run's own events; a detail that failed to load keeps the one
    // the last pass held rather than emptying a tab.
    private func loadDetails(connection: DaemonConnection) async {
        let wanted =
            Array(attemptRows.suffix(Self.attemptLimit))
            + rows.filter { $0.subjectKind == .job }
        var loaded: [String: IncidentDetail] = [:]
        for row in wanted {
            loaded[row.id] = await detail(id: row.id, connection: connection) ?? details[row.id]
        }
        details = loaded
    }

    private func detail(id: String, connection: DaemonConnection) async -> IncidentDetail? {
        do {
            let output = try await connection.client.GetIncident(path: .init(id: id))
            switch output {
            case .ok(let ok):
                report(nil, connection: connection)
                return try IncidentDetail(wire: try ok.body.json)
            case .badRequest(let bad):
                report(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                report(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            report(apiError(error), connection: connection)
        }
        return nil
    }

    // A standalone Job has no other runs to compare with; the name chain is
    // what ties a run to its CronJob, as the Runs table reads it.
    private func loadSiblings(connection: DaemonConnection) async {
        guard let job, let cronjobName = job.cronjobName, !cronjobName.isEmpty else {
            siblingRuns = []
            return
        }
        do {
            let output = try await connection.client.ListJobs(
                query: .init(
                    cluster_ids: [job.clusterID], namespace: job.namespace,
                    limit: Int32(Self.siblingLimit), cronjob_name: cronjobName))
            guard case .ok(let ok) = output else { return }
            siblingRuns = try Page<Job>(wire: try ok.body.json).rows
        } catch {
            guard apiError(error) != .cancelled else { return }
            siblingRuns = []
        }
    }

    // The rail shows five neighbours and says how many more there are, so the
    // page asks for a line of runs rather than every run the CronJob made.
    private static let siblingLimit = 50

    private var attemptRows: [Incident] {
        rows.filter { $0.subjectKind != .job }
            .sorted { a, b in
                a.openedAt.raw == b.openedAt.raw ? a.id < b.id : a.openedAt.raw < b.openedAt.raw
            }
    }

    private func logFiles(_ detail: IncidentDetail) -> [Artifact] {
        detail.artifacts.filter { $0.kind != .podJSON && $0.filePath != nil }
            .sorted(by: artifactOrder)
    }

    // The last line of the newest attempt's newest file is the run's own last
    // word; it is unknown until that file's bytes have answered.
    private var lastLine: String? {
        guard let newest = attemptRows.last, let detail = details[newest.id],
            let artifact = logFiles(detail).last,
            case .success(let data)? = contents[artifact.id]
        else { return nil }
        let text = String(decoding: data, as: UTF8.self)
        return text.split(whereSeparator: \.isNewline).last { !$0.isEmpty }.map(String.init)
    }

    private func eventStamp(_ event: Event) -> String {
        event.lastTS?.raw ?? event.firstTS?.raw ?? ""
    }

    private func report(_ error: APIError?, connection: DaemonConnection) {
        // A route change, a folder change or the window closing cancels calls
        // in flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }

    // Mirrors report, but into timelineError: the timeline's failure is shown
    // in its own tab and must not overwrite the page's own error.
    private func reportTimeline(_ error: APIError?, connection: DaemonConnection) {
        guard error != .cancelled else { return }
        timelineError = error
        connection.note(error)
    }
}
