import Foundation
import IdiosAPI
import IdiosModel
import Observation
import OpenAPIRuntime

/// IncidentFilter is what the incidents list holds: a lifecycle state and a
/// category, each optional and each an independent predicate.
struct IncidentFilter: Hashable, Sendable {
    var state: IncidentState?
    var category: IdiosModel.Category?
}

extension IncidentFilter {
    /// title is the heading the list carries for this filter.
    var title: String {
        switch (state, category) {
        case (nil, nil):
            "All incidents"
        case (.attention, nil):
            "Incidents needing attention"
        // "Open" alone reads as a verb; the other states read as the state.
        case (.open, nil):
            "Open incidents"
        case (let state?, nil):
            state.title
        case (nil, let category?):
            "\(category.label) incidents, every state"
        case (let state?, let category?):
            "\(state.title) \(category.label) incidents"
        }
    }

    fileprivate func matches(_ incident: Incident) -> Bool {
        if let state {
            if state == .attention {
                // A row's own state is never attention. The application does
                // not know the window, and a streamed change on a closed row
                // is nearly always a fresh close, so this checks only unread
                // and undismissed; a stale change can linger until reload.
                guard incident.dismissedAt == nil,
                    incident.closedAt == nil || incident.acknowledgedAt == nil
                else { return false }
            } else if incident.state != state {
                return false
            }
        }
        if let category, incident.category != category { return false }
        return true
    }
}

/// IncidentsStore holds the rows of one filter and the sidebar counters, and
/// keeps both current from the incident stream.
@Observable @MainActor
final class IncidentsStore {
    private(set) var rows: [Incident] = []
    private(set) var counts: IncidentCounts?
    private(set) var isLoading = false
    private(set) var error: APIError?

    // remove needs the counts reloaded in the scope watch is currently
    // running under; it has no filter or scope of its own to recompute one.
    private var clusterIDs: [String]?

    // A write answers with the row it changed, and whether that row still
    // belongs in the list is the watch's filter and scope to decide.
    private var watchedFilter = IncidentFilter()
    private var watchedScope = ClusterScope.all

    /// filter narrows the loaded rows over workload, pod name, reason and
    /// container without another call.
    var filter = ""

    /// visibleRows is what the list draws: the loaded rows the filter keeps.
    var visibleRows: [Incident] {
        let needle = filter.trimmingCharacters(in: .whitespaces).lowercased()
        guard !needle.isEmpty else { return rows }
        return rows.filter { row in
            [row.workloadName, row.podName ?? "", row.lastReason, row.containerName ?? ""]
                .contains { $0.lowercased().contains(needle) }
        }
    }

    /// watch loads the filtered rows and the counters, then applies stream rows
    /// until it is cancelled, reloading before every resubscribe because the
    /// stream has no replay.
    func watch(connection: DaemonConnection, filter: IncidentFilter, scope: ClusterScope) async {
        watchedFilter = filter
        watchedScope = scope
        // No cluster in scope is a real answer, and the wire has no way to ask
        // for it: an absent cluster_ids means every cluster.
        guard !scope.isEmpty else {
            rows = []
            counts = nil
            clusterIDs = nil
            Screenshot.noteFirstLoad()
            return
        }
        let clusterIDs = scope.isAll ? nil : scope.selected.sorted()
        self.clusterIDs = clusterIDs
        while !Task.isCancelled {
            await load(connection: connection, filter: filter, clusterIDs: clusterIDs)
            do {
                let output = try await connection.streamClient.StreamIncidents(
                    query: .init(cluster_ids: clusterIDs))
                let body = try output.ok.body.text_event_hyphen_stream
                for try await event in body.asDecodedServerSentEventsWithJSONData(
                    of: Components.Schemas.IncidentRow.self)
                {
                    guard let wire = event.data else { continue }
                    apply(try Incident(wire: wire), filter: filter, scope: scope)
                    await loadCounts(
                        connection: connection, filter: watchedFilter, clusterIDs: clusterIDs)
                }
            } catch {
                report(apiError(error), connection: connection)
            }
            guard !Task.isCancelled else { return }
            try? await Task.sleep(for: .seconds(2))
        }
    }

    /// remove drops a row the application itself just deleted and reloads the
    /// counts; the stream sends nothing for a deleted row, so no event will.
    func remove(id: String, connection: DaemonConnection) async {
        rows.removeAll { $0.id == id }
        await loadCounts(connection: connection, filter: watchedFilter, clusterIDs: clusterIDs)
    }

    /// acknowledge stamps one row acknowledged. A repeat is a no-op the daemon
    /// still answers with the current row.
    func acknowledge(id: String, connection: DaemonConnection) async {
        await write(connection: connection) {
            let output = try await connection.client.AcknowledgeIncident(
                path: .init(id: id), body: .json(.init(id: id)))
            return try Self.row(output)
        }
    }

    /// dismiss stamps one row dismissed. A repeat is a no-op.
    func dismiss(id: String, connection: DaemonConnection) async {
        await write(connection: connection) {
            let output = try await connection.client.DismissIncident(
                path: .init(id: id), body: .json(.init(id: id)))
            return try Self.row(output)
        }
    }

    /// resolve closes one open row as manual. A closed row keeps its close.
    func resolve(id: String, connection: DaemonConnection) async {
        await write(connection: connection) {
            let output = try await connection.client.ResolveIncident(
                path: .init(id: id), body: .json(.init(id: id)))
            return try Self.row(output)
        }
    }

    // The write answers with the changed row, so the list moves without waiting
    // for the stream to repeat it.
    private func write(
        connection: DaemonConnection, call: () async throws -> Components.Schemas.IncidentRow
    ) async {
        do {
            apply(try Incident(wire: try await call()), filter: watchedFilter, scope: watchedScope)
            report(nil, connection: connection)
        } catch {
            report(apiError(error), connection: connection)
        }
    }

    // Each write operation generates its own Output type with the same three
    // cases, so unwrapping it is one overload per operation rather than a
    // shared generic (the generator gives these no common protocol).
    private static func row(_ output: Operations.AcknowledgeIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.DismissIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private static func row(_ output: Operations.ResolveIncident.Output) throws
        -> Components.Schemas.IncidentRow
    {
        switch output {
        case .ok(let ok): return try ok.body.json
        case .badRequest(let bad): throw APIError(wire: try bad.body.json)
        case .default(let statusCode, let payload):
            throw apiError(statusCode: statusCode, body: try payload.body.json)
        }
    }

    private func load(
        connection: DaemonConnection, filter: IncidentFilter, clusterIDs: [String]?
    ) async {
        isLoading = true
        defer { isLoading = false }
        do {
            let output = try await connection.client.ListIncidents(
                query: query(filter: filter, clusterIDs: clusterIDs))
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
        await loadCounts(connection: connection, filter: filter, clusterIDs: clusterIDs)
        Screenshot.noteFirstLoad()
    }

    // Each facet is counted against the other facet's selection, so the
    // counts travel with the filter the list is showing.
    private func loadCounts(
        connection: DaemonConnection, filter: IncidentFilter, clusterIDs: [String]?
    ) async {
        do {
            let output = try await connection.client.GetIncidentCounts(
                query: .init(
                    cluster_ids: clusterIDs, state: filter.state?.rawValue,
                    category: filter.category?.rawValue))
            switch output {
            case .ok(let ok):
                counts = try IncidentCounts(wire: try ok.body.json)
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

    private func query(filter: IncidentFilter, clusterIDs: [String]?)
        -> Operations.ListIncidents.Input.Query
    {
        .init(
            cluster_ids: clusterIDs, state: filter.state?.rawValue,
            category: filter.category?.rawValue)
    }

    // An event carries the whole row, so the row is replaced by id; a row that
    // left the filter or the scope is dropped, and a row that entered it goes
    // to the top, where the newest incident belongs.
    private func apply(_ incident: Incident, filter: IncidentFilter, scope: ClusterScope) {
        let belongs = filter.matches(incident) && scope.includes(incident.clusterID)
        guard let index = rows.firstIndex(where: { $0.id == incident.id }) else {
            if belongs { rows.insert(incident, at: 0) }
            return
        }
        if belongs {
            rows[index] = incident
        } else {
            rows.remove(at: index)
        }
    }

    private func report(_ error: APIError?, connection: DaemonConnection) {
        // A route change, a filter change or the window closing cancels calls in
        // flight; that is the app itself, not the daemon going away.
        guard error != .cancelled else { return }
        self.error = error
        connection.note(error)
    }
}
