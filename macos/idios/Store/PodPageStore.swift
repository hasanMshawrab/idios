import Foundation
import IdiosAPI
import IdiosModel
import Observation
import OpenAPIRuntime

/// ResolvedIncident is what an incident route resolved to: the pod and
/// container to select, or the run the route belongs on instead.
enum ResolvedIncident: Hashable {
    case pod(uid: String, container: String?)
    case run(jobUID: String)
}

/// PodPageStore holds one pod page: the pod as GetPod answers it, its whole
/// event stream, the incident whose segment is lit, that incident's
/// timeline, the history for Conditions, the Job's related rows, the
/// bytes of the files a person opened, and the last error of each.
@Observable @MainActor
final class PodPageStore {
    private(set) var detail: PodDetail?
    /// events is nil until PodEvents answered, so the Pod card does not claim
    /// a count it has not seen.
    private(set) var events: [Event]?
    private(set) var eventsTruncated = false
    /// lit is the GetIncident of the lit segment, kept until the next one
    /// arrives so a segment switch never blanks the pane's header.
    private(set) var lit: IncidentDetail?
    private(set) var litError: APIError?
    private(set) var timeline: [TimelineEntry] = []
    private(set) var timelineTruncated = false
    private(set) var timelineLoading = false
    private(set) var timelineError: APIError?
    private(set) var history: PodHistory?
    /// related is the Job's other rows for the Related tab: the lit
    /// incident's own related rows when one is lit, and the Job's incidents
    /// fetched by uid when the page was opened on a pod with nothing lit.
    var related: [Incident] {
        guard let lit else { return fetchedRelated }
        return relatedElsewhere(lit.relatedIncidents, podUID: detail?.pod.uid)
    }
    /// fetchedRelated is what ListIncidents answered; a failure leaves the
    /// tab empty rather than hiding the pod.
    private var fetchedRelated: [Incident] = []
    private(set) var isLoading = false
    private(set) var error: APIError?
    /// missing is the pod row having been swept while an incident that
    /// names it outlived it: nothing is broken, there is nothing to show.
    private(set) var missing = false

    let writes = IncidentWriteStore()
    private let artifacts = ArtifactContentStore()
    var contents: [String: Result<Data, APIError>] { artifacts.contents }

    /// resolve answers where an incident route belongs and seeds lit with the
    /// detail it fetched; nil when the incident is unknown (its error is in
    /// litError) or names neither a pod nor a run.
    func resolve(incidentID: String, connection: DaemonConnection) async -> ResolvedIncident? {
        await light(incidentID: incidentID, connection: connection)
        guard let loaded = lit, loaded.incident.id == incidentID else { return nil }
        // The run is the page a job-subject incident belongs on, whether or
        // not the daemon borrowed a pod to describe it.
        if loaded.incident.subjectKind == .job, let jobUID = loaded.incident.jobUID,
            !jobUID.isEmpty
        {
            return .run(jobUID: jobUID)
        }
        guard let uid = loaded.incident.podUID ?? loaded.pod?.uid else {
            // The schema lets only a job incident go without a pod, so a pod
            // incident here is the daemon contradicting itself; a job incident
            // with no run to open either has no page at all.
            litError = .notFound(message: "incident \(incidentID) names no pod and no run")
            return nil
        }
        return .pod(uid: uid, container: loaded.incident.containerName)
    }

    /// watch loads the pod and its events, then reloads the pod (and the lit
    /// incident when the row was its) on every stream event whose row is
    /// on this pod, until cancelled; it reloads before every resubscribe
    /// because the stream has no replay and a reconnect gap loses events.
    func watch(uid: String, connection: DaemonConnection) async {
        while !Task.isCancelled {
            await loadPod(uid: uid, connection: connection)
            await loadEvents(uid: uid, connection: connection)
            do {
                let output = try await connection.streamClient.StreamIncidents(query: .init())
                let body = try output.ok.body.text_event_hyphen_stream
                for try await event in body.asDecodedServerSentEventsWithJSONData(
                    of: Components.Schemas.IncidentRow.self)
                {
                    guard event.data?.podUid == uid else { continue }
                    await loadPod(uid: uid, connection: connection)
                    await loadEvents(uid: uid, connection: connection)
                    if let id = event.data?.id, id == lit?.incident.id {
                        await light(incidentID: id, connection: connection)
                    }
                }
            } catch {
                report(apiError(error), connection: connection)
            }
            guard !Task.isCancelled else { return }
            try? await Task.sleep(for: .seconds(2))
        }
    }

    /// reload fetches the pod and its events again, as the window becoming
    /// key asks for.
    func reload(uid: String, connection: DaemonConnection) async {
        await loadPod(uid: uid, connection: connection)
        await loadEvents(uid: uid, connection: connection)
    }

    /// light fetches the incident whose segment is lit; the previous one
    /// stays until this one arrives.
    func light(incidentID: String, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.GetIncident(path: .init(id: incidentID))
            switch output {
            case .ok(let ok):
                lit = try IncidentDetail(wire: try ok.body.json)
                reportLit(nil, connection: connection)
            case .badRequest(let bad):
                reportLit(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                reportLit(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            reportLit(apiError(error), connection: connection)
        }
        Screenshot.noteFirstLoad()
    }

    /// loadTimeline fetches the lit incident's timeline.
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

    /// loadHistory fetches the transitions and condition history.
    func loadHistory(uid: String, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.PodHistory(path: .init(uid: uid))
            switch output {
            case .ok(let ok):
                history = try PodHistory(wire: try ok.body.json)
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

    // A job uid is unique across clusters, so the query needs no cluster scope;
    // a failure here leaves the card empty rather than hiding the pod the
    // person came for, so it does not reach error.
    /// loadRelated fetches every incident of the Job, for the Related tab.
    func loadRelated(jobUID: String, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.ListIncidents(query: .init(job_uid: jobUID))
            guard case .ok(let ok) = output else {
                fetchedRelated = []
                return
            }
            fetchedRelated = try Page<Incident>(wire: try ok.body.json).rows
        } catch {
            guard apiError(error) != .cancelled else { return }
            fetchedRelated = []
        }
    }

    /// content fetches a captured file's bytes once.
    func content(of artifact: Artifact, connection: DaemonConnection) async {
        await artifacts.content(of: artifact, connection: connection)
    }

    /// apply replaces the lit incident's row with what a write answered and
    /// re-reads the pod, when there is one, so the badges on the cards stay
    /// true.
    func apply(_ incident: Incident, uid: String?, connection: DaemonConnection) async {
        lit = lit?.replacingIncident(incident)
        guard let uid else { return }
        await loadPod(uid: uid, connection: connection)
    }

    // The sweep removes a pod before the incidents that name it, so a 404
    // here is the retention working and never the connection's business.
    private func loadPod(uid: String, connection: DaemonConnection) async {
        isLoading = true
        defer { isLoading = false }
        do {
            let output = try await connection.client.GetPod(path: .init(uid: uid))
            switch output {
            case .ok(let ok):
                detail = try PodDetail(wire: try ok.body.json)
                missing = false
                report(nil, connection: connection)
            case .badRequest(let bad):
                notePod(APIError(wire: try bad.body.json), connection: connection)
            case .default(let statusCode, let payload):
                notePod(
                    apiError(statusCode: statusCode, body: try payload.body.json),
                    connection: connection)
            }
        } catch {
            notePod(apiError(error), connection: connection)
        }
        Screenshot.noteFirstLoad()
    }

    private func notePod(_ error: APIError, connection: DaemonConnection) {
        guard case .notFound = error else {
            report(error, connection: connection)
            return
        }
        missing = true
        report(nil, connection: connection)
    }

    /// loadEvents fetches every event on the pod, attached or not.
    private func loadEvents(uid: String, connection: DaemonConnection) async {
        do {
            let output = try await connection.client.PodEvents(
                path: .init(uid: uid), query: .init())
            switch output {
            case .ok(let ok):
                let page = try Page<Event>(wire: try ok.body.json)
                events = page.rows
                eventsTruncated = page.truncated
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

    private func report(_ error: APIError?, connection: DaemonConnection) {
        // A route change, a folder change or the window closing cancels calls
        // in flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }

    // Mirrors report, but into litError: the lit incident's failure is shown
    // in its own pane and must not overwrite the pod page's own error.
    private func reportLit(_ error: APIError?, connection: DaemonConnection) {
        guard error != .cancelled else { return }
        litError = error
        connection.note(error)
    }

    // Mirrors report, but into timelineError, for the same reason.
    private func reportTimeline(_ error: APIError?, connection: DaemonConnection) {
        guard error != .cancelled else { return }
        timelineError = error
        connection.note(error)
    }
}
